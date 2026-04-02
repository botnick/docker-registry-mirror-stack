package main

import "testing"

func TestCanonicalRepoHelpers(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Upstreams: []UpstreamTarget{
			{Host: "docker.io", DisplayName: "Docker Hub", BackendURL: "http://dockerhub", UpstreamHealthURL: "https://registry-1.docker.io/v2/", Default: true},
			{Host: "ghcr.io", DisplayName: "GHCR", BackendURL: "http://ghcr", UpstreamHealthURL: "https://ghcr.io/v2/"},
			{Host: "quay.io", DisplayName: "Quay", BackendURL: "http://quay", UpstreamHealthURL: "https://quay.io/v2/"},
		},
	}

	if got := cfg.CanonicalRepo("ghcr.io", "pterodactyl/yolks"); got != "ghcr.io/pterodactyl/yolks" {
		t.Fatalf("CanonicalRepo() = %q, want %q", got, "ghcr.io/pterodactyl/yolks")
	}
	if got := cfg.NormalizeCanonicalRepo("library/hello-world"); got != "docker.io/library/hello-world" {
		t.Fatalf("NormalizeCanonicalRepo() = %q, want %q", got, "docker.io/library/hello-world")
	}

	target, repo := cfg.ResolveRepoTarget("quay.io/pterodactyl/yolks")
	if target.Host != "quay.io" || repo != "pterodactyl/yolks" {
		t.Fatalf("ResolveRepoTarget() = host=%q repo=%q", target.Host, repo)
	}

	target, repo = cfg.ResolveRepoTarget("library/alpine")
	if target.Host != "docker.io" || repo != "library/alpine" {
		t.Fatalf("ResolveRepoTarget(default) = host=%q repo=%q", target.Host, repo)
	}
}
