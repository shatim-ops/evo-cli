// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// secretKey reports whether a string under key k is masked in any tool's
// result: the key holds a root and no exception for every tool applies (as
// redactValue decides for a tool without exceptions of its own).
func secretKey(k string) bool {
	return hasRoot(k) && !nonSecret("", k, nil)
}

// restrictionsFor expands HTTPRestrictions into "ID tool" for one setup:
// over HTTP or stdio, with or without --allow-secret-export and
// --read-only. A server setting (StdioToo) wraps handlers only with
// --read-only (H11); --read-only applies its entries (ReadOnlyToo) on
// either transport and leaves H5 to H11.
func restrictionsFor(s *Server, http, allowSecretExport, readOnly bool) []string {
	var out []string
	for _, r := range HTTPRestrictions {
		on := false
		switch {
		case r.StdioToo:
			on = readOnly
		case readOnly && r.ReadOnlyToo:
			on = true
		case http:
			on = !r.UnlessSecretExport || !allowSecretExport && !readOnly
		}
		if !on {
			continue
		}
		names := r.Tools
		if r.AllTools {
			names = nil
			for _, t := range s.reg.Tools() {
				names = append(names, t.Name)
			}
		}
		for _, n := range names {
			out = append(out, r.ID+" "+n)
		}
	}
	slices.Sort(out)
	return out
}

// handlerPointers is the code pointer of every tool's handler, in the
// registry and in the tools/call dispatch table.
func handlerPointers(s *Server) map[string][2]uintptr {
	out := map[string][2]uintptr{}
	for _, t := range s.reg.Tools() {
		out[t.Name] = [2]uintptr{reflect.ValueOf(t.Handler).Pointer(), reflect.ValueOf(s.handlers[t.Name]).Pointer()}
	}
	return out
}

// REQ-9 (spec.md): HTTP mode differs from stdio only as HTTPRestrictions
// lists, and docs/mcp.md lists the same. A new wrap, a new disabled tool or
// masking outside the list fails here.
func TestHTTPPolicy_ClosedList(t *testing.T) {
	ids := map[string]bool{}
	for i, r := range HTTPRestrictions {
		if want := fmt.Sprintf("H%d", i+1); r.ID != want {
			t.Errorf("entry %d: ID %s, want %s (in order, no gaps)", i, r.ID, want)
		}
		ids[r.ID] = true
		if r.Summary == "" || r.Where == "" || r.Symbol == "" {
			t.Errorf("%s: Summary, Where and Symbol are required", r.ID)
		}
		if r.AllTools && len(r.Tools) > 0 {
			t.Errorf("%s: AllTools and Tools", r.ID)
		}
		reg := newTestServer().reg
		for _, n := range r.Tools {
			if _, ok := reg.Lookup(n); !ok {
				t.Errorf("%s: no tool %s", r.ID, n)
			}
		}
		// The code the entry names exists and still holds the symbol.
		src, err := os.ReadFile(filepath.Join("..", "..", r.Where))
		if err != nil {
			t.Errorf("%s: %v", r.ID, err)
		} else if !strings.Contains(string(src), r.Symbol) {
			t.Errorf("%s: %s has no %q", r.ID, r.Where, r.Symbol)
		}
	}
	if !strings.Contains(restriction("H8").Summary, strconv.Itoa(httpMaxToolCalls)) {
		t.Errorf("H8 does not name the limit %d: %s", httpMaxToolCalls, restriction("H8").Summary)
	}

	// The handlers: exactly the listed wraps are applied, and no other tool's
	// handler changes, over the whole HTTP setup (NewHTTPHandler), and with
	// --read-only (SetReadOnly) on either transport.
	for _, setup := range []struct{ http, allow, readOnly bool }{
		{true, false, false}, {true, true, false}, {false, false, true}, {true, false, true},
	} {
		name := fmt.Sprintf("http=%v allow-secret-export=%v read-only=%v", setup.http, setup.allow, setup.readOnly)
		s := newTestServer()
		before := handlerPointers(s)
		if setup.readOnly {
			s.SetReadOnly()
		}
		if setup.http {
			h := NewHTTPHandler(s, HTTPOptions{AllowSecretExport: setup.allow})
			h.Close()
		}
		got := slices.Clone(s.httpApplied)
		slices.Sort(got)
		if want := restrictionsFor(s, setup.http, setup.allow, setup.readOnly); !slices.Equal(got, want) {
			t.Errorf("%s: applied\n%v\nwant\n%v", name, got, want)
		}
		wrapped := map[string]bool{}
		for _, a := range got {
			_, tool, _ := strings.Cut(a, " ")
			wrapped[tool] = true
		}
		after := handlerPointers(s)
		for tool, p := range before {
			if changed := after[tool] != p; changed != wrapped[tool] {
				t.Errorf("%s: %s handler changed=%v, listed=%v", name, tool, changed, wrapped[tool])
			}
		}
	}

	// In the code, handlers are wrapped only through restrict, and results
	// are masked only through H7: a direct wrapHandler / reg.Wrap or
	// redactSecrets elsewhere would bypass the list.
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch f := c.Fun.(type) {
				case *ast.SelectorExpr:
					name = f.Sel.Name
					// Wrap of the registry (x.reg.Wrap), not clierrors.Wrap.
					if x, ok := f.X.(*ast.SelectorExpr); name == "Wrap" && (!ok || x.Sel.Name != "reg") {
						name = ""
					}
				case *ast.Ident:
					name = f.Name
				}
				allowed := map[string]string{"wrapHandler": "restrict", "Wrap": "wrapHandler", "redactSecrets": "restrictSecrets"}
				if in, ok := allowed[name]; ok && fn.Name.Name != in {
					t.Errorf("%s: %s calls %s; only %s may (HTTPRestrictions)", fset.Position(c.Pos()), fn.Name.Name, name, in)
				}
				return true
			})
		}
	}

	// docs/mcp.md: one row per entry, nothing more, naming the entry's tools,
	// and the masked fields.
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "mcp.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\| (H\d+) \|(.*)$`).FindAllStringSubmatch(string(doc), -1) {
		if _, dup := rows[m[1]]; dup {
			t.Errorf("docs/mcp.md: two rows %s", m[1])
		}
		rows[m[1]] = m[2]
	}
	for id := range rows {
		if !ids[id] {
			t.Errorf("docs/mcp.md has %s, HTTPRestrictions does not", id)
		}
	}
	for _, r := range HTTPRestrictions {
		row, ok := rows[r.ID]
		if !ok {
			t.Errorf("docs/mcp.md: no row %s", r.ID)
			continue
		}
		for _, n := range r.Tools {
			if !strings.Contains(row, "`"+n+"`") {
				t.Errorf("docs/mcp.md row %s does not name `%s`", r.ID, n)
			}
		}
	}
	for _, w := range []string{"HTTPRestrictions", "HTTPSecretFields", "HTTPNonSecretFields", "`redacted`"} {
		if !strings.Contains(string(doc), w) {
			t.Errorf("docs/mcp.md does not mention %s", w)
		}
	}
	for _, f := range HTTPSecretFields {
		if !strings.Contains(string(doc), "`"+f.Name+"`") {
			t.Errorf("docs/mcp.md does not list the root `%s`", f.Name)
		}
	}
	// The exceptions: each is a row of the docs table of HTTPNonSecretFields,
	// and the table has no others.
	const excHeader = "| Исключение | Инструменты | Почему не секрет |"
	exc := map[string]bool{}
	_, table, ok := strings.Cut(string(doc), excHeader+"\n")
	if !ok {
		t.Errorf("docs/mcp.md: no table %q", excHeader)
	}
	row := regexp.MustCompile("^\\| `([A-Za-z_]+)` \\|")
	for _, line := range strings.Split(table, "\n") {
		line = strings.TrimLeft(line, " ") // the table may sit in a list item
		if !strings.HasPrefix(line, "|") {
			break
		}
		if m := row.FindStringSubmatch(line); m != nil {
			exc[m[1]] = true
		}
	}
	for _, f := range HTTPNonSecretFields {
		if !exc[f.Name] {
			t.Errorf("docs/mcp.md: no row `%s` in the table of HTTPNonSecretFields", f.Name)
		}
		delete(exc, f.Name)
		if f.Why == "" {
			t.Errorf("%s: no reason", f.Name)
		}
		if strings.HasPrefix(f.Name, "_") != f.Suffix {
			t.Errorf("%s: a suffix is written with a leading _, a whole key without", f.Name)
		}
	}
	for n := range exc {
		t.Errorf("docs/mcp.md lists the exception `%s`, HTTPNonSecretFields does not", n)
	}
}

// redactedOf returns the "redacted" field of a JSON result (nil if absent).
func redactedOf(t *testing.T, text string) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not a JSON object: %s", text)
	}
	raw, ok := m["redacted"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("redacted is not a list: %s", text)
	}
	out := []string{}
	for _, x := range list {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("redacted holds a non-string: %s", text)
		}
		out = append(out, s)
	}
	return out
}

// Q5 (spec.md §8): a masked result names the masked fields in redacted —
// paths, not values; a result with nothing masked is left as the tool wrote
// it, and stdio or --allow-secret-export never adds the field.
func TestSec_RedactedListsMaskedFields(t *testing.T) {
	results := map[string]string{
		"pg_users":     `{"users":[{"name":"a","password":"pw-a"},{"name":"b","password":"pw-b"}],"cluster":{"admin":{"rootPassword":"pw-r"}}}`,
		"kafka_get":    `{"id":"k","name":"n","status":"RUNNING"}`,
		"cert_get":     `{"id":"c","privateKeyPem":"-----BEGIN PRIVATE KEY-----x","private_key_id":"kid"}`,
		"stack_status": `{"vm":{"password":"pw-vm"},"redacted":["earlier.token"]}`,
		"redis_list":   `[{"id":"r","password":"pw-list"}]`,
		"logs":         `password=plain text, not JSON`,
	}
	newServer := func() *Server {
		s := newTestServer()
		for name, text := range results {
			fakeResult(s, name, text)
		}
		return s
	}
	s := newServer()
	s.restrictForHTTP(false)

	want := map[string][]string{
		"pg_users":     {"cluster.admin.rootPassword", "users[].password"},
		"cert_get":     {"privateKeyPem"},
		"stack_status": {"earlier.token", "vm.password"},
	}
	for name, paths := range want {
		text, _ := call(s, name, nil)
		if got := redactedOf(t, text); !slices.Equal(got, paths) {
			t.Errorf("%s: redacted %v, want %v: %s", name, got, paths, text)
		}
		for _, v := range []string{"pw-a", "pw-b", "pw-r", "BEGIN PRIVATE KEY", "pw-vm"} {
			if strings.Contains(text, v) {
				t.Errorf("%s leaks %s: %s", name, v, text)
			}
		}
	}
	if text, _ := call(s, "cert_get", nil); !strings.Contains(text, `"kid"`) {
		t.Errorf("cert_get: private_key_id is an id, not a key: %s", text)
	}
	// Nothing to mask: byte for byte the tool's answer, no redacted.
	if text, _ := call(s, "kafka_get", nil); text != results["kafka_get"] {
		t.Errorf("kafka_get changed: %s", text)
	}
	// A list result is masked; it has no object to carry redacted.
	if text, _ := call(s, "redis_list", nil); strings.Contains(text, "pw-list") || !json.Valid([]byte(text)) {
		t.Errorf("redis_list: %s", text)
	}
	// Text that is not JSON is not touched (as before).
	if text, _ := call(s, "logs", nil); text != results["logs"] {
		t.Errorf("logs changed: %s", text)
	}

	for _, srv := range []*Server{func() *Server { s := newServer(); s.restrictForHTTP(true); return s }(), newServer()} {
		if text, _ := call(srv, "pg_users", nil); text != results["pg_users"] {
			t.Errorf("stdio / --allow-secret-export changed the result: %s", text)
		}
	}
}

// Every root of HTTPSecretFields, in the spellings an API uses and anywhere
// in the key, is masked over HTTP, in every tool's result; so are the
// fields the tools are known to return. Unknown fields of the cloud are
// masked by default: a leak costs more than a mask too many.
func TestSec_KnownSecretFieldsMasked(t *testing.T) {
	camel := func(s string) string {
		parts := strings.Split(s, "_")
		for i := 1; i < len(parts); i++ {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
		return strings.Join(parts, "")
	}
	for _, f := range HTTPSecretFields {
		keys := []string{f.Name, camel(f.Name), strings.ToUpper(f.Name), strings.ReplaceAll(f.Name, "_", "-")}
		if !f.Exact {
			keys = append(keys, "admin_"+f.Name, "db"+camel("_"+f.Name), f.Name+"_pem", camel(f.Name)+"Base64",
				f.Name+"_hash", "new_"+f.Name+"_confirm", f.Name+"s", f.Name+"_2", f.Name+"_old", camel(f.Name)+"Temp",
				f.Name+"_expires_at_raw", f.Name+"expiresat")
		}
		for _, k := range keys {
			if !secretKey(k) {
				t.Errorf("%s (%s) is not masked", k, f.Name)
			}
		}
	}
	for _, k := range []string{
		"password_new", "new_password_confirm", "root_password_1", "passwordTemp", "passwords",
		"private_key_2", "secret_old", "password_expires_at_raw", "passwordreset", "secret_path",
		"accessToken", "idToken", "admin_api_key", "client_secret", "secretKey", "secret_key", "TOKEN", "Token",
	} {
		if !secretKey(k) {
			t.Errorf("%s is not masked", k)
		}
	}

	// Lists and objects under a credential key: every string inside.
	unknown := `{"passwords":["S3cret-a","S3cret-b"],"secret_old":{"v":"S3cret-c","n":[1,"S3cret-d"],"updated_at":"S3cret-e"},"private_key_2":"S3cret-f","count":2}`
	got := redactSecrets("kafka_get", unknown, httpWording)
	if strings.Contains(got, "S3cret") {
		t.Errorf("unknown credential fields leak: %s", got)
	}
	if want := []string{"passwords[]", "private_key_2", "secret_old.n[]", "secret_old.updated_at", "secret_old.v"}; !slices.Equal(redactedOf(t, got), want) {
		t.Errorf("redacted %v, want %v", redactedOf(t, got), want)
	}
	if !strings.Contains(got, `"count": 2`) || !strings.Contains(got, `1,`) {
		t.Errorf("numbers lost: %s", got)
	}

	// Results as the tools build them (internal/tools), and cloud objects
	// with credentials the API might send.
	known := map[string]string{
		"deploy_apply": `{"vm_id":"v","public_ip":"1.2.3.4","ssh":"ssh user1@1.2.3.4","password":"S3cret-VM"}`,
		"pg_get":       `{"id":"p","users":[{"name":"u","password":"S3cret-PG"}]}`,
		"redis_get":    `{"id":"r","password":"S3cret-RD"}`,
		"kafka_get":    `{"id":"k","users":[{"user_name":"u","userPassword":"S3cret-KF"}]}`,
		"k8s_get":      `{"id":"c","auth":{"client_secret":"S3cret-OIDC","id_token":"S3cret-ID","accessToken":"S3cret-AT"}}`,
		"s3_buckets":   `{"credentials":{"access_key":"AK","secret_access_key":"S3cret-S3","secretKey":"S3cret-S3b"}}`,
		"vm_get":       `{"id":"v","metadata":{"admin_password":"S3cret-MD","linux_password":"S3cret-LX"}}`,
		"ar_get":       `{"id":"a","token":"S3cret-TK","api_key":"S3cret-AK"}`,
	}
	s := newTestServer()
	for name, text := range known {
		fakeResult(s, name, text)
	}
	s.restrictForHTTP(false)
	for name := range known {
		text, _ := call(s, name, nil)
		if strings.Contains(text, "S3cret") {
			t.Errorf("%s over HTTP leaks a credential: %s", name, text)
		}
		if len(redactedOf(t, text)) == 0 {
			t.Errorf("%s: no redacted field: %s", name, text)
		}
	}
	if text, _ := call(s, "s3_buckets", nil); !strings.Contains(text, `"AK"`) {
		t.Errorf("access_key is an id, not a secret: %s", text)
	}
}

// camelWords writes "_expires_at" as "ExpiresAt".
func camelWords(s string) string {
	out := ""
	for _, w := range strings.Split(strings.Trim(s, "_"), "_") {
		out += strings.ToUpper(w[:1]) + w[1:]
	}
	return out
}

// The exceptions of HTTPNonSecretFields stay: a key with a root whose value
// is not a credential (an id, a timestamp, a policy, a flag, a page cursor).
func TestSec_NonSecretFieldsKept(t *testing.T) {
	for _, f := range HTTPNonSecretFields {
		if f.Container || !f.Suffix {
			continue
		}
		for _, k := range []string{"password" + f.Name, "secret" + strings.ToUpper(f.Name), "privateKey" + camelWords(f.Name), "api-key" + strings.ReplaceAll(f.Name, "_", "-")} {
			if secretKey(k) {
				t.Errorf("%s (%s) is masked", k, f.Name)
			}
		}
	}
	for _, k := range []string{
		"password_expires_at", "passwordExpiresAt", "PASSWORD_EXPIRES_AT", "passwordPolicy", "password_updated_at", "password_created_at",
		"private_key_id", "privateKeyAlgorithm", "private_key_alg", "secret_key_id", "secretKeyID", "client_secret_expires_at",
		"secret_id", "secret_ids", "secret_name", "secret_ref", "secret_version", "access_token_type", "token_kind",
		"password_length", "password_status", "password_enabled", "password_required", "password_set",
		"next_page_token", "nextPageToken", "page_token", "pageToken", "verification_token", "token_expires_at", "token_source",
		"kubeconfig", "access_key", "key_id", "name", "id",
	} {
		if secretKey(k) {
			t.Errorf("%s is masked", k)
		}
	}

	// Secret Manager answers metadata under secret / secrets: kept for its
	// tools, masked for any other tool (whose object might hold a value).
	list := `{"secrets":[{"id":"1","name":"db","path":"app/db","description":"d"}]}`
	if got := redactSecrets("secret_list", list, httpWording); got != list {
		t.Errorf("secret_list metadata masked: %s", got)
	}
	created := `{"secret":{"id":"1","path":"app/db"},"status":"created"}`
	if got := redactSecrets("secret_create", created, httpWording); got != created {
		t.Errorf("secret_create metadata masked: %s", got)
	}
	if got := redactSecrets("secret_create", `{"secret":"S3cret-x"}`, httpWording); strings.Contains(got, "S3cret") {
		t.Errorf("a string under secret is a credential even for secret_create: %s", got)
	}
	if got := redactSecrets("secret_list", `{"secrets":["S3cret-y"]}`, httpWording); strings.Contains(got, "S3cret") {
		t.Errorf("strings under secrets are credentials: %s", got)
	}
	if got := redactSecrets("kafka_get", `{"secret":{"value":"S3cret-z"}}`, httpWording); strings.Contains(got, "S3cret") {
		t.Errorf("the Secret Manager exception holds outside its tools: %s", got)
	}
	const kubeconfig = `{"auth":"cloudlogin exec-plugin (must be installed)","kubeconfig":"apiVersion: v1\nusers:\n- name: u\n  user:\n    exec:\n      command: cloudlogin\n"}`
	s := newTestServer()
	fakeResult(s, "k8s_kubeconfig", kubeconfig)
	s.restrictForHTTP(false)
	if text, _ := call(s, "k8s_kubeconfig", map[string]any{"cluster_id": "c", "embed_token": false}); text != kubeconfig {
		t.Errorf("k8s_kubeconfig embed_token=false (the HTTP alternative, no credentials inside) changed: %s", text)
	}
}
