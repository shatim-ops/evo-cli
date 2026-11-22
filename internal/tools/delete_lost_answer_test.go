// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Doctor reads the key and its projects through separate HTTP clients.
// Compute still uses the fixture that records the interrupted delete.
func interruptedDeleteRegistry(t *testing.T, compute http.RoundTripper) (*Registry, string) {
	t.Helper()
	stubDoctorIdentity(t)
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: compute}, "")
	return fastRegistry(t, cloud, provisionCfg)
}

func stubDoctorIdentity(t *testing.T) {
	t.Helper()
	// staticToken names service account x. Doctor lists its keys and asks
	// for its roles only in the project of this fixture.
	filter := strings.ReplaceAll(url.QueryEscape("resource_id="+provisionCfg.ProjectID+" AND subject_id=x"), "+", "%20")
	reads := map[string]bool{
		"https://organization.api.cloud.ru/v3/projects":                true,
		"https://iam.api.cloud.ru/api/v3/permissions?filter=" + filter: true,
		"https://iam.api.cloud.ru/api/v2/serviceAccounts/x/accessKeys": true,
	}
	old := httpretry.DefaultBase
	t.Cleanup(func() { httpretry.DefaultBase = old })
	httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// doctorOnFake points both reachability probes at one local server.
		localProbe := req.URL.Scheme == "http" && req.URL.String() == ComputeBaseURL+"/"
		if req.Method != http.MethodGet || !(localProbe || reads[req.URL.String()]) {
			t.Errorf("unexpected doctor request: %s %s", req.Method, req.URL)
		}
		rec := httptest.NewRecorder()
		http.NotFound(rec, req)
		return rec.Result(), nil
	})
}

// lostDeleteWorld is a cloud that has the resource named liveID or
// liveName (liveWorld) until the first write: the write is taken — from
// then on the resource is gone, as in goneWorld — and its answer is lost.
// The parent of the resource stays.
type lostDeleteWorld struct {
	tool   string
	mu     sync.Mutex
	taken  bool
	writes []string
}

const (
	lostDeleteParentID   = "22220000-0000-4000-8000-000000000002"
	lostDeleteParentName = "parent-thing"
)

func (w *lostDeleteWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.writes = append(w.writes, r.Method+" "+r.URL.Host+r.URL.Path)
		if !w.taken {
			w.taken = true
			return nil, errors.New("read tcp: connection reset by peer")
		}
	}
	// The parent stays visible after the resource DELETE. Populate the
	// same scoped collections the real callers consult on the repeat.
	if r.Method == http.MethodGet {
		switch {
		case (w.tool == "sg_rule_delete" || w.tool == "close_ports") && r.URL.Path == "/api/v1/security-groups":
			fmt.Fprintf(rec, `{"items":[{"id":%q,"name":"fixture-group"}],"total":1}`, lostDeleteParentID)
			return rec.Result(), nil
		case (w.tool == "sg_rule_delete" || w.tool == "close_ports") && r.URL.Path == "/api/v1/security-groups/"+lostDeleteParentID+"/rules":
			if w.taken {
				fmt.Fprint(rec, `{"items":[],"total":0}`)
			} else {
				fmt.Fprintf(rec, `{"items":[{"id":%q,"direction":"ingress","ether_type":"IPv4","ip_protocol":"tcp","port_range":"80:80","remote_ip_prefix":"0.0.0.0/0","state":"created"}],"total":1}`, liveID)
			}
			return rec.Result(), nil
		case w.tool == "dns_zone_delete" && r.URL.Path == "/v1/publicZones":
			if w.taken {
				fmt.Fprint(rec, `{"zones":[]}`)
			} else {
				fmt.Fprintf(rec, `{"zones":[{"id":%q,"domain":"fixture.example."}]}`, liveID)
			}
			return rec.Result(), nil
		case w.tool == "k8s_nodepool_delete" && r.URL.Path == "/v3/nodePools/"+liveID && !w.taken:
			fmt.Fprintf(rec, `{"id":%q,"clusterId":%q,"status":"RUNNING"}`, liveID, lostDeleteParentID)
			return rec.Result(), nil
		case w.tool == "k8s_nodepool_delete" && r.URL.Path == "/v3/nodePools":
			fmt.Fprintf(rec, `{"nodePools":[{"id":%q,"clusterId":%q,"status":"RUNNING"}]}`, liveID, lostDeleteParentID)
			return rec.Result(), nil
		}
	}
	if !w.taken {
		if w.tool == "s3_bucket_delete" && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Host, "s3.cloud.ru") {
			goneWorld(rec, r) // existing, empty bucket
			return rec.Result(), nil
		}
		liveWorld(rec, r)
		return rec.Result(), nil
	}
	gone := strings.NewReplacer(liveID, goneID, liveName, goneName)
	u := *r.URL
	u.Path, u.RawQuery = gone.Replace(u.Path), gone.Replace(u.RawQuery)
	was := r.Clone(r.Context())
	was.URL = &u
	goneWorld(rec, was)
	return rec.Result(), nil
}

func (w *lostDeleteWorld) sent() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.writes...)
}

// lostDeleteUnreached are the deletes this world does not bring to their
// request, each with why: the class says nothing of them yet. The list
// only shrinks. Closes by 1.10.
var lostDeleteUnreached = map[string]string{}

// lostDeleteSaysNeverExisted are the deletes whose client sends the DELETE
// again after the lost answer and reads the 404 to its own repeat as
// «already deleted»: the call that removed the resource answers that it
// did not exist («deleted earlier, or the id is wrong») though it read it
// alive before the question. The list only shrinks. Closes by 1.10.
var lostDeleteSaysNeverExisted = map[string]string{}

// Release plan 1.9, «Находки промежуточного дельта-аудита», class 1, for
// the deletes: the request is taken and its answer is lost. For every
// delete of Decl.Delete, on one consent:
//
//   - the call sends nothing but the delete of its target (a repeat of the
//     same DELETE is the same request);
//   - the call does not answer that the resource was never there: it read
//     it alive, and what removed it was this call (the misses are
//     lostDeleteSaysNeverExisted);
//   - the same call again ends without an error — the resource is gone,
//     already_deleted or deleted (D26) — and sends no write.
//
// The world is the registry's, not a list: a new delete is in the class
// or in lostDeleteUnreached with why.
func TestDeleteLostAnswer_TheRepeatConverges(t *testing.T) {
	var names []string
	for name := range allDeletes() {
		names = append(names, name)
	}
	sort.Strings(names)
	reached := 0
	for _, name := range names {
		d := decls[name].Delete
		args := goneArgs(name, d)
		for k, v := range args {
			switch v {
			case goneID:
				args[k] = liveID
			case goneName:
				args[k] = liveName
			}
		}
		if d.parent != "" {
			args[d.parent] = lostDeleteParentID
			if d.parent == "bucket" {
				args[d.parent] = lostDeleteParentName
			}
		}
		args["confirmed"] = true

		w := &lostDeleteWorld{tool: name}
		stackHome(t)
		bare := httpretry.DefaultBase
		httpretry.DefaultBase = w
		dir := t.TempDir()
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
		cloud.Backoff = time.Millisecond // the repeat of a dropped DELETE (computeRetry)
		r := New(func(map[string]any) (*engine.Engine, error) {
			var buf bytes.Buffer
			return &engine.Engine{
				Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
					S3: config.S3Config{AccessKey: "a", SecretKey: "s"}},
				Auth:     staticToken{},
				Cloud:    cloud,
				Printer:  output.NewWithWriter(&buf, output.ModeText, false),
				StateDir: dir,
			}, nil
		})
		r.SetStateDir(dir)
		first, firstText, firstErr := callNamed(t, r, name, args)
		sent := w.sent()
		again, againText, againErr := callNamed(t, r, name, args)
		repeated := w.sent()[len(sent):]
		t.Logf("%s first=%v status=%v reason=%v repeat=%v status=%v", name, sent, first["status"], first["reason"], repeated, again["status"])
		if (name == "sg_rule_delete" || name == "close_ports") && (len(sent) != 1 || !strings.HasSuffix(sent[0], "/security-groups/"+lostDeleteParentID+"/rules/"+liveID)) {
			t.Errorf("%s: wrong number or target of DELETE: %v", name, sent)
		}
		httpretry.DefaultBase = bare

		if len(sent) == 0 {
			if lostDeleteUnreached[name] == "" {
				t.Errorf("%s: the world did not bring the delete to its request, and lostDeleteUnreached does not say why: %s", name, firstText)
			}
			continue
		}
		if lostDeleteUnreached[name] != "" {
			t.Errorf("%s: lostDeleteUnreached names a delete the world reaches now: remove the line (the list only shrinks)", name)
		}
		reached++
		for _, write := range sent {
			if write != sent[0] || !strings.HasPrefix(write, http.MethodDelete+" ") {
				t.Errorf("%s: one consent sent more than the delete of its target: %v", name, sent)
				break
			}
		}
		never := !firstErr && first["status"] == "already_deleted"
		switch {
		case never && lostDeleteSaysNeverExisted[name] == "":
			t.Errorf("%s: the call that removed the resource (%v) answers that it was not there: %s", name, sent, firstText)
		case !never && lostDeleteSaysNeverExisted[name] != "":
			t.Errorf("%s: lostDeleteSaysNeverExisted names a delete that answers its own delete truly now: remove the line (the list only shrinks): %s", name, firstText)
		}
		if againErr || (again["status"] != "already_deleted" && again["status"] != "deleted") {
			t.Errorf("%s: the repeat of a delete whose answer was lost does not end as «the resource is gone»:\nfirst: %s\nagain: %s", name, firstText, againText)
		}
		if len(repeated) != 0 {
			t.Errorf("%s: the repeat wrote to a cloud that has no such resource: %v", name, repeated)
		}
	}
	for name := range lostDeleteSaysNeverExisted {
		if _, ok := deleteOf(name); !ok {
			t.Errorf("lostDeleteSaysNeverExisted names %s, which Decl.Delete does not have", name)
		}
	}
	if want := len(names) - len(lostDeleteUnreached); reached != want || reached < 20 {
		t.Errorf("the world reached %d deletes, want %d of %d (at least 20)", reached, want, len(names))
	}
	t.Logf("deletes: %d, reached: %d, answer their own delete as «was not there»: %d", len(names), reached, len(lostDeleteSaysNeverExisted))
}
