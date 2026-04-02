package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	if cfg.AppMode == "router" {
		handler, err := NewRegistryRouter(cfg, logger)
		if err != nil {
			logger.Error("router startup failed", "error", err)
			os.Exit(1)
		}
		server := &http.Server{
			Addr:              cfg.ListenAddress(),
			Handler:           handler,
			ReadHeaderTimeout: 15 * second,
		}
		logger.Info("registry router started", "addr", server.Addr, "targets", cfg.TargetHostList())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("router stopped with error", "error", err)
			os.Exit(1)
		}
		return
	}

	app, err := NewApp(cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.AppMode == "gc-worker" {
		for _, target := range cfg.Upstreams {
			_ = app.setGCActiveForTarget(target, false)
		}
		go app.gcWorkerLoop(ctx)
		app.logger.Info("registry gc worker started", "poll_interval", cfg.GCWorkerPollInterval.String())
		<-ctx.Done()
		return
	}

	go app.healthLoop(ctx)
	go app.housekeepingLoop(ctx)
	go app.janitorLoop(ctx)
	go app.catalogSyncLoop(ctx)

	server := &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           app.routes(),
		ReadHeaderTimeout: 15 * second,
	}

	app.logger.Info("registry control started", "addr", server.Addr, "registry_url", cfg.RegistryURL)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}
