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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

// DefiniteRefusal tells an HTTP status of the API's answer that says the
// request was not carried out: a 4xx other than 408 — a request that timed
// out on the way may have been taken. The one rule of it, for the record
// of a call (OutcomeUnknown) and for a call that re-reads the resource
// after an error (the interim delta audit of 1.9; «Риск», conclusion 176,
// У5).
func DefiniteRefusal(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout
}

// OutcomeUnknown tells a failed write whose outcome the cloud did not
// say, so its intent stays: the call was cut (INTERRUPTED), did not reach
// the cloud's answer (API_UNREACHABLE: a transport break after the request
// may have gone), failed in the API (API_ERROR: a 5xx may follow a done
// write) or without a code. A refusal with a code (USAGE, NOT_FOUND,
// AUTH_*, API_RATE_LIMITED…) says it, and so does an answer with a status
// that is a definite refusal (DefiniteRefusal), when the caller knows it;
// a 408 leaves it unknown whatever the reason. status is
// the HTTP status of the answer, 0 when unknown — a tool's answer gives its
// reason only, and 4xx and 5xx do not part in it. One rule for the tools
// and for evo login («Риск», conclusion 46, condition 1; audit Fable 1.8,
// FB180-3).
func OutcomeUnknown(reason string, status int) bool {
	if DefiniteRefusal(status) {
		return false
	}
	if status == http.StatusRequestTimeout {
		return true
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
	// Held tells a call that ended at its time limit with the operation
	// still going (its answer named the same tool as the next step): the
	// record stays until a call of that step ends the operation, and
	// StartedAt is when the operation began — the limit of its repeats
	// counts from there, not from each repeat (release plan 1.9, «Намерение
	// D92 держит запись, пока операция идёт»).
	Held bool `json:"held,omitempty"`
	// Print tells the same call again from another call of the tool on
	// the same resource (a rule of a security group, a kubectl): the hash
	// of the record's id and of the arguments that make the call what it
	// is (Canon). The values are not written — an argument may hold what
	// the file must not; the id in the hash only keeps two records of the
	// same call from showing it by an equal print, it is no defence of a
	// value. "" in a record of evo before the print: such a record is
	// told by its name and target.
	Print string `json:"print,omitempty"`
	// Ended tells a call that returned with the record kept: its outcome
	// is not known, and it is no longer in flight. A record without it
	// whose process lives is a call that has not returned (inFlight).
	Ended bool `json:"ended,omitempty"`
	// Sent tells a call whose first write went out: it is marked right
	// before that write is sent (MarkSent), so a process killed after it
	// leaves the mark.
	Sent bool `json:"sent,omitempty"`
	// Counts tells a record of a call whose every write marks it (Sent)
	// before it goes out: such a record of a call that is no longer in
	// flight, not ended and without Sent is of a call killed before it
	// sent anything, and no reader keeps it (Pending; «Риск», conclusion
	// 187, У2). A record without it — of evo before the mark — is kept as
	// it was (У6).
	Counts bool `json:"counts,omitempty"`
	// Machine is the machine the call ran on (thisMachine): a pid tells
	// a process only there. A directory of state may be shared — a volume
	// of containers, a network disk — and a call that runs on another
	// machine looks dead here: only a record of this machine is dropped as
	// one that sent nothing («Риск», conclusion 187, У2-3). A hash, not
	// the name of the host: the file names nothing of the machine.
	Machine string `json:"machine,omitempty"`
	// CutSend is when a call that went on with this held operation sent
	// its write again and was cut before the answer: the operation's
	// resource may have been changed once more, and the next call asks
	// rather than sends a third time («Риск», conclusion 177, У13). Zero
	// when no such send was cut.
	CutSend time.Time `json:"cut_send,omitempty"`
	// Canon is the canonical form of the arguments Print is made of; not
	// saved. The caller sets it before Open and keeps it for Settle.
	Canon []byte `json:"-"`
	// Dir is the directory the intent was read from (Pending); not saved.
	Dir string `json:"-"`
}

// thisMachine is the mark of the machine evo runs on: a hash of its host
// name; "" when the system does not tell it — then no record is taken
// for one of this machine.
func thisMachine() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("evo-machine\x00" + host))
	return hex.EncodeToString(sum[:8])
}

// printOf is the print of the arguments canon in the record id.
func printOf(id string, canon []byte) string {
	h := sha256.New()
	h.Write([]byte(id))
	h.Write([]byte{0})
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil))
}

// SameCall tells that in is a record of a call with the arguments canon:
// by its print. A record without a print (evo before the print) is told by
// the caller, by its name and target.
func (in Intent) SameCall(canon []byte) bool {
	return in.Print != "" && canon != nil && in.Print == printOf(in.ID, canon)
}

// inFlight tells a record of a call that may not have returned yet: it
// has not said it ended, and its process lives — this one, or another
// (two agents of one server, two terminals). In doubt it is in flight:
// its record is not another call's to remove.
func (in Intent) inFlight() bool {
	return !in.Ended && !in.Held && (in.PID == os.Getpid() || processAlive(in.PID))
}

// MarkSent marks the record in as a call whose write goes out (Sent). An
// error: the mark is not written, and the write must not go out — the
// record would say, after a kill, that nothing was sent.
func MarkSent(in Intent) error {
	in.Sent = true
	if err := write(in); err != nil {
		return notWritten(in.Dir, err)
	}
	return nil
}

// HoldCut keeps the record in of a cut call whose change goes on as the
// operation evo goes on with (Held), marked as the cut send it is
// (CutSend at at): the call that waits for it opens no record of its own,
// and the trace of the cut send outlives the wait.
func HoldCut(in Intent, at time.Time) {
	in.Held, in.CutSend = true, at
	_ = write(in)
}

// MarkCutSend marks the held record in with a send of its operation that
// was cut at at (CutSend).
func MarkCutSend(in Intent, at time.Time) {
	in.CutSend = at
	_ = write(in)
}

// End marks the record in as a call that returned with its outcome not
// known: the record stays, and a later call of the same may settle it.
func End(in Intent) {
	in.Ended = true
	_ = write(in)
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
	rec, err := Open(dir, in)
	if err != nil {
		return nil, err
	}
	return func() { Close(rec) }, nil
}

// Open writes in as Begin does and returns the record written, with its
// id and directory: Close removes it, Hold keeps it as an operation that
// goes on.
func Open(dir string, in Intent) (Intent, error) {
	if err := config.MkdirPrivate(dir); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	in.ID, in.Dir = hex.EncodeToString(b[:]), dir
	if in.Machine == "" {
		in.Machine = thisMachine()
	}
	if in.Canon != nil {
		in.Print = printOf(in.ID, in.Canon)
	}
	sort.Strings(in.Args)
	if err := write(in); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	return in, nil
}

// write writes the record in whole (temp file and rename).
func write(in Intent) error {
	path := recordPath(in.Dir, in.ID)
	if path == "" {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data, 0o600)
}

// Hold keeps the record in as an operation that goes on (Held), as the
// caller names it now: by the resource its next step names — a call that
// named a VM by name goes on by its id. A record that cannot be rewritten
// stays as it was written — a cut call, which is what the next run then
// says of it.
func Hold(in Intent) {
	in.Held = true
	_ = write(in)
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
//
// A record of a call that sent nothing and will not (Counts, not Sent,
// not in flight, not ended) is removed, not returned: the request did
// not go out, so there is nothing to look for. Its age does not matter.
func Pending(dir string) []Intent {
	all, _ := scan(dir)
	var out []Intent
	for _, in := range all {
		if in.Counts && !in.Sent && !in.Ended && !in.Held && in.Machine != "" && in.Machine == thisMachine() && !in.inFlight() {
			remove(dir, in.ID)
			continue
		}
		out = append(out, in)
	}
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

// Settle removes the earlier intents of the same call as done: a later
// call of it answered, so what the cut one did is now known to that call —
// an existing resource by the name (D49), a new one, or «already deleted»
// (D26). The same call is the same tool in the same project with the same
// arguments (SameCall): another rule of the same security group is another
// call, and its answer says nothing of this one. A record without a print
// (evo before the print) is the same call by its name or its target, as it
// was. Only a record that began no later than done and is not in flight is
// removed: the answer of one of two calls in flight says nothing of the
// other, whose record must outlive it (the interim delta audit of 1.9,
// FB19I-13 and FB19I-34; «Риск», conclusion 177, У4, У6 and У8). An
// operation going on (Held) is not a cut call: the call that ends it
// closes it. A record another key left is kept and returned: the key of
// done may not see what that key changed — its «already deleted» is a 404
// of its own view, not the outcome of the cut call (R-82, «Риск»,
// conclusion 128). A record with a print and no key is of a call whose
// key was not known: no repeat settles it.
func Settle(dir string, done Intent) (kept []Intent) {
	if done.Name == "" && done.Target == "" {
		return nil
	}
	for _, in := range Pending(dir) {
		if in.ID == done.ID || in.Tool != done.Tool || in.ProjectID != done.ProjectID {
			continue
		}
		if in.Print != "" {
			if !in.SameCall(done.Canon) {
				continue
			}
		} else if in.Name != done.Name || in.Target != done.Target {
			continue
		}
		if in.Key.Other(done.Key) {
			kept = append(kept, in)
			continue
		}
		if in.StartedAt.After(done.StartedAt) || in.Held || in.inFlight() || (in.Print != "" && in.Key == nil) {
			continue
		}
		remove(dir, in.ID)
	}
	return kept
}

// Close removes the intent in: what the call did is now known.
func Close(in Intent) { remove(in.Dir, in.ID) }

// remove removes the record id of dir; an id not of evo's form removes
// nothing.
func remove(dir, id string) {
	if path := recordPath(dir, id); path != "" {
		// A concurrent read of the record by another call blocks the
		// removal on Windows: it is retried (config.RemoveYielding).
		_ = config.RemoveYielding(path)
	}
}
