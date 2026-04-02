package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegistryRouterRoutesRequestsByHostPrefix(t *testing.T) {
	t.Parallel()

	type observedRequest struct {
		Path  string
		Query string
	}

	var dockerhubSeen observedRequest
	var ghcrSeen observedRequest
	var quaySeen observedRequest

	dockerhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dockerhubSeen = observedRequest{Path: r.URL.Path, Query: r.URL.RawQuery}
		w.WriteHeader(http.StatusOK)
	}))
	defer dockerhub.Close()

	ghcr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ghcrSeen = observedRequest{Path: r.URL.Path, Query: r.URL.RawQuery}
		w.WriteHeader(http.StatusOK)
	}))
	defer ghcr.Close()

	quay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quaySeen = observedRequest{Path: r.URL.Path, Query: r.URL.RawQuery}
		w.WriteHeader(http.StatusOK)
	}))
	defer quay.Close()

	cfg := Config{
		Upstreams: []UpstreamTarget{
			{Host: "docker.io", DisplayName: "Docker Hub", BackendURL: dockerhub.URL, UpstreamHealthURL: dockerhub.URL + "/v2/", Default: true},
			{Host: "ghcr.io", DisplayName: "GHCR", BackendURL: ghcr.URL, UpstreamHealthURL: ghcr.URL + "/v2/"},
			{Host: "quay.io", DisplayName: "Quay", BackendURL: quay.URL, UpstreamHealthURL: quay.URL + "/v2/"},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewRegistryRouter(cfg, logger)
	if err != nil {
		t.Fatalf("NewRegistryRouter() error = %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	cases := []struct {
		name         string
		path         string
		targetHost   string
		wantPath     string
		wantRawQuery string
	}{
		{
			name:         "docker hub default route",
			path:         "/v2/library/hello-world/manifests/latest?ns=docker.io",
			targetHost:   "docker.io",
			wantPath:     "/v2/library/hello-world/manifests/latest",
			wantRawQuery: "ns=docker.io",
		},
		{
			name:       "ghcr host-prefixed route",
			path:       "/v2/ghcr.io/pterodactyl/yolks/manifests/java_21",
			targetHost: "ghcr.io",
			wantPath:   "/v2/pterodactyl/yolks/manifests/java_21",
		},
		{
			name:       "quay host-prefixed route",
			path:       "/v2/quay.io/pterodactyl/yolks/manifests/java_11",
			targetHost: "quay.io",
			wantPath:   "/v2/pterodactyl/yolks/manifests/java_11",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s error = %v", tc.path, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d", tc.path, resp.StatusCode)
			}

			switch tc.targetHost {
			case "docker.io":
				if dockerhubSeen.Path != tc.wantPath || dockerhubSeen.Query != tc.wantRawQuery {
					t.Fatalf("dockerhub saw path=%q query=%q, want path=%q query=%q", dockerhubSeen.Path, dockerhubSeen.Query, tc.wantPath, tc.wantRawQuery)
				}
			case "ghcr.io":
				if ghcrSeen.Path != tc.wantPath || ghcrSeen.Query != tc.wantRawQuery {
					t.Fatalf("ghcr saw path=%q query=%q, want path=%q query=%q", ghcrSeen.Path, ghcrSeen.Query, tc.wantPath, tc.wantRawQuery)
				}
			case "quay.io":
				if quaySeen.Path != tc.wantPath || quaySeen.Query != tc.wantRawQuery {
					t.Fatalf("quay saw path=%q query=%q, want path=%q query=%q", quaySeen.Path, quaySeen.Query, tc.wantPath, tc.wantRawQuery)
				}
			}
		})
	}
}

func TestRegistryRouterHealthAndLegacyPing(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Upstreams: []UpstreamTarget{
			{Host: "docker.io", DisplayName: "Docker Hub", BackendURL: "http://dockerhub.invalid", UpstreamHealthURL: "https://registry-1.docker.io/v2/", Default: true},
			{Host: "ghcr.io", DisplayName: "GHCR", BackendURL: "http://ghcr.invalid", UpstreamHealthURL: "https://ghcr.io/v2/"},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewRegistryRouter(cfg, logger)
	if err != nil {
		t.Fatalf("NewRegistryRouter() error = %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	healthResp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	defer healthResp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(healthResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /healthz response: %v", err)
	}
	if got := payload["default"]; got != "docker.io" {
		t.Fatalf("healthz default = %v, want docker.io", got)
	}

	pingResp, err := http.Get(server.URL + "/v1/_ping")
	if err != nil {
		t.Fatalf("GET /v1/_ping error = %v", err)
	}
	defer pingResp.Body.Close()
	if pingResp.StatusCode != http.StatusOK {
		t.Fatalf("/v1/_ping status = %d, want 200", pingResp.StatusCode)
	}
	if got := pingResp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("/v1/_ping api header = %q, want registry/2.0", got)
	}
}
