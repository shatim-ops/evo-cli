// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Один и тот же порт в списке открывается дважды: после успешного POST
// он не попадает в already_open, и правило создаётся ещё раз.
func TestBreakR4_R4_19_DuplicateOpenPortOnce(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rules"):
			posts++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"00000000-0000-4000-8000-0000000000c1","direction":"ingress","port_range":"22:22"}`))
		case strings.Contains(r.URL.Path, "/rules"):
			_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		default:
			_, _ = w.Write([]byte(`{"items":[{"id":"00000000-0000-4000-8000-0000000000b1","name":"web"}],"total":1}`))
		}
	}))
	defer srv.Close()

	reg := New(func(map[string]any) (*engine.Engine, error) {
		cloud := provision.NewCloudRuAdapter(srv.Client(), srv.URL)
		cloud.SetToken("a.eyJzdWIiOiJ4In0.c")
		cloud.Backoff = time.Millisecond
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: contractID},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: t.TempDir(),
		}, nil
	})
	tl, ok := reg.Lookup("open_ports")
	if !ok {
		t.Fatal("no open_ports")
	}
	text, isErr, pan := contractCall(tl, map[string]any{
		"security_group": "web",
		"ports":          []any{float64(22), float64(22)},
		"confirmed":      true,
	}, time.Second)
	if pan != "" || isErr {
		t.Fatalf("open_ports: %s %s", pan, text)
	}
	if posts != 1 {
		t.Fatalf("ports [22, 22] created %d rules, want 1; %s", posts, text)
	}
}
