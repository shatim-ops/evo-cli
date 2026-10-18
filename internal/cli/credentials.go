// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// resolveConfig resolves the configuration for the selected profile and
// prints its warnings (deprecated variables, loose file modes) to stderr.
func resolveConfig(p *output.Printer, flags map[string]any) (config.Config, error) {
	cfg, _, err := resolveConfigWithSources(p, flags)
	return cfg, err
}

// resolveConfigWithSources is resolveConfig that also reports where each
// value came from.
func resolveConfigWithSources(p *output.Printer, flags map[string]any) (config.Config, []config.Source, error) {
	cfg, sources, err := config.ResolveWithSources(config.Options{
		Profile:     flagProfile,
		ProjectFile: projectConfigPath(),
		Flags:       flags,
	})
	if err != nil {
		return config.Config{}, nil, err
	}
	printWarnings(p, cfg.Warnings)
	return cfg, sources, nil
}

func sourceOrigin(sources []config.Source, key string) string {
	for _, s := range sources {
		if s.Key == key {
			return s.Origin
		}
	}
	return config.OriginDefault
}

var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

// printWarnings prints each warning once per process: the MCP server
// resolves the configuration on every tool call. Warnings go to stderr,
// with --json as {"warning": "..."} lines: both streams stay parseable.
func printWarnings(p *output.Printer, warnings []string) {
	warnedMu.Lock()
	defer warnedMu.Unlock()
	for _, w := range warnings {
		if warned[w] {
			continue
		}
		warned[w] = true
		p.Warning(w)
	}
}

var (
	providersMu sync.Mutex
	providers   = map[string]*auth.IAMProvider{}
)

// tokenProvider returns the process-wide token provider of the resolved
// credentials. The MCP server builds an Engine per tool call; sharing the
// provider keeps the IAM exchange to one per token lifetime, and the file
// cache extends that across CLI runs.
func tokenProvider(cfg config.Config) *auth.IAMProvider {
	cachePath := config.TokenCachePath(cfg.Profile)
	key := fmt.Sprintf("%s\x00%s\x00%x", cachePath, cfg.KeyID, sha256.Sum256([]byte(cfg.Secret)))
	providersMu.Lock()
	defer providersMu.Unlock()
	if p, ok := providers[key]; ok {
		return p
	}
	var cache auth.TokenCache // no cache directory (no HOME): every run asks IAM
	if cachePath != "" {
		cache = auth.FileCache{Path: cachePath}
	}
	p := auth.NewIAMProvider(
		auth.Credentials{KeyID: cfg.KeyID, Secret: cfg.Secret},
		httpClient,
		cache,
	)
	providers[key] = p
	return p
}
