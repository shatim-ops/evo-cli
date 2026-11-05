// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package iamadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// The world of the Administration API, from its public specification
// (iam-gw-public.yaml): the answers of the spec in camelCase, or — snake
// — in snake_case, as the live API answered (IAM-2). No network.
const (
	project = "00000000-0000-4000-8000-0000000000a1"
	saID    = "00000000-0000-4000-8000-0000000000c1"
	userID  = "00000000-0000-4000-8000-0000000000e1"
	secret  = "SECRET-VALUE-MUST-NOT-LEAVE"
	token   = "TOKEN-VALUE-MUST-NOT-LEAVE"
)

type world struct {
	snake  bool
	status map[string]int // path → status to answer instead
	pages  int            // pages of every list (default 1)
	mu     sync.Mutex
	asked  []*http.Request
	bodies []string
}

type staticToken struct{}

func (staticToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{Value: token, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (w *world) client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(srv.Close)
	c := New(staticToken{}, srv.URL)
	// The retries of httpretry, without its production waits.
	c.http.Transport.(*httpretry.Transport).Backoff = time.Millisecond
	return c
}

// key turns a camelCase key of the spec into the live API's snake_case.
func (w *world) key(k string) string {
	if !w.snake {
		return k
	}
	var b strings.Builder
	for _, r := range k {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (w *world) obj(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		m[w.key(kv[i].(string))] = kv[i+1]
	}
	return m
}

// page answers page n (pageToken "p<n>") of a list of items under list,
// with the next token until the last page.
func (w *world) page(r *http.Request, list string, item func(n int) map[string]any) map[string]any {
	pages := max(w.pages, 1)
	n := 0
	if tok := r.URL.Query().Get("pageToken"); tok != "" {
		fmt.Sscanf(tok, "p%d", &n)
	}
	out := w.obj(list, []any{item(n)})
	if n+1 < pages {
		out[w.key("nextPageToken")] = fmt.Sprintf("p%d", n+1)
	}
	return out
}

func (w *world) serve(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	w.asked = append(w.asked, r)
	w.bodies = append(w.bodies, string(body))
	w.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+token {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	if st, ok := w.status[r.URL.Path]; ok {
		rw.WriteHeader(st)
		fmt.Fprintf(rw, `{"code":%d,"message":"refused by the world"}`, st)
		return
	}
	var out any
	switch p := r.URL.Path; {
	case r.Method == http.MethodGet && p == "/api/v1/roles":
		out = w.page(r, "roles", func(n int) map[string]any {
			return w.obj("id", fmt.Sprintf("role-%d", n), "name", "platform.project.admin", "displayName", "Администратор проекта",
				"type", "ROLE_TYPE_PRODUCT", "enabled", true, "description", "free text", "actions", []any{"a.b.c"}, "customerId", "c")
		})
	case r.Method == http.MethodGet && p == "/api/v1/actions":
		out = w.page(r, "actions", func(n int) map[string]any {
			return w.obj("name", fmt.Sprintf("iam.serviceAccounts.create.%d", n), "productCode", "iam", "groups", []any{"g"}, "description", "free text")
		})
	case r.Method == http.MethodGet && p == "/api/v3/permissions":
		out = w.page(r, "permissions", func(n int) map[string]any {
			return w.obj("id", fmt.Sprintf("perm-%d", n), "roleName", "platform.project.admin", "primaryRoleId", "r-uuid",
				"roleId", "platform.project.admin", "resourceId", project, "subjectId", saID, "subjectType", "service_account",
				"expiresAt", "2026-12-05T00:00:00Z", "enabled", true, "roleType", "ROLE_TYPE_PRODUCT")
		})
	case r.Method == http.MethodGet && p == "/api/v1/permissions/evaluate":
		out = w.obj("accessGranted", r.URL.Query().Get("action") == "granted")
	case r.Method == http.MethodPost && p == "/api/v1/permissions/bulk-evaluate":
		var req struct {
			Requests []map[string]any `json:"requests"`
		}
		_ = json.Unmarshal(body, &req)
		var results []any
		// Answered in reverse: the client matches results by the request
		// each one echoes, not by position.
		for i := len(req.Requests) - 1; i >= 0; i-- {
			q := req.Requests[i]
			results = append(results, w.obj("accessGranted", q["action"] == "granted", "request", q))
		}
		out = w.obj("results", results)
	case r.Method == http.MethodGet && p == "/api/v2/serviceAccounts":
		out = w.page(r, "serviceAccounts", func(n int) map[string]any {
			return w.obj("id", saID, "name", fmt.Sprintf("evo-%d", n), "enabled", true, "type", "regular",
				"email", "sa@example.invalid", "description", "free text")
		})
	case r.Method == http.MethodGet && p == "/api/v2/serviceAccounts/"+saID+"/accessKeys":
		out = w.obj("accessKeys", []any{w.obj("id", "ak-1", "keyId", "KEYID1", "secret", secret, "description", "evo login",
			"expiredAt", "2026-12-05T00:00:00Z", "createdAt", "2026-11-05T00:00:00Z", "serviceAccountId", saID)})
	default:
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(rw).Encode(out)
}

func (w *world) last() (*http.Request, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.asked[len(w.asked)-1], w.bodies[len(w.bodies)-1]
}

func both(t *testing.T, f func(t *testing.T, w *world)) {
	for _, snake := range []bool{false, true} {
		t.Run(map[bool]string{false: "spec camelCase", true: "live snake_case"}[snake], func(t *testing.T) {
			f(t, &world{snake: snake})
		})
	}
}

// noLeak: neither the secret of a key nor the token reaches what the
// client returns (R-43: keys without secrets).
func noLeak(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if s := string(b); strings.Contains(s, secret) || strings.Contains(s, token) {
		t.Fatalf("a secret left the client: %s", s)
	}
}

func TestRoles_WhitelistBothSpellingsEveryPage(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		w.pages = 3
		roles, err := w.client(t).Roles(context.Background(), RoleFilter{Type: "ROLE_TYPE_PRODUCT", Name: "platform.project.admin"})
		if err != nil {
			t.Fatal(err)
		}
		if len(roles) != 3 || roles[2] != (Role{ID: "role-2", Name: "platform.project.admin", DisplayName: "Администратор проекта", Type: "ROLE_TYPE_PRODUCT", Enabled: true}) {
			t.Fatalf("roles %+v", roles)
		}
		r, _ := w.last()
		if got := r.URL.Query().Get("filter"); got != "type=ROLE_TYPE_PRODUCT AND name=platform.project.admin" {
			t.Errorf("filter %q", got)
		}
	})
}

func TestActions(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		acts, err := w.client(t).Actions(context.Background(), "iam")
		if err != nil {
			t.Fatal(err)
		}
		if len(acts) != 1 || acts[0].Name != "iam.serviceAccounts.create.0" || acts[0].ProductCode != "iam" {
			t.Fatalf("actions %+v", acts)
		}
		r, _ := w.last()
		if got := r.URL.Query().Get("filter"); got != "product_code=iam" {
			t.Errorf("filter %q", got)
		}
	})
}

func TestPermissions_BySubjectOnTheResource(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		w.pages = 2
		perms, err := w.client(t).Permissions(context.Background(), project, saID)
		if err != nil {
			t.Fatal(err)
		}
		want := Permission{ID: "perm-1", RoleName: "platform.project.admin", RoleID: "r-uuid", ResourceID: project,
			SubjectID: saID, SubjectType: "service_account", ExpiresAt: "2026-12-05T00:00:00Z", Enabled: true}
		if len(perms) != 2 || perms[1] != want {
			t.Fatalf("permissions %+v, want the second %+v", perms, want)
		}
		r, _ := w.last()
		if got := r.URL.Query().Get("filter"); got != "resource_id="+project+" AND subject_id="+saID {
			t.Errorf("filter %q: resource_id is required, the subject narrows it", got)
		}
	})
}

func TestEvaluate(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		c := w.client(t)
		for action, want := range map[string]bool{"granted": true, "denied": false} {
			got, err := c.Evaluate(context.Background(), Check{Action: action, ResourceID: project, ServiceAccountID: saID})
			if err != nil || got != want {
				t.Fatalf("%s: %v, %v", action, got, err)
			}
			r, _ := w.last()
			q := r.URL.Query()
			if q.Get("action") != action || q.Get("resourceId") != project || q.Get("serviceAccountId") != saID || q.Has("userId") {
				t.Errorf("query %s", r.URL.RawQuery)
			}
		}
	})
}

func TestBulkEvaluate_MatchedByTheEchoedRequest(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		checks := []Check{{Action: "granted", ResourceID: project}, {Action: "denied", ResourceID: project}, {Action: "granted", ResourceID: saID}}
		got, err := w.client(t).BulkEvaluate(context.Background(), checks)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || !got[0] || got[1] || !got[2] {
			t.Fatalf("results %v, want true false true in the order asked", got)
		}
		_, body := w.last()
		if !strings.Contains(body, `"requests"`) || !strings.Contains(body, `"resourceId"`) {
			t.Errorf("body %s", body)
		}
	})
}

func TestServiceAccounts(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		sas, err := w.client(t).ServiceAccounts(context.Background(), project, "evo")
		if err != nil {
			t.Fatal(err)
		}
		if len(sas) != 1 || sas[0] != (ServiceAccount{ID: saID, Name: "evo-0", Type: "regular", Enabled: true}) {
			t.Fatalf("service accounts %+v", sas)
		}
		r, _ := w.last()
		if r.URL.Query().Get("target.projectId") != project || r.URL.Query().Get("filter") != "name=evo" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
	})
}

func TestAccessKeys_NoSecret(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		keys, err := w.client(t).AccessKeys(context.Background(), saID)
		if err != nil {
			t.Fatal(err)
		}
		want := AccessKey{ID: "ak-1", KeyID: "KEYID1", Description: "evo login", ExpiredAt: "2026-12-05T00:00:00Z", CreatedAt: "2026-11-05T00:00:00Z"}
		if len(keys) != 1 || keys[0] != want {
			t.Fatalf("keys %+v", keys)
		}
		noLeak(t, keys)
	})
}

// A list longer than the client's bound is an error that says so, never
// a list cut short (pager; «Риск», заключение 22: a bound on IAM lists).
func TestLists_BoundedPages(t *testing.T) {
	w := &world{pages: MaxPages + 1}
	_, err := w.client(t).Roles(context.Background(), RoleFilter{Type: "ROLE_TYPE_PRODUCT"})
	if err == nil || !strings.Contains(err.Error(), "roles") {
		t.Fatalf("err %v, want the list named and refused", err)
	}
	if len(w.asked) != MaxPages {
		t.Errorf("asked %d pages, want %d", len(w.asked), MaxPages)
	}
}

// A value is never part of the expression: a filter value with a space,
// «AND» or a quote is refused before any request (the filter takes only
// AND of key=value).
func TestFilter_ValueCannotWidenTheExpression(t *testing.T) {
	w := &world{}
	c := w.client(t)
	for _, v := range []string{project + " AND subject_type=user", "a b", `x"`, "", "a\nb"} {
		if _, err := c.Permissions(context.Background(), v, ""); err == nil {
			t.Errorf("resource %q accepted", v)
		}
		if _, err := c.ServiceAccounts(context.Background(), project, v+" AND enabled=false"); err == nil {
			t.Errorf("name %q accepted", v)
		}
	}
	if len(w.asked) != 0 {
		t.Errorf("%d requests sent for refused filters", len(w.asked))
	}
}

// Errors: 403 says the role, 429 has its own code; every other status is
// «iam API <status>: …» for the reason every client's status decides
// (internal/tools, TestStatusBeforeText_*). The token is in no error.
func TestErrors(t *testing.T) {
	for st, code := range map[int]string{
		http.StatusForbidden:       clerrors.CodeAPIError,
		http.StatusTooManyRequests: clerrors.CodeAPIRateLimited,
		http.StatusUnauthorized:    "",
		http.StatusBadRequest:      "",
		http.StatusNotFound:        "",
	} {
		w := &world{status: map[string]int{"/api/v3/permissions": st}}
		_, err := w.client(t).Permissions(context.Background(), project, saID)
		if err == nil {
			t.Errorf("%d: no error", st)
			continue
		}
		text := err.Error()
		if ce, ok := err.(*clerrors.CLIError); ok {
			text += ce.Hint
			if ce.Code != code {
				t.Errorf("%d: %s, want %q", st, ce.Code, code)
			}
		} else if code != "" {
			t.Errorf("%d: %v, want %s", st, err, code)
		}
		if strings.Contains(text, token) || !strings.Contains(err.Error(), fmt.Sprintf("iam API %d", st)) {
			t.Errorf("%d: %q", st, text)
		}
	}
}

// Every path and query parameter the client sends is one the spec names.
func TestRequests_AsTheSpecNamesThem(t *testing.T) {
	w := &world{}
	c := w.client(t)
	ctx := context.Background()
	_, _ = c.Roles(ctx, RoleFilter{Type: "ROLE_TYPE_PRODUCT"})
	_, _ = c.Actions(ctx, "iam")
	_, _ = c.Permissions(ctx, project, "")
	_, _ = c.Evaluate(ctx, Check{Action: "granted", ResourceID: project, UserID: userID})
	_, _ = c.BulkEvaluate(ctx, []Check{{Action: "granted", ResourceID: project}})
	_, _ = c.ServiceAccounts(ctx, project, "")
	_, _ = c.AccessKeys(ctx, saID)
	known := map[string][]string{
		"/api/v1/roles":                                   {"customerId", "filter", "pageSize", "pageToken", "orderBy"},
		"/api/v1/actions":                                 {"filter", "pageSize", "pageToken", "orderBy"},
		"/api/v3/permissions":                             {"filter", "pageSize", "pageToken"},
		"/api/v1/permissions/evaluate":                    {"action", "resourceId", "customerId", "userId", "serviceAccountId"},
		"/api/v1/permissions/bulk-evaluate":               nil,
		"/api/v2/serviceAccounts":                         {"target.customerId", "target.projectId", "filter", "pageSize", "pageToken"},
		"/api/v2/serviceAccounts/" + saID + "/accessKeys": nil,
	}
	if len(w.asked) != len(known) {
		t.Fatalf("asked %d requests, want %d", len(w.asked), len(known))
	}
	for _, r := range w.asked {
		params, ok := known[r.URL.Path]
		if !ok {
			t.Errorf("path %s is not in the spec", r.URL.Path)
			continue
		}
		for k := range r.URL.Query() {
			if !slices.Contains(params, k) {
				t.Errorf("%s: parameter %q is not in the spec", r.URL.Path, k)
			}
		}
		if r.Method == http.MethodGet && r.ContentLength > 0 {
			t.Errorf("%s: a GET with a body", r.URL.Path)
		}
	}
}
