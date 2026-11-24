// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The world of a repeated delete (D26): the cloud answers 404 to whatever
// names goneID or goneName and has everything else — an empty one, with no
// resources inside.
const (
	goneID   = "9a0e0000-0000-4000-8000-000000000001"
	goneName = "gone-thing"
	liveID   = "11110000-0000-4000-8000-000000000001"
	liveName = "live-thing"
)

func goneWorld(w http.ResponseWriter, r *http.Request) bool {
	where := r.URL.Path + "?" + r.URL.RawQuery
	gone := strings.Contains(where, goneID) || strings.Contains(where, goneName)
	if r.URL.Host == "s3.cloud.ru" || strings.HasSuffix(r.URL.Host, ".s3.cloud.ru") {
		// Object Storage: a bucket that is not there is 404; a key that is
		// not there is not listed.
		w.Header().Set("Content-Type", "application/xml")
		if bucket, _, _ := strings.Cut(strings.Trim(r.URL.Path, "/"), "/"); strings.Contains(bucket, goneName) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>not found</Message></Error>`)
			return true
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`+
				`<Name>`+liveName+`</Name><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/security-groups" {
		// The group of close_ports is a list's item (the API reads no group
		// by id): liveID is there, goneID is not; its rules are none.
		_, _ = io.WriteString(w, `{"items":[{"id":"`+liveID+`","name":"`+liveName+`"}],"total":1}`)
		return true
	}
	if gone {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":5,"message":"not found"}`)
		return true
	}
	if r.Method != http.MethodGet {
		_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
		return true
	}
	_, _ = io.WriteString(w, `{"id":"`+liveID+`","name":"`+liveName+`","status":"RUNNING","state":"running",`+
		`"project_id":"`+dbaasProject+`","projectId":"`+dbaasProject+`",`+
		`"items":[],"total":0,"clusters":[],"databases":[],"users":[],"routes":[],"zones":[],"records":[],"secrets":[]}`)
	return true
}

// goneRegistry is a registry on goneWorld, S3 keys and a state directory
// of its own.
func goneRegistry(t *testing.T) *Registry {
	t.Helper()
	stackHome(t)
	f := &apiFake{own: goneWorld}
	old := httpretry.DefaultBase
	httpretry.DefaultBase = f
	t.Cleanup(func() { httpretry.DefaultBase = old })
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
				S3: config.S3Config{AccessKey: "a", SecretKey: "s"}},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	})
	r.SetStateDir(dir)
	return r
}

// deleteLike: a tool that changes the cloud and whose name says it
// deletes — the tools Decl.Delete must declare.
func deleteLike(t Tool) bool {
	n := t.Name
	return t.Mutating && (strings.HasSuffix(n, "_delete") || strings.HasSuffix(n, "_rm") ||
		n == "destroy" || n == "release_floating_ip")
}

// Every delete tool is declared, with arguments its schema has.
func TestAlreadyDeleted_EveryDeleteDeclared(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range New(nil).Tools() {
		_, declared := deleteOf(tl.Name)
		if deleteLike(tl) && !declared {
			t.Errorf("%s deletes a resource but is not in Decl.Delete (D26: a repeated delete answers already_deleted)", tl.Name)
		}
		if !declared {
			continue
		}
		seen[tl.Name] = true
		d := decls[tl.Name].Delete
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range []string{d.arg, d.parent} {
			if _, ok := props[a]; a != "" && !ok {
				t.Errorf("%s: Decl.Delete names argument %q, which its schema has not", tl.Name, a)
			}
		}
	}
	for name := range allDeletes() {
		if !seen[name] {
			t.Errorf("Decl.Delete names %s, which is not a tool", name)
		}
	}
}

// goneArgs are the arguments of a delete of a resource that is not there:
// the target names goneID (a UUID) or goneName, the parent is there.
func goneArgs(name string, d deleteTarget) map[string]any {
	args := map[string]any{d.arg: goneID}
	if strings.Contains(d.arg, "name") || d.arg == "path" || d.arg == "bucket" || d.arg == "key" {
		args[d.arg] = goneName
	}
	// A target that is a list of ports (close_ports): a port with no rule.
	if tl, ok := New(nil).Lookup(name); ok {
		if p, _ := schemaProps(tl)[d.arg].(map[string]any); p["type"] == "array" {
			args[d.arg] = []any{float64(80)}
		}
	}
	if d.parent != "" {
		args[d.parent] = liveID
		if d.parent == "bucket" {
			args[d.parent] = liveName
		}
	}
	return args
}

// A delete of a resource that is not there answers already_deleted, exit 0
// and existed false — in the preview, with no question, and after consent
// (owner's decision 22.10, D26).
func TestAlreadyDeleted_RepeatedDeleteIsDeleted(t *testing.T) {
	r := goneRegistry(t)
	var names []string
	for name := range allDeletes() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := decls[name].Delete
		for _, confirmed := range []bool{false, true} {
			args := goneArgs(name, d)
			if confirmed {
				args["confirmed"] = true
			}
			m, text, isErr := callNamed(t, r, name, args)
			if isErr || m["status"] != "already_deleted" || m["existed"] != false || !namesTarget(m[d.arg], args[d.arg]) {
				t.Errorf("%s confirmed=%v: want already_deleted, exit 0: %s", name, confirmed, text)
			}
		}
	}
}

// namesTarget: the answer names the target as the call did — the same
// value, or for a list (ports) every element, compared as a whole: port
// 80 is not named by 8080 (a substring found it there, Qwen on #315).
func namesTarget(answer, arg any) bool {
	list, ok := arg.([]any)
	if !ok {
		return answer == arg
	}
	named := map[string]bool{}
	if as, ok := answer.([]any); ok {
		for _, a := range as {
			named[fmt.Sprint(a)] = true
		}
	} else {
		for _, w := range strings.FieldsFunc(fmt.Sprint(answer), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
		}) {
			named[w] = true
		}
	}
	for _, v := range list {
		if !named[fmt.Sprint(v)] {
			return false
		}
	}
	return true
}

func TestNamesTarget_ElementsAreWhole(t *testing.T) {
	for _, c := range []struct {
		answer, arg any
		want        bool
	}{
		{"80, 443", []any{80, 443}, true},
		{[]any{80.0, 443.0}, []any{80.0}, true},
		{"8080", []any{80}, false},
		{[]any{8080.0}, []any{80.0}, false},
		{"vm-1", "vm-1", true},
	} {
		if got := namesTarget(c.answer, c.arg); got != c.want {
			t.Errorf("namesTarget(%v, %v) = %v, want %v", c.answer, c.arg, got, c.want)
		}
	}
}

// A parent that is not there is the call's error, not a repeat: NOT_FOUND.
func TestAlreadyDeleted_ParentGoneIsNotFound(t *testing.T) {
	r := goneRegistry(t)
	for name, d := range allDeletes() {
		if d.parent == "" {
			continue
		}
		// A target the schema takes as a UUID (vpc_route_delete route_id,
		// D36) is named by one: another form is USAGE before the parent.
		target := any(liveName)
		tl, _ := r.Lookup(name)
		switch p, _ := schemaProps(tl)[d.arg].(map[string]any); {
		case p["format"] == "uuid":
			target = liveID
		case p["type"] == "array":
			target = []any{float64(80)} // ports (close_ports)
		}
		for _, confirmed := range []bool{false, true} {
			args := map[string]any{d.arg: target, d.parent: goneID, "confirmed": confirmed}
			if d.parent == "bucket" {
				args[d.parent] = goneName
			}
			m, text, isErr := callNamed(t, r, name, args)
			if !isErr || m["reason"] != clerrors.CodeNotFound {
				t.Errorf("%s confirmed=%v without its %s: want NOT_FOUND: %s", name, confirmed, d.parent, text)
			}
		}
	}
}

// An id of the wrong form is USAGE before the cloud, not already_deleted:
// the target of a delete whose schema says format uuid (D36).
func TestAlreadyDeleted_WrongFormIsUsage(t *testing.T) {
	uuids := 0
	for name, d := range allDeletes() {
		tl, _ := New(nil).Lookup(name)
		props, _ := tl.InputSchema["properties"].(map[string]any)
		if p, _ := props[d.arg].(map[string]any); p["format"] != "uuid" {
			continue
		}
		uuids++
		f := &apiFake{own: goneWorld}
		r := apiFakeRegistry(t, f)
		for _, confirmed := range []bool{false, true} {
			m, text, isErr := callNamed(t, r, name, map[string]any{d.arg: "not-a-uuid", "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage {
				t.Errorf("%s %s=not-a-uuid confirmed=%v: want USAGE: %s", name, d.arg, confirmed, text)
			}
		}
		if n := f.count(); n != 0 {
			t.Errorf("%s: %d requests for an id of the wrong form", name, n)
		}
	}
	// The Compute deletes: destroy, disk_delete, image_delete,
	// release_floating_ip, snat_delete.
	if uuids < 5 {
		t.Errorf("%d deletes take a UUID by their schema, want at least 5", uuids)
	}
}

// The answer an agent reads: what is not there, and a note that the id may
// be wrong; any other error is no answer of this kind.
func TestAlreadyDeleted_Answer(t *testing.T) {
	m := map[string]any{}
	r := New(nil)
	text, isErr, ok := r.alreadyDeleted("disk_delete", "", goneID, clerrors.New(clerrors.CodeNotFound, "not found", ""))
	if err := json.Unmarshal([]byte(text), &m); err != nil || !ok || isErr {
		t.Fatalf("answer %q: %v", text, err)
	}
	if m["status"] != "already_deleted" || m["existed"] != false || m["disk_id"] != goneID {
		t.Errorf("answer: %s", text)
	}
	if note, _ := m["note"].(string); !strings.Contains(note, goneID) || !strings.Contains(note, "disk_id") {
		t.Errorf("note: %q", note)
	}
	if _, _, ok := r.alreadyDeleted("disk_delete", "", goneID, clerrors.New(clerrors.CodeAPIError, "boom", "")); ok {
		t.Error("an API error read as gone")
	}
}

// liveWorld is goneWorld's opposite: the cloud has the resource named
// liveID or liveName, in its lists too — the world of a delete's preview.
func liveWorld(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Host == "s3.cloud.ru" || strings.HasSuffix(r.URL.Host, ".s3.cloud.ru") {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`+
			`<Name>`+liveName+`</Name><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated>`+
			`<Contents><Key>`+liveName+`</Key><Size>1</Size></Contents></ListBucketResult>`)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	item := `{"id":"` + liveID + `","name":"` + liveName + `","status":"RUNNING"}`
	_, _ = io.WriteString(w, `{"id":"`+liveID+`","name":"`+liveName+`","status":"RUNNING","project_id":"`+dbaasProject+`",`+
		`"items":[`+item+`],"total":1,"databases":[`+item+`],"users":[`+item+`],"routes":[`+item+`]}`)
	return true
}

// liveDelete is a delete tool's answer without consent on liveWorld.
func liveDelete(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	f := &apiFake{own: liveWorld}
	_, text, _ := callNamed(t, apiFakeS3Registry(t, f), name, args)
	return text
}

// apiFakeS3Registry is apiFakeRegistry with S3 keys.
func apiFakeS3Registry(t *testing.T, f *apiFake) *Registry {
	t.Helper()
	old := httpretry.DefaultBase
	httpretry.DefaultBase = f
	t.Cleanup(func() { httpretry.DefaultBase = old })
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
				S3: config.S3Config{AccessKey: "a", SecretKey: "s"}},
			Auth:    staticToken{},
			Cloud:   provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
	r.SetStateDir(t.TempDir())
	return r
}

// Bound to its projects (evo mcp --allow-project), a delete of what is not
// there is NOT_FOUND, not already_deleted (owner's decision 22.10, D26; H9
// option b) — for every delete tool: the target the bound check places in
// no allowed project, and the one the tool itself looks up in the server's
// project (an S3 object, a secret, a database of a cluster of the project).
func TestAlreadyDeleted_BoundServerKeepsNotFound(t *testing.T) {
	r := goneRegistry(t)
	r.SetProjectBound()
	var names []string
	for name := range allDeletes() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, confirmed := range []bool{false, true} {
			args := goneArgs(name, decls[name].Delete)
			if confirmed {
				args["confirmed"] = true
			}
			m, text, isErr := callNamed(t, r, name, args)
			if !isErr || m["reason"] != "NOT_FOUND" || strings.Contains(text, "already_deleted") {
				t.Errorf("%s confirmed=%v on a bound server: want NOT_FOUND, got %s", name, confirmed, text)
			}
		}
	}
}

// A description that promises already_deleted says the server bound to
// its projects answers NOT_FOUND instead (H9; TestAlreadyDeleted_
// BoundServerKeepsNotFound): the model reads the description over MCP,
// where --allow-project may be set («Риск», 194; close_ports and
// sg_rule_delete). In both languages.
func TestAlreadyDeleted_DescriptionNamesTheBoundAnswer(t *testing.T) {
	for _, tl := range New(nil).Tools() {
		if _, ok := deleteOf(tl.Name); !ok {
			continue
		}
		for lang, d := range map[string]string{"en": tl.Description, "ru": tl.DescriptionRU} {
			if strings.Contains(d, "already_deleted") && (!strings.Contains(d, "NOT_FOUND") || !strings.Contains(d, "--allow-project")) {
				t.Errorf("%s (%s): promises already_deleted, not what a server with --allow-project answers (NOT_FOUND)", tl.Name, lang)
			}
		}
	}
}

// Some services expose a card; others establish absence by listing the
// correct collection in the target's project, bucket or parent. DNS must
// check both visibilities. Keep this oracle independent of the handlers.
func TestDeleteReadsAbsenceBeforeWriting(t *testing.T) {
	const (
		compute   = "compute.api.cloud.ru/api/v1/"
		pg        = "postgresql.api.cloud.ru/v1/clusters/"
		dns       = "dns.api.cloud.ru/v1/"
		rules     = compute + "security-groups/" + liveID + "/rules"
		project   = "?project_id=" + dbaasProject
		projectID = "?projectId=" + dbaasProject
	)
	lookups := map[string][]string{
		"destroy":             {compute + "vms" + project},
		"image_delete":        {compute + "images/" + goneID},
		"disk_delete":         {compute + "disks/" + goneID},
		"subnet_delete":       {compute + "subnets/" + goneID},
		"release_floating_ip": {compute + "floating-ips/" + goneID},
		"backup_delete":       {compute + "backups/" + goneID},
		"snat_delete":         {compute + "nat-gateways" + project},
		"sg_rule_delete":      {rules},
		"close_ports":         {rules},
		"pg_delete":           {pg + goneID},
		"pg_db_delete":        {pg + liveID + "/databases"},
		"pg_user_delete":      {pg + liveID + "/users"},
		"cert_delete":         {"certificatemanager.api.cloud.ru/v1/certificate/" + goneID},
		"kafka_delete":        {"kafka.api.cloud.ru/v1/clusters/" + goneID},
		"redis_delete":        {"redis.api.cloud.ru/v1/clusters/" + goneID},
		"vpc_delete":          {"vpc.api.cloud.ru/v1/vpcs/" + goneID},
		"vpc_route_delete":    {"vpc.api.cloud.ru/v1/vpcs/" + liveID + "/routes/static"},
		"dns_zone_delete":     {dns + "publicZones" + project, dns + "privateZones" + project},
		"dns_record_delete":   {dns + "publicRecordsSole/" + goneID, dns + "privateRecordsSole/" + goneID},
		"secret_delete":       {"secretmanager.api.cloud.ru/v2/secret/" + goneName + projectID},
		"k8s_delete":          {"mk8s.api.cloud.ru/v3/clusters/" + goneID + project},
		"k8s_nodepool_delete": {"mk8s.api.cloud.ru/v3/nodePools/" + goneID + project},
		"ca_delete":           {"containers.api.cloud.ru/v2/containers/" + goneName + projectID},
		"tg_delete":           {"nlb.api.cloud.ru/v2/targetGroups/" + goneID},
		"lb_delete":           {"nlb.api.cloud.ru/v2/networkLoadBalancers/" + goneID},
		"ar_delete":           {"ar.api.cloud.ru/v1/registries/" + goneID + projectID},
		"s3_bucket_delete":    {"s3.cloud.ru/" + goneName + "/?list-type=2&prefix="},
		"s3_rm":               {"s3.cloud.ru/" + liveName + "/?list-type=2&prefix=" + goneName},
	}
	for name, target := range allDeletes() {
		t.Run(name, func(t *testing.T) {
			want := lookups[name]
			if len(want) == 0 {
				t.Fatal("missing target lookup in fixture")
			}
			for _, deny := range []bool{false, true} {
				stackHome(t)
				seen := map[string]bool{}
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					key := deleteReadKey(r)
					if r.Method == http.MethodGet && slices.Contains(want, key) {
						seen[key] = true
						if deny {
							http.Error(w, "target lookup requires authentication", http.StatusUnauthorized)
							return true
						}
					}
					return goneWorld(w, r)
				}}
				reg := apiFakeS3Registry(t, fake)
				args := goneArgs(name, target)
				args["confirmed"] = true
				result, text, failed := callNamed(t, reg, name, args)
				if contractWrites(fake.calls) != 0 {
					t.Fatalf("wrote while checking absence: %v", fake.calls)
				}
				if deny {
					if len(seen) == 0 || !failed || result["status"] == "already_deleted" {
						t.Fatalf("denied target lookup must fail: %s, requests %v", text, fake.calls)
					}
				} else if failed || result["status"] != "already_deleted" || len(seen) != len(want) {
					t.Fatalf("expected absence from %v, observed %v: %s", want, seen, text)
				}
			}
		})
	}
}

// A page size does not change which collection is read. Keep all scope
// and target filters, including S3's key prefix, in the lookup identity.
func deleteReadKey(r *http.Request) string {
	q := r.URL.Query()
	q.Del("limit")
	if q.Get("offset") == "0" {
		q.Del("offset")
	}
	if r.URL.Host == "s3.cloud.ru" {
		q.Del("encoding-type")
		q.Del("fetch-owner")
		q.Del("max-keys")
		if q.Get("delimiter") == "" {
			q.Del("delimiter")
		}
	}
	key := r.URL.Host + r.URL.Path
	if query := q.Encode(); query != "" {
		key += "?" + query
	}
	return key
}

// Where the specification has a terminal or deleting state, absence alone
// does not exercise D61. Keep the target present. The healthy control must
// reach the write; stop at that boundary without polling an operation.
// Plan 2.3 (docs/internal/audits/1.10-phase0/plan.md): pending is the
// known extra DELETE on main. Flip it to false when the zero-write control
// passes; keep both the healthy and the terminal case.
func TestDeleteTerminalStateOnlyShrinks(t *testing.T) {
	for _, tc := range []struct {
		tool, healthy, terminal, deletePath string
		pending                             bool
	}{
		{"destroy", "running", "deleting", "/api/v1/vms/" + liveID, false},
		{"pg_db_delete", "READY", "ERASING", "/v1/clusters/" + liveID + "/databases/" + liveName, true},
		{"pg_user_delete", "READY", "ERASING", "/v1/clusters/" + liveID + "/users/" + liveName, true},
		{"secret_delete", "ENABLED", "DESTROYED", "/v2/secret/" + liveName, true},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			for _, state := range []string{tc.healthy, tc.terminal} {
				stackHome(t)
				issued := false
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					w.Header().Set("Content-Type", "application/json")
					if r.Method != "GET" && r.Method != "HEAD" {
						if r.Method != "DELETE" || r.URL.Path != tc.deletePath {
							t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
						}
						issued = true
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"message":"test stopped at DELETE"}`))
						return true
					}
					// The VM list reports deletion in progress. Its next card
					// read completes the wait without a wall-clock delay.
					if tc.tool == "destroy" && state == tc.terminal && r.URL.Path == "/api/v1/vms/"+liveID {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"not found"}`))
						return true
					}

					item := map[string]any{"id": liveID, "name": liveName, "state": state, "status": state, "project_id": dbaasProject, "projectId": dbaasProject, "path": liveName}
					card := maps.Clone(item)
					for _, key := range []string{"items", "databases", "users"} {
						card[key] = []any{item}
					}
					card["total"] = 1
					_ = json.NewEncoder(w).Encode(card)
					return true
				}}
				reg := apiFakeS3Registry(t, fake)
				target := decls[tc.tool].Delete
				args := map[string]any{target.arg: liveID, "confirmed": true}
				if target.arg == "name" || target.arg == "path" {
					args[target.arg] = liveName
				}
				if target.parent != "" {
					args[target.parent] = liveID
				}
				out, text, failed := callNamed(t, reg, tc.tool, args)
				if state == tc.healthy {
					if !issued {
						t.Fatalf("healthy control never attempted deletion: %v %s", fake.calls, text)
					}
					continue
				}
				stateRefusal := failed && out["reason"] == "USAGE" && strings.Contains(strings.ToUpper(text), strings.ToUpper(state))
				if !issued && (len(fake.calls) == 0 || !(stateRefusal || !failed && deleteSettledInTest(out))) {
					t.Fatalf("terminal control inconclusive: %v %s", fake.calls, text)
				}
				if issued != tc.pending {
					t.Errorf("state %s: DELETE=%t, recorded exception=%t; update shrinking baseline after fixing the delete: %s", state, issued, tc.pending, strings.Join(fake.calls, ", "))
				}
			}
		})
	}
}

// When a delete no longer sends, it must also give a terminal or ongoing
// outcome. A malformed fixture or an unrelated refusal is inconclusive.
func deleteSettledInTest(out map[string]any) bool {
	switch out["status"] {
	case "deleted", "already_deleted", "deleting", "already_deleting":
		return true
	case "ok":
		return out["deleted_vm"] == liveID
	}
	return false
}

// stack_destroy delegates its cloud writes. Observe the delegate at the
// registry boundary as well as the state read from the stored job.
func TestStackDeleteReadsJobBeforeDelegating(t *testing.T) {
	for _, status := range []string{"done", "destroyed"} {
		t.Run(status, func(t *testing.T) {
			stackHome(t)
			r := newTestServer(t)
			job := planJob(t, `{"name":"fixture","secrets":[{"path":"fixture","value":"example"}]}`)
			step := job.Steps[0].Key
			job.Steps[0].Status = "done"
			job.Status = status
			job.Outputs[step] = "ok"
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			r.handlers["secret_delete"] = func(context.Context, map[string]any) (string, bool) {
				calls++
				return toJSON(map[string]any{"status": "deleted"}), false
			}
			out, text, failed := callNamed(t, r, "stack_destroy", map[string]any{"job_id": job.ID, "confirmed": true})
			wantCalls, wantStatus := 1, "destroyed"
			if status == "destroyed" {
				wantCalls, wantStatus = 0, "already_destroyed"
			}
			if failed || out["status"] != wantStatus || calls != wantCalls {
				t.Fatalf("job %s: %d child deletes, want %d; %s", status, calls, wantCalls, text)
			}
		})
	}
}
