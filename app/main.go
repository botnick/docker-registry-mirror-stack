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

	app, err := NewApp(cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go app.janitorLoop(ctx)
	go app.gcLoop(ctx)
	go app.healthLoop(ctx)
	go app.housekeepingLoop(ctx)

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
