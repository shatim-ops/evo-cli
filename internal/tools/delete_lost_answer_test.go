// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
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

// lostDeleteWorld is a cloud that has the resource named liveID or
// liveName (liveWorld) until the first write: the write is taken — from
// then on the resource is gone, as in goneWorld — and its answer is lost.
// The parent of the resource stays.
type lostDeleteWorld struct {
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
	if !w.taken {
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
var lostDeleteUnreached = map[string]string{
	"close_ports":         "the rule of a port is found in the list of rules of the group; liveWorld lists no rule of a port",
	"dns_zone_delete":     "the zone is read from the list of zones; liveWorld lists none",
	"k8s_nodepool_delete": "the node pool is read with its cluster in the body; liveWorld names no cluster there (unreachedDeletes, #1465)",
	"s3_bucket_delete":    "the bucket must be empty before the question; liveWorld lists an object in it",
	"sg_rule_delete":      "the group is found in the list of groups; the list of the world names liveID, and the rule and its group cannot both be it",
}

// lostDeleteSaysNeverExisted are the deletes whose client sends the DELETE
// again after the lost answer and reads the 404 to its own repeat as
// «already deleted»: the call that removed the resource answers that it
// did not exist («deleted earlier, or the id is wrong») though it read it
// alive before the question. The list only shrinks. Closes by 1.10.
var lostDeleteSaysNeverExisted = map[string]string{
	"backup_delete":       "the compute client repeats the DELETE; the 404 to the repeat is answered as already_deleted",
	"destroy":             "the compute client repeats the DELETE; the answer is «VM already deleted (not found in the cloud)»",
	"disk_delete":         "the compute client repeats the DELETE; existed false, «deleted earlier, or the disk_id is wrong»",
	"image_delete":        "the compute client repeats the DELETE; the 404 to the repeat is answered as already_deleted",
	"release_floating_ip": "the compute client repeats the DELETE; the 404 to the repeat is answered as already_deleted",
	"snat_delete":         "the compute client repeats the DELETE; the 404 to the repeat is answered as already_deleted",
	"subnet_delete":       "the compute client repeats the DELETE; the 404 to the repeat is answered as already_deleted",
}

// Release plan 1.9, «Находки промежуточного дельта-аудита», class 1, for
// the deletes: the request is taken and its answer is lost. For every
// delete of deleteTargets, on one consent:
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
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	reached := 0
	for _, name := range names {
		d := deleteTargets[name]
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

		w := &lostDeleteWorld{}
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
		if _, ok := deleteTargets[name]; !ok {
			t.Errorf("lostDeleteSaysNeverExisted names %s, which deleteTargets does not have", name)
		}
	}
	if want := len(deleteTargets) - len(lostDeleteUnreached); reached != want || reached < 20 {
		t.Errorf("the world reached %d deletes, want %d of %d (at least 20)", reached, want, len(deleteTargets))
	}
	t.Logf("deletes: %d, reached: %d, answer their own delete as «was not there»: %d", len(deleteTargets), reached, len(lostDeleteSaysNeverExisted))
}
