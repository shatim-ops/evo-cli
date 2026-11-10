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
	root, project, zone := "", "", ""
	if r.newEngine != nil {
		eng, err := r.newEngine(nil)
		if err != nil {
			return nil, err
		}
		if eng != nil {
			root, project, zone = eng.StateDir, eng.Config.ProjectID, eng.Config.Region
		}
	}
	if root == "" {
		return func(string) {}, nil
	}
	in := intent.Intent{Tool: name, Surface: "mcp", PID: os.Getpid(), StartedAt: clock.Now(), ProjectID: project, Zone: zone}
	if liveCaller(ctx) != nil {
		in.Surface = "cli"
	}
	for k := range args {
		if k != "confirmed" {
			in.Args = append(in.Args, k)
		}
	}
	in.Name, _ = args["name"].(string)
	if p, _ := args["project_id"].(string); p != "" {
		in.ProjectID = p
	}
	if z, _ := args["zone"].(string); z != "" {
		in.Zone = z
	}
	done, err := intent.Begin(root, in)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeConfigInvalid,
			i18n.Tf("could not write what evo is about to do to %s (%v); nothing was sent to the cloud",
				"не удалось записать, что evo собирается сделать, в %s (%v); в облако ничего не отправлено", intent.Dir(root), err),
			i18n.Tf("Make %s writable (evo keeps there what a cut call may have left in the cloud), then repeat the call.",
				"Сделайте каталог %s доступным для записи (evo хранит там, что мог оставить в облаке прерванный вызов) и повторите вызов.", config.DataDir(root)), err)
	}
	return func(answer string) {
		if ctx.Err() != nil || answerReason(answer) == clierrors.ReasonInterrupted {
			return
		}
		done()
		intent.Settle(root, name, in.Name)
	}, nil
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
func (r *Registry) withPending(answer string) string {
	root, err := os.Getwd()
	if err != nil {
		return answer
	}
	notes := r.PendingNotes(root)
	if len(notes) == 0 {
		return answer
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer
	}
	m["pending_operations"] = notes
	return toJSON(m)
}
