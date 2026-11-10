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
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Intent is one call that went to the cloud and has not answered yet.
type Intent struct {
	ID   string   `json:"id"`
	Tool string   `json:"tool"`
	Args []string `json:"args,omitempty"`
	Name string   `json:"name,omitempty"`
	// Target is the resource a call on an existing one names, as
	// «argument=id» (vm_id=…): a delete, a resize. Not a secret.
	Target    string `json:"target,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Zone      string `json:"zone,omitempty"`
	// Note is what the next run says of the intent when the words of a
	// call that the cloud did not answer do not fit it (evo login killed
	// after its key: findings ledger 1.8, № 37); "" for a tool's call.
	Note      string    `json:"note,omitempty"`
	Surface   string    `json:"surface"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	// Dir is the directory the intent was read from (Pending); not saved.
	Dir string `json:"-"`
}

// Dir is the directory of the intents of root's data directory: the calls
// of the tools, which the folder's state follows.
func Dir(root string) string { return filepath.Join(config.DataDir(root), "intents") }

// ConfigDir is the directory of the intents of the configuration directory:
// what belongs to the profiles, not to a folder — evo login's key (№ 37,
// «Риск», conclusion 75: a login in one folder, doctor in another). ""
// when there is no configuration directory.
func ConfigDir() string {
	if d := config.Dir(); d != "" {
		return filepath.Join(d, "intents")
	}
	return ""
}

// Dirs are the directories of intents a run in root reads: the folder's
// and the configuration's.
func Dirs(root string) []string {
	out := []string{Dir(root)}
	if c := ConfigDir(); c != "" && c != out[0] {
		out = append(out, c)
	}
	return out
}

// Begin writes in before the call goes out and returns the end of it: done
// removes the file; the caller does not call it when the outcome is not
// known (the call was cut). An error means nothing was written: the
// caller refuses the call before it goes out.
func Begin(dir string, in Intent) (done func(), err error) {
	done, err = begin(dir, in)
	if err != nil {
		return nil, notWritten(dir, err)
	}
	return done, nil
}

func begin(dir string, in Intent) (done func(), err error) {
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

// notWritten is the refusal of a call whose intent could not be written:
// a local error, the call not sent (CONFIG_INVALID, code 2) — the same
// for a tool's call and for evo login («Риск», conclusion 75).
func notWritten(dir string, err error) error {
	return clierrors.Wrap(clierrors.CodeConfigInvalid,
		i18n.Tf("could not write what evo is about to do to %s (%v); nothing was sent to the cloud",
			"не удалось записать, что evo собирается сделать, в %s (%v); в облако ничего не отправлено", dir, err),
		i18n.Tf("Make %s writable (evo keeps there what a cut call may have left in the cloud), then repeat the call.",
			"Сделайте каталог %s доступным для записи (evo хранит там, что мог оставить в облаке прерванный вызов) и повторите вызов.", filepath.Dir(dir)), err)
}

// Pending are the intents of root, oldest first; a file that does not read
// is skipped.
func Pending(dir string) []Intent {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Intent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var in Intent
		if json.Unmarshal(data, &in) == nil && in.ID != "" {
			in.Dir = dir
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
func Settle(dir string, done Intent) {
	if done.Name == "" && done.Target == "" {
		return
	}
	for _, in := range Pending(dir) {
		if in.Tool == done.Tool && in.Name == done.Name && in.Target == done.Target && in.ProjectID == done.ProjectID {
			_ = os.Remove(filepath.Join(dir, in.ID+".json"))
		}
	}
}

// Close removes the intent in: what the call did is now known.
func Close(in Intent) { _ = os.Remove(filepath.Join(in.Dir, in.ID+".json")) }
