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
// tool, the surface, the pid and the key the call was sent with (its
// account and its masked Key ID, as a deployment record keeps them).
package intent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// OutcomeUnknown tells a failed write whose outcome the cloud did not
// say, so its intent stays: the call was cut (INTERRUPTED), did not reach
// the cloud's answer (API_UNREACHABLE: a transport break after the request
// may have gone), failed in the API (API_ERROR: a 5xx may follow a done
// write) or without a code. A refusal with a code (USAGE, NOT_FOUND,
// AUTH_*, API_RATE_LIMITED…) says it, and so does an answer with a 4xx
// status, when the caller knows it: the API refused the request. status is
// the HTTP status of the answer, 0 when unknown — a tool's answer gives its
// reason only, and 4xx and 5xx do not part in it. One rule for the tools
// and for evo login («Риск», conclusion 46, condition 1; audit Fable 1.8,
// FB180-3).
func OutcomeUnknown(reason string, status int) bool {
	if status >= 400 && status < 500 {
		return false
	}
	switch reason {
	case clierrors.ReasonInterrupted, clierrors.CodeAPIUnreachable, clierrors.CodeAPIError, clierrors.ReasonGeneral:
		return true
	}
	return false
}

// Intent is one call that went to the cloud and has not answered yet.
type Intent struct {
	ID   string   `json:"id"`
	Tool string   `json:"tool"`
	Args []string `json:"args,omitempty"`
	Name string   `json:"name,omitempty"`
	// Target is the resource a call on an existing one names, as
	// «argument=id» (vm_id=…): a delete, a resize. Not a secret.
	Target string `json:"target,omitempty"`
	// ProjectID is the project the request names. A call that addresses
	// its resource by id and sends no project has none: the write lands
	// where the resource is, which the call's project does not tell (R-82).
	ProjectID string `json:"project_id,omitempty"`
	Zone      string `json:"zone,omitempty"`
	// Key is the key the call was sent with: a read by id answers for the
	// resource only with the key that saw it (state.Maker.Other). Nil in a
	// record of evo before 1.9.
	Key *state.Maker `json:"key,omitempty"`
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

// idForm is the form of the id begin gives a record: its file's name.
var idForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

// validID tells an id of evo's own form.
func validID(id string) bool { return idForm.MatchString(id) }

// recordPath is the file of the record id in dir; "" for an id not of
// evo's form — the path of a removal is never built from what a file says
// (FB180-1, R-67: the project directory comes with git clone).
func recordPath(dir, id string) string {
	if !validID(id) {
		return ""
	}
	return filepath.Join(dir, id+".json")
}

// Pending are the intents of root, oldest first. A record is read only in
// evo's form: a regular file named by an id of evo's, whose content names
// the same id; anything else is skipped, not read — a link, another name,
// another id (FB180-1).
func Pending(dir string) []Intent {
	out, _ := scan(dir)
	return out
}

// Foreign are the files of dir that look like records (*.json) but are
// not of evo's form: evo neither reads nor removes them, and doctor names
// them («Риск», conclusion 88: a record skipped silently hides a planted
// one). The names are the directory's, not evo's.
func Foreign(dir string) []string {
	_, foreign := scan(dir)
	return foreign
}

// scan reads dir once: the records of evo's form, and the paths of the
// other *.json files.
func scan(dir string) (out []Intent, foreign []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		var in Intent
		if e.Type().IsRegular() && validID(id) {
			if data, err := config.ReadFileNoFollow(recordPath(dir, id)); err == nil &&
				json.Unmarshal(data, &in) == nil && in.ID == id {
				in.Dir = dir
				out = append(out, in)
				continue
			}
		}
		foreign = append(foreign, filepath.Join(dir, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, foreign
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
			remove(dir, in.ID)
		}
	}
}

// Close removes the intent in: what the call did is now known.
func Close(in Intent) { remove(in.Dir, in.ID) }

// remove removes the record id of dir; an id not of evo's form removes
// nothing.
func remove(dir, id string) {
	if path := recordPath(dir, id); path != "" {
		_ = os.Remove(path)
	}
}
