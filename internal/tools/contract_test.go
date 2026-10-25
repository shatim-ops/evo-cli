// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const contractID = "00000000-0000-4000-8000-000000000001"

// contractCallLimit is the context of one call of the contract tests: a
// call that waits longer waits on a clock of its own (minio's retries,
// s3_retry_test.go).
const contractCallLimit = 400 * time.Millisecond

// contractRT records every request and answers with one fixed status.
// Retry-After: 0 so httpretry does not sleep (its cap is 30s).
type contractRT struct {
	mu   sync.Mutex
	reqs []string
	kind string
	// body replaces the answer of kind "200" when set.
	body string
}

func (c *contractRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
	}
	c.mu.Lock()
	c.reqs = append(c.reqs, r.Method+" "+r.URL.Host+r.URL.Path)
	kind, override := c.kind, c.body
	c.mu.Unlock()
	if kind == "conn" {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	hdr := http.Header{"Content-Type": {"application/json"}, "Retry-After": {"0"}}
	body := `{"error":"refused"}`
	code := 200
	switch kind {
	case "html":
		hdr.Set("Content-Type", "text/html")
		body = "<html><body>gateway</body></html>"
	case "empty":
		body = ""
	default:
		code, _ = strconv.Atoi(kind)
		if code == 0 {
			code = 500
		}
		if code == 200 {
			body = `{"items":[],"total":0,"id":"` + contractID + `","name":"web","done":true,"status":"ACTIVE"}`
			if override != "" {
				body = override
			}
		}
	}
	return &http.Response{
		StatusCode: code,
		Status:     strconv.Itoa(code) + " status",
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func (c *contractRT) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.reqs
	c.reqs = nil
	return out
}

func contractWrites(reqs []string) int {
	n := 0
	for _, q := range reqs {
		if !strings.HasPrefix(q, "GET ") && !strings.HasPrefix(q, "HEAD ") {
			n++
		}
	}
	return n
}

func contractReg(t *testing.T, rt *contractRT) *Registry {
	t.Helper()
	return contractRegCfg(t, rt, nil)
}

// contractRegCfg is contractReg with the configuration changed by edit
// (nil keeps it).
func contractRegCfg(t *testing.T, rt *contractRT, edit func(*config.Config)) *Registry {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("EVO_LANG", "en")
	old := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = old })
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := rt.RoundTrip(r)
		if err != nil {
			http.Error(w, "conn", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(s3.Close)
	dir := t.TempDir()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
	cloud.Backoff = time.Millisecond
	cloud.SetToken("a.eyJzdWIiOiJ4In0.c")
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		cfg := config.Config{
			ProjectID: contractID, Region: contractID, ImageID: contractID,
			KeyID: "k", Secret: "s",
			S3: config.S3Config{Endpoint: s3.URL, AccessKey: "a", SecretKey: "b"},
		}
		if edit != nil {
			edit(&cfg)
		}
		return &engine.Engine{
			Config:   cfg,
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings: engine.Timings{
				VMPollInterval: time.Millisecond, VMPollTimeout: 5 * time.Millisecond,
				VMProgressInterval: time.Hour, SSHRetryInterval: time.Millisecond,
				FIPPollInterval: time.Millisecond, DestroyPollInterval: time.Millisecond,
			},
		}, nil
	})
}

func contractCall(tl Tool, args map[string]any, limit time.Duration) (text string, isErr bool, pan string) {
	defer func() {
		if p := recover(); p != nil {
			pan = "panic"
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	text, isErr = tl.Handler(ctx, args)
	return text, isErr, ""
}

func contractReason(text string) (reason, status string) {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return "", ""
	}
	reason, _ = m["reason"].(string)
	status, _ = m["status"].(string)
	return reason, status
}

func schemaRequired(tl Tool) []string {
	switch r := tl.InputSchema["required"].(type) {
	case []string:
		return append([]string(nil), r...)
	case []any:
		var out []string
		for _, n := range r {
			if s, ok := n.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func schemaProps(tl Tool) map[string]any {
	props, _ := tl.InputSchema["properties"].(map[string]any)
	return props
}

// contractArgs fills the schema with values that pass the type check.
// Names are "web": a UUID is rejected by the Cloud.ru name rule before the gate.
func contractArgs(tl Tool) map[string]any {
	args := map[string]any{}
	for k, raw := range schemaProps(tl) {
		p, _ := raw.(map[string]any)
		if enum, ok := p["enum"].([]any); ok && len(enum) > 0 {
			args[k] = enum[0]
			continue
		}
		switch p["type"] {
		case "boolean":
			args[k] = false
		case "integer", "number":
			if strings.Contains(k, "port") {
				args[k] = float64(8080)
			} else if strings.Contains(k, "size") || strings.HasSuffix(k, "_gb") {
				args[k] = float64(10)
			} else {
				args[k] = float64(2)
			}
		case "array":
			args[k] = []any{float64(80)}
		case "object":
			args[k] = map[string]any{"name": "web"}
		default:
			switch {
			case p["format"] == "cidr":
				args[k] = "10.0.0.0/16"
			case k == "job_id":
				args[k] = "stk-0123456789"
			case k == "plan_id":
				args[k] = "plan-0123456789ab"
			case strings.Contains(k, "base64"):
				args[k] = "YQ=="
			case strings.Contains(k, "url"):
				args[k] = "https://example.invalid/chart"
			case strings.Contains(k, "name") || k == "bucket" || k == "key" || k == "path":
				args[k] = "web"
			case k == "args":
				args[k] = "delete pod web"
			default:
				args[k] = contractID
			}
		}
	}
	if tl.Name == "k8s_kubectl" {
		args["args"] = "delete pod web"
	}
	if tl.Name == "k8s_helm" {
		args["action"] = "install"
		args["repo_url"] = "https://example.invalid/chart"
		args["release"] = "web"
		args["chart"] = "web"
	}
	delete(args, "confirmed")
	return args
}

// A tool with required arguments answers USAGE for {} and for
// {"confirmed": true}, and the transport is not used. docs/mcp.md
// «Ошибки»: USAGE — аргументы отвергнуты, в облако ничего не ушло.
func TestContract_EmptyRequiredIsUsage(t *testing.T) {
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	nTools, nRequired := 0, 0
	for _, tl := range reg.Tools() {
		nTools++
		req := schemaRequired(tl)
		if len(req) == 0 {
			continue
		}
		nRequired++
		for _, args := range []map[string]any{nil, {"confirmed": true}} {
			rt.take()
			text, isErr, pan := contractCall(tl, args, 200*time.Millisecond)
			n := len(rt.take())
			reason, _ := contractReason(text)
			if pan != "" || !isErr || reason != clerrors.CodeUsage || n != 0 {
				t.Errorf("%s args=%v panic=%s err=%v reason=%s reqs=%d text=%.180s", tl.Name, args, pan, isErr, reason, n, text)
			}
		}
	}
	t.Logf("tools=%d with required=%d", nTools, nRequired)
	if nTools < 100 || nRequired < 50 {
		t.Fatalf("registry too small: tools=%d with required=%d", nTools, nRequired)
	}
}

// Bad confirmed values are USAGE before the handler (docs/mcp.md: a scalar
// of the wrong type is USAGE). Absent and null confirmed do not write.
// k8s_kubectl without a writing verb is a read and is not in this loop's
// "delete" arguments. cert_private_key and secret_value are not Mutating:
// they ask consent because they reveal a secret (sensitiveTools).
func TestContract_GateNoWrite(t *testing.T) {
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	var mutating int
	for _, tl := range reg.Tools() {
		if !tl.Mutating {
			continue
		}
		mutating++
		for _, v := range []any{"true", float64(1), "yes", []any{}} {
			rt.take()
			text, _, pan := contractCall(tl, map[string]any{"confirmed": v}, 200*time.Millisecond)
			n := len(rt.take())
			reason, _ := contractReason(text)
			if pan != "" || reason != clerrors.CodeUsage || n != 0 {
				t.Errorf("%s confirmed=%v panic=%s reason=%s reqs=%d text=%.160s", tl.Name, v, pan, reason, n, text)
			}
		}
		args := contractArgs(tl)
		for _, label := range []string{"absent", "null"} {
			call := args
			if label == "null" {
				call = make(map[string]any, len(args)+1)
				for k, v := range args {
					call[k] = v
				}
				call["confirmed"] = nil
			}
			rt.take()
			text, _, pan := contractCall(tl, call, 200*time.Millisecond)
			reqs := rt.take()
			reason, status := contractReason(text)
			if pan != "" || contractWrites(reqs) != 0 {
				t.Errorf("%s %s panic=%s writes=%d reason=%s status=%s text=%.160s", tl.Name, label, pan, contractWrites(reqs), reason, status, text)
			}
		}
	}
	t.Logf("mutating=%d", mutating)
	if mutating < 40 {
		t.Fatalf("mutating tools: %d", mutating)
	}
}

// The second half of the gate: a write (anything but GET/HEAD) without
// confirmed is a defect. Kept separate so the type sweep stays obviously green.
func TestContract_GatePreviewDoesNotWrite(t *testing.T) {
	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	for _, tl := range reg.Tools() {
		if !tl.Mutating {
			continue
		}
		args := contractArgs(tl)
		for _, label := range []string{"absent", "null"} {
			call := args
			if label == "null" {
				call = make(map[string]any, len(args)+1)
				for k, v := range args {
					call[k] = v
				}
				call["confirmed"] = nil
			}
			rt.take()
			text, isErr, pan := contractCall(tl, call, 250*time.Millisecond)
			reqs := rt.take()
			reason, status := contractReason(text)
			// already_deleted is no error and no write: a delete of what the
			// cloud does not have answers so already in the preview (D26).
			if pan != "" || contractWrites(reqs) != 0 || !isErr && status != "already_deleted" {
				t.Errorf("%s %s panic=%s writes=%d err=%v reason=%s status=%s text=%.180s", tl.Name, label, pan, contractWrites(reqs), isErr, reason, status, text)
			}
			// Не confirmation_required, но и не запись — так задумано:
			// USAGE до облака (имя, enum); NOT_FOUND родителя уже на превью;
			// already_deleted — удаление того, чего нет (D26);
			// need_specification — меню спецификаций, только GET.
		}
	}
}

// required ⊂ properties; every property has a type the checker understands;
// confirmed is on every mutating tool. The only non-mutating tools with
// confirmed are sensitiveTools (cert_private_key, secret_value): they reveal
// a secret (internal/tools/registry.go). Annotations are TestToolsList_Annotations.
func TestContract_Schema(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	known := map[string]bool{"string": true, "integer": true, "number": true, "boolean": true, "array": true, "object": true}
	for _, tl := range reg.Tools() {
		props := schemaProps(tl)
		for _, name := range schemaRequired(tl) {
			if _, ok := props[name]; !ok {
				t.Errorf("%s: required %q is not a property", tl.Name, name)
			}
		}
		for name, raw := range props {
			p, _ := raw.(map[string]any)
			typ, _ := p["type"].(string)
			if !known[typ] {
				t.Errorf("%s: property %s type %q", tl.Name, name, typ)
			}
			if typ == "array" {
				items, _ := p["items"].(map[string]any)
				it, _ := items["type"].(string)
				if it != "" && !known[it] {
					t.Errorf("%s: %s items type %q", tl.Name, name, it)
				}
			}
		}
		_, has := props["confirmed"]
		if tl.Mutating && !has {
			t.Errorf("%s: mutating without confirmed", tl.Name)
		}
		if !tl.Mutating && has && !IsSensitive(tl) {
			t.Errorf("%s: read-only tool has confirmed", tl.Name)
		}
		if IsSensitive(tl) && (!has || tl.Mutating) {
			t.Errorf("%s: sensitive tool must take confirmed and not be mutating", tl.Name)
		}
	}
}

// HTTP statuses the sweep feeds to every tool that actually calls the cloud.
// Shell tools are not this surface.
var contractHTTPSkip = map[string]bool{"k8s_kubectl": true, "k8s_apply": true, "k8s_helm": true}

func TestContract_APIStatusReason(t *testing.T) {
	kinds := []string{"400", "401", "403", "404", "409", "422", "429", "500", "502", "html", "empty", "conn"}
	rt := &contractRT{}
	reg := contractReg(t, rt)
	var reached, skipped int
	for _, tl := range reg.Tools() {
		if contractHTTPSkip[tl.Name] {
			skipped++
			continue
		}
		args := contractArgs(tl)
		if tl.Mutating {
			args["confirmed"] = true
		}
		for _, kind := range kinds {
			rt.kind = kind
			rt.take()
			limit := contractCallLimit
			if kind == "conn" {
				limit = 40 * time.Millisecond
			}
			text, isErr, pan := contractCall(tl, args, limit)
			reqs := rt.take()
			if pan != "" {
				t.Errorf("%s %s panicked", tl.Name, kind)
				continue
			}
			if len(reqs) == 0 {
				continue
			}
			if tl.Name == "org_projects" && strings.Contains(text, "Not an error") {
				continue // BACKLOG [02]: 400 списка проектов у сервисного аккаунта — не ошибка вызова.
			}
			if tl.Name == "org_quotas" && kind == "403" && strings.Contains(text, "quota") {
				continue // нет права на квоты организации: ответ без isError, причина текстом.
			}
			if (tl.Name == "whoami" || tl.Name == "doctor") && !isErr {
				continue // whoami: без списка проектов число неизвестно, поля visible_projects нет; doctor: любой ответ API — проверка (хост доступен, key scope — skip), а не ошибка вызова.
			}
			if tl.Name == "metrics_scrape" && (kind == "html" || kind == "empty") {
				continue // тело scrape — текст Prometheus, не JSON API.
			}
			if contractAPIBreak[tl.Name+" "+kind] {
				continue // пустой 2xx — успех по построению, см. contractAPIBreak.
			}
			reached++
			if _, del := deleteTargets[tl.Name]; del && kind == "404" && !isErr {
				// A 404 on the delete target is a done delete (D26); a 404 on
				// the parent stays NOT_FOUND and takes the check below.
				if !strings.Contains(text, `"status": "already_deleted"`) {
					t.Errorf("%s 404 success that is not already_deleted: %.200s", tl.Name, text)
				}
				continue
			}
			reason, _ := contractReason(text)
			if !isErr || reason == "" || reason == clerrors.ReasonGeneral {
				t.Errorf("%s %s reason=%q err=%v reqs=%v text=%.200s", tl.Name, kind, reason, isErr, reqs, text)
				continue
			}
			if !contractReasonOK(kind, reqs, text, reason) {
				t.Errorf("%s %s reason=%s want per exit-codes reqs=%v text=%.200s", tl.Name, kind, reason, reqs, text)
			}
			// A refusal of rights says so in fields of its own, whatever the
			// service; its reason stays (A10, decision 21.10).
			if kind == "403" {
				var m map[string]any
				_ = json.Unmarshal([]byte(text), &m)
				if m["access_denied"] != true || m["http_status"] != float64(403) {
					t.Errorf("%s 403: no access_denied/http_status: %.300s", tl.Name, text)
				}
			}
		}
	}
	t.Logf("reached=%d shell_skipped=%d", reached, skipped)
	if reached < 50 {
		t.Fatalf("only %d tool/status pairs reached the transport (skipped shell %d)", reached, skipped)
	}
}

// contractAPIBreak are tool/status pairs left out of
// TestContract_APIStatusReason by design (break r3b, R3B-1/R3B-2 fixed: an
// HTML 2xx is a 502 in httpretry and the compute adapter, a cut answer is
// API_ERROR). An empty 2xx stays a success: for a delete, start or stop it
// is a valid HTTP answer, and S3 answers PUT, CreateBucket, DELETE and an
// empty object without a body. These calls work live (run 18.10); reading
// an empty answer as a failure would break them on a body nobody uses.
var contractAPIBreak = map[string]bool{}

func init() {
	for _, name := range []string{
		"image_delete", "disk_delete", "k8s_delete", "ca_delete", "ca_start", "ca_stop",
		"snat_delete", "pg_db_create", "pg_db_delete", "pg_user_create", "pg_user_delete",
		"secret_create", "secret_add_version", "secret_delete", "cert_delete",
		"s3_put", "s3_bucket_create", "s3_rm", "s3_bucket_delete", "s3_get",
	} {
		contractAPIBreak[name+" empty"] = true
	}
}

func contractReasonOK(kind string, reqs []string, text, reason string) bool {
	compute := true
	for _, q := range reqs {
		if !strings.Contains(q, "compute.api.cloud.ru") {
			compute = false
		}
	}
	if strings.Contains(text, "dbaas API") || strings.Contains(text, "vpc API") || strings.Contains(text, "nlb API") {
		compute = false
	}
	switch kind {
	case "404":
		return reason == clerrors.CodeNotFound || reason == clerrors.CodeVMNotFound
	case "429":
		return reason == clerrors.CodeAPIRateLimited
	case "401":
		if compute {
			return reason == clerrors.CodeAuthExpired
		}
		return reason == clerrors.CodeAPIError || reason == clerrors.CodeAuthExpired || reason == clerrors.CodeAuthFailed
	case "403":
		if compute {
			return reason == clerrors.CodeAuthFailed
		}
		// docs/exit-codes.md: 403 from vpc, dns, … is still 5; their clients
		// do not distinguish the status.
		return reason == clerrors.CodeAPIError || reason == clerrors.CodeAuthFailed
	case "400", "409", "422", "500", "502":
		return reason == clerrors.CodeAPIError
	case "html", "empty":
		return reason == clerrors.CodeAPIError || reason == clerrors.CodeAPIUnreachable
	case "conn":
		return reason == clerrors.CodeAPIUnreachable
	}
	return false
}
