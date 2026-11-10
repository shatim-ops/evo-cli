// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// beginIntent writes the intent of a consented call before it goes to the
// cloud, at the one gate of consent (D35): a process cut between the
// consent and the cloud's answer leaves it, and the next run names it
// (release plan 1.8, (и); «Риск», conclusion 46). Every consented call
// writes one — a create, a delete, a change: each can leave the cloud in a
// state no record names. end takes the answer: an answer with an outcome
// removes the intent and settles the earlier ones of the same tool and
// name; a cut call (its context done, INTERRUPTED) keeps it. An intent
// that cannot be written refuses the call before it goes out.
func (r *Registry) beginIntent(ctx context.Context, name string, args map[string]any) (end func(answer string), err error) {
	if r.newEngine == nil {
		return func(string) {}, nil
	}
	// No engine: the call cannot reach the cloud, its handler answers that
	// itself.
	eng, err := r.newEngine(nil)
	if err != nil || eng == nil {
		return func(string) {}, nil
	}
	root := eng.StateDir
	in := intent.Intent{Tool: name, Surface: "mcp", PID: os.Getpid(), StartedAt: clock.Now(),
		ProjectID: eng.Config.ProjectID, Zone: eng.Config.Region}
	// The surface as the journal (D46) has it: a call of the CLI carries
	// its Caller, a call over MCP none.
	if callerOf(ctx) != nil {
		in.Surface = "cli"
	}
	for k := range args {
		if k != "confirmed" {
			in.Args = append(in.Args, k)
		}
	}
	in.Name, in.Target = intentResource(name, args)
	if p, _ := args["project_id"].(string); p != "" {
		in.ProjectID = p
	}
	if z, _ := args["zone"].(string); z != "" {
		in.Zone = z
	}
	// The engine of evo always has its directory (buildEngine fails
	// without the working directory); one without is a test's fake.
	if root == "" {
		return func(string) {}, nil
	}
	done, err := intent.Begin(root, in)
	if err == nil {
		return func(answer string) {
			if ctx.Err() != nil || outcomeUnknown(answer) {
				return
			}
			done()
			intent.Settle(root, name, in.Name, in.Target)
		}, nil
	}
	return nil, clierrors.Wrap(clierrors.CodeConfigInvalid,
		i18n.Tf("could not write what evo is about to do to %s (%v); nothing was sent to the cloud",
			"не удалось записать, что evo собирается сделать, в %s (%v); в облако ничего не отправлено", intent.Dir(root), err),
		i18n.Tf("Make %s writable (evo keeps there what a cut call may have left in the cloud), then repeat the call.",
			"Сделайте каталог %s доступным для записи (evo хранит там, что мог оставить в облаке прерванный вызов) и повторите вызов.", config.DataDir(root)), err)
}

// intentResource is what a call names its resource by: the argument the create
// declares (createdIDs: name, bucket, path…), else name; and, for a call on
// an existing resource, its first declared id argument (idArgs) as
// «argument=id» («Риск», conclusion 46, condition 2).
func intentResource(tool string, args map[string]any) (name, target string) {
	arg := "name"
	if c, ok := createdIDs[tool]; ok && c.arg != "" {
		arg = c.arg
	}
	name, _ = args[arg].(string)
	for _, a := range idArgs[tool] {
		if a.why == whyProjectArg {
			continue
		}
		if v, _ := args[a.name].(string); v != "" {
			return name, a.name + "=" + v
		}
	}
	return name, ""
}

// outcomeUnknown tells an answer that does not say what the cloud did: the
// call was cut (INTERRUPTED), did not reach the cloud's answer
// (API_UNREACHABLE: a transport break after the request may have gone),
// failed in the API (API_ERROR: 5xx and 4xx do not part in the answer, and a
// 5xx may follow a done write) or without a code. A success and a
// refusal with a code (USAGE, NOT_FOUND, AUTH_*, API_RATE_LIMITED…) say it
// («Риск», conclusion 46, condition 1).
func outcomeUnknown(answer string) bool {
	switch answerReason(answer) {
	case clierrors.ReasonInterrupted, clierrors.CodeAPIUnreachable, clierrors.CodeAPIError, clierrors.ReasonGeneral:
		return true
	}
	return false
}

// answerReason is the reason of a tool's answer, "" when it has none.
func answerReason(answer string) string {
	var a struct {
		Reason string `json:"reason"`
		Error  struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(answer), &a) != nil {
		return ""
	}
	if a.Reason != "" {
		return a.Reason
	}
	return a.Error.Reason
}

// PendingNotes are the intents root's earlier runs left, in words: what
// was sent, where, when, and where to look — no advice to delete: the
// resource may be the person's now (R-56). The lines are the same in
// doctor, in the first answer of a tool and in the CLI's stderr.
func (r *Registry) PendingNotes(root string) []string {
	var out []string
	for _, in := range intent.Pending(root) {
		where := ""
		if in.ProjectID != "" {
			where = i18n.Tf(" in project %s", " в проекте %s", in.ProjectID)
		}
		if in.Zone != "" {
			where += i18n.Tf(", zone %s", ", зона %s", in.Zone)
		}
		name := in.Name
		if name == "" {
			name = i18n.T("(no name)", "(без имени)")
		}
		out = append(out, i18n.Tf("%s %s%s, started %s (%s, pid %d): the cloud did not answer, the outcome is unknown — check %s for the name %s; the same %s with this name settles it",
			"%s %s%s, начат %s (%s, pid %d): облако не ответило, исход неизвестен — проверьте %s по имени %s; тот же %s с этим именем закроет запись",
			in.Tool, name, where, in.StartedAt.Local().Format("02.01.2006 15:04"), in.Surface, in.PID, r.listToolOf(in.Tool), name, in.Tool))
	}
	return out
}

// listToolOf is the read that names what tool may have made: <service>_list
// of a <service>_create, vm_list of the VM tools, else the tool itself
// (its preview names the resource).
func (r *Registry) listToolOf(tool string) string {
	switch tool {
	case "provision", "deploy", "deploy_apply", "destroy":
		return "vm_list"
	case "stack_apply", "stack_destroy":
		return "stack_status"
	}
	if c, ok := createdIDs[tool]; ok && c.list != "" {
		return c.list
	}
	for _, suffix := range []string{"_create", "_delete"} {
		if base, ok := strings.CutSuffix(tool, suffix); ok {
			if _, found := r.Lookup(base + "_list"); found {
				return base + "_list"
			}
		}
	}
	return tool
}

// withPending adds pending_operations — the intents earlier runs in this
// directory left — to the first answer over MCP: the agent reads them
// before it creates the same thing again (release plan 1.8, (и)).
func (r *Registry) withPending(answer string) (string, bool) {
	root, err := os.Getwd()
	if err != nil {
		return answer, true
	}
	notes := r.PendingNotes(root)
	if len(notes) == 0 {
		return answer, true
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer, false // not an object: the next answer carries them
	}
	m["pending_operations"] = notes
	return toJSON(m), true
}

// notePending is answer with the intents of earlier runs, on the first JSON
// answer of the process; the lock is released however withPending ends.
func (r *Registry) notePending(answer string) string {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if !r.pendingNoted {
		answer, r.pendingNoted = r.withPending(answer)
	}
	return answer
}
