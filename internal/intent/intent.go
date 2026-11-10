// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package intent keeps what evo is about to change in the cloud, from the
// consent until the cloud answers: a process killed in between (kill -9,
// a closed terminal, a crashed agent) leaves the intent, and the next run
// names it — the resource may exist and be billed with no record of it
// (release plan 1.8, (и); «Риск», conclusion 46; R-59, cloud-e2e 181).
//
// An intent is one file in the data directory next to state.json:
// <data dir>/intents/<id>.json, 0600, written whole (temp file and
// rename). It holds no secret and no value of an argument but the name of
// the resource, the project and the zone: the names of the arguments, the
// tool, the surface and the pid.
package intent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Intent is one call that went to the cloud and has not answered yet.
type Intent struct {
	ID   string   `json:"id"`
	Tool string   `json:"tool"`
	Args []string `json:"args,omitempty"`
	Name string   `json:"name,omitempty"`
	// Target is the resource a call on an existing one names, as
	// «argument=id» (vm_id=…): a delete, a resize. Not a secret.
	Target    string    `json:"target,omitempty"`
	ProjectID string    `json:"project_id,omitempty"`
	Zone      string    `json:"zone,omitempty"`
	Surface   string    `json:"surface"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// Dir is the directory of the intents of root's data directory.
func Dir(root string) string { return filepath.Join(config.DataDir(root), "intents") }

// Begin writes in before the call goes out and returns the end of it: done
// removes the file; the caller does not call it when the outcome is not
// known (the call was cut). An error means nothing was written: the
// caller refuses the call before it goes out.
func Begin(root string, in Intent) (done func(), err error) {
	dir := Dir(root)
	if err := config.MkdirPrivate(dir); err != nil {
		return nil, err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	in.ID = hex.EncodeToString(b[:])
	sort.Strings(in.Args)
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, in.ID+".json")
	if err := config.WriteFileAtomic(path, data, 0o600); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

// Pending are the intents of root, oldest first; a file that does not read
// is skipped.
func Pending(root string) []Intent {
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		return nil
	}
	var out []Intent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(root), e.Name()))
		if err != nil {
			continue
		}
		var in Intent
		if json.Unmarshal(data, &in) == nil && in.ID != "" {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// Settle removes the earlier intents of the same tool on the same resource
// in the same project as done (its name or its target): a later call of it
// answered, so what the cut one did is now known to that call — an
// existing resource by the name (D49), a new one, or «already deleted»
// (D26). Another project's record of the same name is not this one.
func Settle(root string, done Intent) {
	if done.Name == "" && done.Target == "" {
		return
	}
	for _, in := range Pending(root) {
		if in.Tool == done.Tool && in.Name == done.Name && in.Target == done.Target && in.ProjectID == done.ProjectID {
			_ = os.Remove(filepath.Join(Dir(root), in.ID+".json"))
		}
	}
}

// Close removes the intent id: what the call did is now known.
func Close(root, id string) { _ = os.Remove(filepath.Join(Dir(root), id+".json")) }
