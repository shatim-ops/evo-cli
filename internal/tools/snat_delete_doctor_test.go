// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func TestSNATDeleteInterruptedKeepsDoctorEvidence(t *testing.T) {
	const gatewayID = "00000000-0000-4000-8000-0000000000c1"
	visible := true
	reads := 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		switch req.Method {
		case "GET":
			reads++
			if visible {
				fmt.Fprintf(rec, `{"items":[{"id":%q}],"total":1}`, gatewayID)
			} else {
				fmt.Fprint(rec, `{"items":[],"total":0}`)
			}
		case "DELETE":
			writes++
			cancel()
			return nil, ctx.Err()
		default:
			t.Fatalf("unexpected %s", req.Method)
		}
		return rec.Result(), nil
	})
	r, dir := interruptedDeleteRegistry(t, rt)
	t.Chdir(dir)
	tl, _ := r.Lookup("snat_delete")
	text, failed := tl.Handler(ctx, map[string]any{"id": gatewayID, "confirmed": true})
	if !failed || writes != 1 {
		t.Fatalf("writes=%d result=%s", writes, text)
	}
	pending := intent.Pending(intent.Dir(dir))
	if len(pending) != 1 || pending[0].Target != "id="+gatewayID || pending[0].Key == nil || pending[0].Key.KeyID != config.MaskKeyID(provisionCfg.KeyID) || !pending[0].Sent {
		t.Fatalf("pending=%+v", pending)
	}
	report := doctorOnFake(t, r)
	if writes != 1 || len(intent.Pending(intent.Dir(dir))) != 1 || !strings.Contains(report, gatewayID) {
		t.Fatalf("writes=%d doctor=%s", writes, report)
	}
	visible = false
	e, err := r.newEngine(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"different project", "different key", "legacy record"} {
		rec := pending[0]
		switch scope {
		case "different project":
			rec.ProjectID = "another"
		case "different key":
			rec.Key = &state.Maker{KeyID: "another"}
		case "legacy record":
			rec.ProjectID = ""
		}
		before := reads
		_, settled, _ := r.checkPending(t.Context(), rec, e)
		if settled || reads != before {
			t.Errorf("%s: settled=%v reads=%d", scope, settled, reads-before)
		}
	}
	report = doctorOnFake(t, r)
	if writes != 1 || len(intent.Pending(intent.Dir(dir))) != 0 || !strings.Contains(report, "not acknowledged") {
		t.Fatalf("writes=%d doctor=%s", writes, report)
	}
}

func TestSNATDeleteIncompleteSecondPage(t *testing.T) {
	const target = "00000000-0000-4000-8000-0000000000c1"
	for _, second := range []string{`{"items":[]}`, `[]`, `[{"id":"other"}]`, `{"items":[],"total":1}`} {
		t.Run(second, func(t *testing.T) {
			partial := true
			reads, writes := 0, 0
			after := second
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				if req.Method == "DELETE" {
					writes++
					partial = true
					return nil, errors.New("read tcp: connection reset by peer")
				}
				if req.Method != "GET" || req.URL.Path != "/api/v1/nat-gateways" || req.URL.Query().Get("project_id") != provisionCfg.ProjectID {
					t.Fatalf("unexpected %s %s", req.Method, req.URL)
				}
				reads++
				switch {
				case !partial:
					fmt.Fprintf(rec, `{"items":[{"id":%q}],"total":1}`, target)
				case req.URL.Query().Get("offset") == "":
					fmt.Fprint(rec, `{"items":[{"id":"other"}],"total":2}`)
				case req.URL.Query().Get("offset") == "1":
					fmt.Fprint(rec, after)
				default:
					t.Fatal(req.URL)
				}
				return rec.Result(), nil
			})
			r, dir := interruptedDeleteRegistry(t, rt)
			t.Chdir(dir)
			tool, _ := r.Lookup("snat_delete")
			for _, consent := range []bool{false, true} {
				before := reads
				out, failed := tool.Handler(t.Context(), map[string]any{"id": target, "confirmed": consent})
				if !failed || !strings.Contains(out, "API_ERROR") || writes != 0 || reads-before != 2 || len(intent.Pending(intent.Dir(dir))) != 0 {
					t.Fatalf("preflight: %s reads=%d writes=%d", out, reads-before, writes)
				}
			}
			partial = false
			before := reads
			out, failed := tool.Handler(t.Context(), map[string]any{"id": target, "confirmed": true})
			if !failed || !strings.Contains(out, "API_UNREACHABLE") || !strings.Contains(out, "API_ERROR") || writes != 1 || reads-before != 3 || len(intent.Pending(intent.Dir(dir))) != 1 {
				t.Fatalf("lost: %s reads=%d writes=%d", out, reads-before, writes)
			}
			before = reads
			report := doctorOnFake(t, r)
			if reads-before != 2 || writes != 1 || len(intent.Pending(intent.Dir(dir))) != 1 || !strings.Contains(report, "API_ERROR") {
				t.Fatalf("incomplete: %s", report)
			}
			after = `[{"id":"another"}]`
			report = doctorOnFake(t, r)
			if writes != 1 || len(intent.Pending(intent.Dir(dir))) != 0 || !strings.Contains(report, "not acknowledged") {
				t.Fatalf("complete: %s", report)
			}
		})
	}
}
