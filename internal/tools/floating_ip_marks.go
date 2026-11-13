// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// withAddressMarks runs reserve with the marks of lost floating IPs the
// folder's state holds (floatingip) and writes back, under the lock, what
// it changed of them: a call that reserves an address for a VM reads them
// first and leaves one when its own create gets no answer (D107; «Риск»,
// conclusion 180, У3). A state that cannot be written is reported with
// the error of reserve: the mark is what destroy and doctor name.
func withAddressMarks(statePath string, reserve func(sf *state.File) error) (rerr, werr error) {
	sf, err := state.Read(statePath)
	if err != nil {
		sf = &state.File{}
	}
	before := slices.Clone(sf.LostFloatingIPs)
	rerr = reserve(sf)
	after := sf.LostFloatingIPs
	if len(before) == 0 && len(after) == 0 {
		return rerr, nil
	}
	werr = state.Update(statePath, stateLockWait, func(f *state.File) error {
		floatingip.Apply(f, before, after)
		return nil
	})
	return rerr, werr
}

// addressAsked is the question a call that would reserve a floating IP
// for VM vmName asks while the address an earlier call asked for with no
// answer is not listed (floatingip.Settle): nil when there is none. It
// reads only; the marks stay as they are.
func addressAsked(ctx context.Context, eng *engine.Engine, projectID, vmName string) *floatingip.AskError {
	sf, err := state.Read(state.DefaultPath(eng.StateDir))
	if err != nil || len(floatingip.Of(sf, projectID, vmName, "")) == 0 {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, engine.PreviewLookupTimeout)
	defer cancel()
	_, err = floatingip.Settle(lctx, eng.Cloud, sf, projectID, vmName, "", nil)
	ask, _ := floatingip.AsAsk(err)
	return ask
}

// addressQuestion is the answer of a call of tool that would reserve a
// floating IP while the address of an earlier call with no answer is not
// listed: the question of the call — answer, its preview with the price,
// when the call has one — with the address named in it, in the field and
// by the mechanism of a cut call (interrupted, read_tool; «Риск»,
// conclusions 176 and 180, Т1). Nothing was sent. Over MCP the question
// goes to the model, which is told to ask the person: the mark is answered
// now (closeAsked does so for a cut call), so the call with confirmed that
// follows reserves the address. A command of the CLI asks the person
// itself and answers it when they do (CutCallAnswered).
func (r *Registry) addressQuestion(ctx context.Context, tool string, eng *engine.Engine, ask *floatingip.AskError, answer map[string]any) string {
	m := ask.Mark
	price := pricing.FloatingIPMonthlyRub()
	out := map[string]any{}
	for k, v := range answer {
		out[k] = v
	}
	out["status"] = "confirmation_required"
	interrupted := map[string]any{"tool": tool, "read_tool": "list_floating_ips", "floating_ip": m.Name,
		"started_at": m.RequestedAt.UTC().Format(time.RFC3339), "calls": 1, "floating_ip_monthly_rub": price}
	message := i18n.Tf("Floating IP %s, which an earlier %s asked for %s, got no answer from the cloud, and %s does not show it now: the request may have been accepted, and the address billed. No second address was requested. A yes reserves another address, billed at about %s ₽ a month, beside the first one if that one comes: check %s for the name %s again first; whether to reserve another is the person's to answer — ask them, and with their yes call %s with confirmed=true.",
		"Floating IP %s, который запросил прошлый вызов %s %s, не получил ответа облака, и %s сейчас его не показывает: запрос мог быть принят, а адрес — тарифицироваться. Второй адрес не запрашивался. «Да» зарезервирует ещё один адрес, около %s ₽ в месяц, рядом с первым, если тот появится: сначала ещё раз проверьте %s по имени %s; резервировать ли другой адрес, решает человек — спросите его и с его «да» вызовите %s с confirmed=true.",
		m.Name, tool, m.RequestedAt.Local().Format("02.01.2006 15:04"), "list_floating_ips",
		fmt.Sprintf("%.2f", price), "list_floating_ips", m.Name, tool)
	if was, _ := answer["message"].(string); was != "" {
		message += " " + was
	}
	out["message"], out["interrupted"] = message, interrupted
	if callerOf(ctx) == nil {
		_ = state.Update(state.DefaultPath(eng.StateDir), stateLockWait, func(f *state.File) error {
			floatingip.Answer(f, m.ProjectID, m.Name)
			return nil
		})
	}
	return toJSON(out)
}

// addressAnswered records that a person answered at the terminal the
// question that named a lost floating IP (interrupted.floating_ip): the
// mark stays, and the next call reserves an address without asking.
func (r *Registry) addressAnswered(args map[string]any, interrupted any) {
	var in map[string]any
	switch v := interrupted.(type) {
	case map[string]any:
		in = v
	default:
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &in)
	}
	name, _ := in["floating_ip"].(string)
	if name == "" {
		return
	}
	eng, err := r.engineOfCall(args)
	if err != nil || eng == nil {
		return
	}
	project := firstNonEmptyStr(stringArg(args, "project_id"), eng.Config.ProjectID)
	_ = state.Update(state.DefaultPath(eng.StateDir), stateLockWait, func(f *state.File) error {
		floatingip.Answer(f, project, name)
		return nil
	})
}
