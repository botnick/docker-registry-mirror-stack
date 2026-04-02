package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type UpstreamTarget struct {
	Host               string `json:"host"`
	DisplayName        string `json:"display_name"`
	BackendURL         string `json:"backend_url"`
	UpstreamHealthURL  string `json:"upstream_health_url"`
	RegistryDataPath   string `json:"registry_data_path"`
	RegistryConfigPath string `json:"registry_config_path"`
	RegistryBinaryPath string `json:"registry_binary_path"`
	GCRequestFlag      string `json:"gc_request_flag"`
	GCActiveFlag       string `json:"gc_active_flag"`
	Default            bool   `json:"default"`
}

type UpstreamStatus struct {
	Host         string         `json:"host"`
	DisplayName  string         `json:"display_name"`
	Default      bool           `json:"default"`
	Registry     HealthProbe    `json:"registry"`
	Upstream     HealthProbe    `json:"upstream"`
	Storage      map[string]any `json:"storage"`
	GCPending    bool           `json:"gc_pending"`
	GCActive     bool           `json:"gc_active"`
	Healthy      bool           `json:"healthy"`
	Summary      string         `json:"summary"`
	CanonicalRef string         `json:"canonical_ref"`
}

func parseUpstreamTargets(value string) ([]UpstreamTarget, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	var targets []UpstreamTarget
	if err := json.Unmarshal([]byte(value), &targets); err != nil {
		return nil, fmt.Errorf("UPSTREAMS_JSON must be valid JSON: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("UPSTREAMS_JSON must include at least one target")
	}

	defaultCount := 0
	for index := range targets {
		targets[index].Host = strings.TrimSpace(strings.ToLower(targets[index].Host))
		targets[index].DisplayName = strings.TrimSpace(targets[index].DisplayName)
		targets[index].BackendURL = strings.TrimRight(strings.TrimSpace(targets[index].BackendURL), "/")
		targets[index].UpstreamHealthURL = strings.TrimRight(strings.TrimSpace(targets[index].UpstreamHealthURL), "/")
		targets[index].RegistryDataPath = strings.TrimSpace(targets[index].RegistryDataPath)
		targets[index].RegistryConfigPath = strings.TrimSpace(targets[index].RegistryConfigPath)
		targets[index].RegistryBinaryPath = strings.TrimSpace(targets[index].RegistryBinaryPath)
		targets[index].GCRequestFlag = strings.TrimSpace(targets[index].GCRequestFlag)
		targets[index].GCActiveFlag = strings.TrimSpace(targets[index].GCActiveFlag)
		if targets[index].DisplayName == "" {
			targets[index].DisplayName = targets[index].Host
		}
		if targets[index].Default {
			defaultCount++
		}
		if targets[index].Host == "" {
			return nil, fmt.Errorf("UPSTREAMS_JSON target[%d].host must not be empty", index)
		}
		if targets[index].BackendURL == "" {
			return nil, fmt.Errorf("UPSTREAMS_JSON target[%d].backend_url must not be empty", index)
		}
		if targets[index].UpstreamHealthURL == "" {
			return nil, fmt.Errorf("UPSTREAMS_JSON target[%d].upstream_health_url must not be empty", index)
		}
	}

	switch {
	case defaultCount == 0:
		targets[0].Default = true
	case defaultCount > 1:
		return nil, fmt.Errorf("UPSTREAMS_JSON must have only one default target")
	}

	for index := range targets {
		for other := range targets {
			if index != other && targets[index].Host == targets[other].Host {
				return nil, fmt.Errorf("UPSTREAMS_JSON contains duplicate host %q", targets[index].Host)
			}
		}
	}

	return targets, nil
}

func (c Config) DefaultTarget() UpstreamTarget {
	for _, target := range c.Upstreams {
		if target.Default {
			return target
		}
	}
	if len(c.Upstreams) > 0 {
		return c.Upstreams[0]
	}
	return UpstreamTarget{}
}

func (c Config) FindTargetByHost(host string) (UpstreamTarget, bool) {
	host = strings.TrimSpace(strings.ToLower(host))
	for _, target := range c.Upstreams {
		if target.Host == host {
			return target, true
		}
	}
	return UpstreamTarget{}, false
}

func (c Config) CanonicalRepo(host, repo string) string {
	repo = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(repo), "/"))
	if repo == "" {
		return ""
	}
	target, ok := c.FindTargetByHost(host)
	if !ok {
		target = c.DefaultTarget()
	}
	return target.Host + "/" + repo
}

func (c Config) ResolveRepoTarget(repo string) (UpstreamTarget, string) {
	repo = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(repo), "/"))
	if repo == "" {
		return c.DefaultTarget(), ""
	}
	for _, target := range c.Upstreams {
		prefix := target.Host + "/"
		if strings.HasPrefix(strings.ToLower(repo), prefix) {
			return target, strings.TrimPrefix(repo, prefix)
		}
	}
	defaultTarget := c.DefaultTarget()
	return defaultTarget, repo
}

func (c Config) NormalizeCanonicalRepo(repo string) string {
	target, upstreamRepo := c.ResolveRepoTarget(repo)
	return c.CanonicalRepo(target.Host, upstreamRepo)
}

func (c Config) UpstreamRepoPrefix(target UpstreamTarget) string {
	return target.Host + "/"
}

func (c Config) TargetHostList() []string {
	hosts := make([]string, 0, len(c.Upstreams))
	for _, target := range c.Upstreams {
		hosts = append(hosts, target.Host)
	}
	return hosts
}
