// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package iamadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// The world of the Administration API, from its public specification
// (iam-gw-public.yaml): the spec's camelCase, or — snake — the live API's
// snake_case (IAM-2). No network.
const (
	project = "00000000-0000-4000-8000-0000000000a1"
	saID    = "00000000-0000-4000-8000-0000000000c1"
	token   = "TOKEN-VALUE-MUST-NOT-LEAVE"
	secret  = "SECRET-VALUE-MUST-NOT-LEAVE"
)

type world struct {
	snake  bool
	status int // answer to every request instead of the list
	pages  int // pages of the list (default 1)
	mu     sync.Mutex
	asked  []*http.Request
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
	return snake(k)
}

func (w *world) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	w.asked = append(w.asked, r)
	w.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+token {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	if w.status != 0 {
		rw.WriteHeader(w.status)
		fmt.Fprintf(rw, `{"code":%d,"message":"refused by the world"}`, w.status)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v2/serviceAccounts/"+saID+"/accessKeys" {
		key := map[string]any{}
		for k, v := range map[string]any{"id": "ak-1", "keyId": "KEYID1", "secret": secret, "description": "evo login",
			"expiredAt": "2026-12-05T00:00:00Z", "createdAt": "2026-11-05T00:00:00Z", "serviceAccountId": saID} {
			key[w.key(k)] = v
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{w.key("accessKeys"): []any{key}})
		return
	}
	if r.Method != http.MethodGet || r.URL.Path != "/api/v3/permissions" {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	n := 0
	if tok := r.URL.Query().Get("pageToken"); tok != "" {
		fmt.Sscanf(tok, "p%d", &n)
	}
	item := map[string]any{}
	for k, v := range map[string]any{"id": fmt.Sprintf("perm-%d", n), "roleName": "platform.project.admin",
		"primaryRoleId": "r-uuid", "roleId": "platform.project.admin", "resourceId": project, "subjectId": saID,
		"subjectType": "service_account", "expiresAt": "2026-12-05T00:00:00Z", "enabled": true,
		"roleType": "ROLE_TYPE_PRODUCT", "createdAt": "2026-11-05T00:00:00Z"} {
		item[w.key(k)] = v
	}
	out := map[string]any{w.key("permissions"): []any{item}}
	if n+1 < max(w.pages, 1) {
		out[w.key("nextPageToken")] = fmt.Sprintf("p%d", n+1)
	}
	_ = json.NewEncoder(rw).Encode(out)
}

func TestPermissions_WhitelistBothSpellingsEveryPage(t *testing.T) {
	for _, snake := range []bool{false, true} {
		w := &world{snake: snake, pages: 2}
		perms, err := w.client(t).Permissions(context.Background(), project, saID)
		if err != nil {
			t.Fatalf("snake=%v: %v", snake, err)
		}
		want := Permission{ID: "perm-1", RoleName: "platform.project.admin", RoleID: "r-uuid", ResourceID: project,
			SubjectID: saID, SubjectType: "service_account", ExpiresAt: "2026-12-05T00:00:00Z", Enabled: true}
		if len(perms) != 2 || perms[1] != want {
			t.Fatalf("snake=%v: permissions %+v, want the second %+v", snake, perms, want)
		}
		r := w.asked[len(w.asked)-1]
		if got := r.URL.Query().Get("filter"); got != "resource_id="+project+" AND subject_id="+saID {
			t.Errorf("filter %q: resource_id is required, the subject narrows it", got)
		}
		if b, _ := json.Marshal(perms); strings.Contains(string(b), token) {
			t.Errorf("the token left the client: %s", b)
		}
	}
}

// The parameters are the spec's: filter, pageSize, pageToken.
func TestPermissions_AsTheSpecNamesThem(t *testing.T) {
	w := &world{pages: 2}
	if _, err := w.client(t).Permissions(context.Background(), project, ""); err != nil {
		t.Fatal(err)
	}
	for _, r := range w.asked {
		for k := range r.URL.Query() {
			if k != "filter" && k != "pageSize" && k != "pageToken" {
				t.Errorf("parameter %q is not in the spec", k)
			}
		}
		if got := r.URL.Query().Get("filter"); got != "resource_id="+project {
			t.Errorf("filter %q without a subject", got)
		}
	}
}

// A list longer than the client's bound is an error that says so, never
// a list cut short (pager; «Риск», conclusion 22: a bound on IAM lists).
func TestPermissions_BoundedPages(t *testing.T) {
	w := &world{pages: MaxPages + 1}
	_, err := w.client(t).Permissions(context.Background(), project, saID)
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("err %v, want the list named and refused", err)
	}
	if len(w.asked) != MaxPages {
		t.Errorf("asked %d pages, want %d", len(w.asked), MaxPages)
	}
}

// A value is never part of the expression: a filter value with a space,
// «AND», a quote or a line break is refused before any request (the filter
// takes only AND of key=value); the resource is required.
func TestPermissions_ValueCannotWidenTheExpression(t *testing.T) {
	w := &world{}
	c := w.client(t)
	for _, v := range []string{project + " AND subject_type=user", "a b", `x"`, "", "a\nb"} {
		if _, err := c.Permissions(context.Background(), v, ""); err == nil {
			t.Errorf("resource %q accepted", v)
		}
		if v == "" {
			continue
		}
		_, err := c.Permissions(context.Background(), project, v)
		if ce, ok := err.(*clerrors.CLIError); !ok || ce.Code != clerrors.CodeUsage {
			t.Errorf("subject %q: %v, want USAGE", v, err)
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
		w := &world{status: st}
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

// The list of keys carries each key's secret; what the client returns has
// no field for it (R-43). Both spellings; the path is the spec's.
func TestAccessKeys_NoSecret(t *testing.T) {
	for _, snake := range []bool{false, true} {
		w := &world{snake: snake}
		keys, err := w.client(t).AccessKeys(context.Background(), saID)
		if err != nil {
			t.Fatalf("snake=%v: %v", snake, err)
		}
		if len(keys) != 1 || keys[0] != (AccessKey{ID: "ak-1", KeyID: "KEYID1", ExpiredAt: "2026-12-05T00:00:00Z"}) {
			t.Fatalf("snake=%v: keys %+v", snake, keys)
		}
		if b, _ := json.Marshal(keys); strings.Contains(string(b), secret) || strings.Contains(string(b), token) {
			t.Fatalf("snake=%v: a secret left the client: %s", snake, b)
		}
	}
	// The service account's id is a segment of the path: anything but an
	// id is refused before the request.
	w := &world{}
	for _, v := range []string{"../permissions", "..", ".", "a/b", "", "x y", "a.b"} {
		if _, err := w.client(t).AccessKeys(context.Background(), v); err == nil {
			t.Errorf("service account %q accepted", v)
		}
	}
	if len(w.asked) != 0 {
		t.Errorf("%d requests for refused ids", len(w.asked))
	}
}

// Audit of 06.11 (B-5, 2 of 2): IAM read «resource_id=P+AND+subject_id=S»
// with «+» as itself, not a space, and refused it — «invalid filter
// format: "+subject_id=…+"». The spaces of a filter of two or more parts
// go as %20, which every server reads as a space; the raw query is what
// is checked, not its decoding by Go.
func TestPermissions_FilterSpacesAreNotPlus(t *testing.T) {
	w := &world{pages: 1}
	if _, err := w.client(t).Permissions(context.Background(), project, saID); err != nil {
		t.Fatal(err)
	}
	for _, r := range w.asked {
		raw := r.URL.RawQuery
		if strings.Contains(raw, "+") || !strings.Contains(raw, "filter=resource_id%3D"+project+"%20AND%20subject_id%3D"+saID) {
			t.Errorf("raw query %q: the spaces of the filter are not %%20", raw)
		}
	}
}
