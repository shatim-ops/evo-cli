// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// unreachableIAM is a token provider behind a dead proxy: every call is
// counted and fails with API_UNREACHABLE.
type unreachableIAM struct{ n *atomic.Int32 }

func (u unreachableIAM) Token(context.Context) (auth.Token, error) {
	u.n.Add(1)
	return auth.Token{}, clierrors.New(clierrors.CodeAPIUnreachable, "failed to reach IAM endpoint", "")
}

// deadIAMRegistry: a project and a key are set, IAM does not answer.
func deadIAMRegistry(t *testing.T, f *apiFake) (*Registry, *atomic.Int32) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	n := &atomic.Int32{}
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:     unreachableIAM{n},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	}), n
}

// REL-9: deploy_plan с disk_gb < 0 — USAGE до токена: раньше сначала шёл
// обмен ключа, и за мёртвым прокси ответ был API_UNREACHABLE.
func TestRel9_DeployPlanNegativeDiskBeforeToken(t *testing.T) {
	f := &apiFake{}
	r, tokens := deadIAMRegistry(t, f)
	m, text, isErr := callNamed(t, r, "deploy_plan", map[string]any{"disk_gb": -5})
	if !isErr || m["reason"] != clierrors.CodeUsage {
		t.Fatalf("deploy_plan disk_gb=-5: %s", text)
	}
	if n := tokens.Load(); n != 0 {
		t.Errorf("%d token requests before the refusal", n)
	}
	if n := f.count(); n != 0 {
		t.Errorf("%d API requests before the refusal", n)
	}
}
