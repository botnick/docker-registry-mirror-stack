package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type RegistryRouter struct {
	cfg           Config
	logger        *slog.Logger
	defaultTarget UpstreamTarget
	proxies       map[string]*httputil.ReverseProxy
}

func NewRegistryRouter(cfg Config, logger *slog.Logger) (http.Handler, error) {
	router := &RegistryRouter{
		cfg:           cfg,
		logger:        logger,
		defaultTarget: cfg.DefaultTarget(),
		proxies:       make(map[string]*httputil.ReverseProxy, len(cfg.Upstreams)),
	}

	for _, target := range cfg.Upstreams {
		targetURL, err := url.Parse(target.BackendURL)
		if err != nil {
			return nil, fmt.Errorf("parse backend URL for %s: %w", target.Host, err)
		}
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("registry proxy failed", "error", err, "target", target.Host, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"registry upstream proxy failed"}`))
		}
		router.proxies[target.Host] = proxy
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", router.handleHealthz)
	mux.HandleFunc("/v1/_ping", router.handleLegacyPing)
	mux.HandleFunc("/v2", router.handleProxy)
	mux.HandleFunc("/v2/", router.handleProxy)
	mux.HandleFunc("/", router.handleRoot)
	return mux, nil
}

func (r *RegistryRouter) handleHealthz(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	payload := map[string]any{
		"status":  "ok",
		"mode":    "router",
		"default": r.defaultTarget.Host,
		"targets": r.cfg.TargetHostList(),
	}
	writeJSONStatic(w, http.StatusOK, payload)
}

func (r *RegistryRouter) handleRoot(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/" {
		writeJSONStatic(w, http.StatusOK, map[string]any{
			"status":       "ok",
			"mode":         "router",
			"default_host": r.defaultTarget.Host,
			"targets":      r.cfg.TargetHostList(),
		})
		return
	}
	http.NotFound(w, req)
}

func (r *RegistryRouter) handleLegacyPing(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	if req.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSONStatic(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"mode":    "router",
		"default": r.defaultTarget.Host,
	})
}

func (r *RegistryRouter) handleProxy(w http.ResponseWriter, req *http.Request) {
	target, rewrittenPath := r.resolveTarget(req.URL.Path)
	proxy, ok := r.proxies[target.Host]
	if !ok {
		writeJSONStatic(w, http.StatusBadGateway, map[string]any{
			"error":  "proxy target is not configured",
			"target": target.Host,
		})
		return
	}

	cloned := req.Clone(req.Context())
	cloned.URL = cloneURL(req.URL)
	cloned.URL.Path = rewrittenPath
	cloned.URL.RawPath = rewrittenPath
	cloned.Host = req.Host

	r.logger.Info("registry router request",
		"method", req.Method,
		"target", target.Host,
		"path", req.URL.Path,
		"rewritten_path", rewrittenPath,
	)
	proxy.ServeHTTP(w, cloned)
}

func (r *RegistryRouter) resolveTarget(path string) (UpstreamTarget, string) {
	if path == "" || path == "/v2" || path == "/v2/" {
		return r.defaultTarget, normalizeRegistryPath(path)
	}
	remainder := strings.TrimPrefix(path, "/v2/")
	lowerRemainder := strings.ToLower(remainder)
	for _, target := range r.cfg.Upstreams {
		prefix := target.Host + "/"
		if strings.HasPrefix(lowerRemainder, prefix) {
			trimmed := remainder[len(prefix):]
			return target, normalizeRegistryPath("/v2/" + trimmed)
		}
	}
	return r.defaultTarget, normalizeRegistryPath(path)
}

func normalizeRegistryPath(path string) string {
	switch path {
	case "", "/v2":
		return "/v2/"
	default:
		return path
	}
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return &url.URL{}
	}
	cloned := *source
	return &cloned
}

func writeJSONStatic(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}
