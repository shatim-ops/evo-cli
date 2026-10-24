// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
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
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
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
}

// deleteLike: a tool that changes the cloud and whose name says it
// deletes — the tools deleteTargets must declare.
func deleteLike(t Tool) bool {
	n := t.Name
	return t.Mutating && (strings.HasSuffix(n, "_delete") || strings.HasSuffix(n, "_rm") ||
		n == "destroy" || n == "release_floating_ip")
}

// Every delete tool is declared, with arguments its schema has.
func TestAlreadyDeleted_EveryDeleteDeclared(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range New(nil).Tools() {
		_, declared := deleteTargets[tl.Name]
		if deleteLike(tl) && !declared {
			t.Errorf("%s deletes a resource but is not in deleteTargets (D26: a repeated delete answers already_deleted)", tl.Name)
		}
		if !declared {
			continue
		}
		seen[tl.Name] = true
		d := deleteTargets[tl.Name]
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range []string{d.arg, d.parent} {
			if _, ok := props[a]; a != "" && !ok {
				t.Errorf("%s: deleteTargets names argument %q, which its schema has not", tl.Name, a)
			}
		}
	}
	for name := range deleteTargets {
		if !seen[name] {
			t.Errorf("deleteTargets names %s, which is not a tool", name)
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
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := deleteTargets[name]
		for _, confirmed := range []bool{false, true} {
			args := goneArgs(name, d)
			if confirmed {
				args["confirmed"] = true
			}
			m, text, isErr := callNamed(t, r, name, args)
			if isErr || m["status"] != "already_deleted" || m["existed"] != false || m[d.arg] != args[d.arg] {
				t.Errorf("%s confirmed=%v: want already_deleted, exit 0: %s", name, confirmed, text)
			}
		}
	}
}

// A parent that is not there is the call's error, not a repeat: NOT_FOUND.
func TestAlreadyDeleted_ParentGoneIsNotFound(t *testing.T) {
	r := goneRegistry(t)
	for name, d := range deleteTargets {
		if d.parent == "" {
			continue
		}
		for _, confirmed := range []bool{false, true} {
			args := map[string]any{d.arg: liveName, d.parent: goneID, "confirmed": confirmed}
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
	for name, d := range deleteTargets {
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
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
				S3: config.S3Config{AccessKey: "a", SecretKey: "s"}},
			Auth:    staticToken{},
			Cloud:   provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// Bound to its projects (evo mcp --allow-project), a delete of what is not
// there is NOT_FOUND, not already_deleted (owner's decision 22.10, D26; H9
// option b) — for every delete tool: the target the bound check places in
// no allowed project, and the one the tool itself looks up in the server's
// project (an S3 object, a secret, a database of a cluster of the project).
func TestAlreadyDeleted_BoundServerKeepsNotFound(t *testing.T) {
	r := goneRegistry(t)
	r.SetProjectBound()
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, confirmed := range []bool{false, true} {
			args := goneArgs(name, deleteTargets[name])
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
