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
	cfg, err := configOf(flags)
	if err != nil {
		return config.Config{}, err
	}
	printWarnings(p, cfg.Warnings)
	return cfg, nil
}

// configOf resolves the configuration for the selected profile: the
// profile, ./.evo.yaml, the environment and flags.
func configOf(flags map[string]any) (config.Config, error) {
	cfg, _, err := config.ResolveWithSources(config.Options{
		Profile:     flagProfile,
		ProjectFile: projectConfigPath(),
		Flags:       flags,
	})
	return cfg, err
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

// providers hold the token providers for the life of the process: the
// CLI, evo mcp over stdio and over HTTP all build their engines through
// tokenProvider (buildEngine). auth keeps only weak references to the
// providers it renews a refused token with (httpretry.OnTokenRenewed): a
// provider no one holds here would be collected in the middle of a wait,
// and the wait would end on the 401 the renewal is there to absorb
// (TestTokenProvider_LivesForTheProcess).
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
