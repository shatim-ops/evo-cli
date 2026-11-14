// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// imageCloud is filterCloud whose cards name the image of the VM, as the
// published specification of Compute does: the item of the list of VMs
// carries no image, the card does.
type imageCloud struct {
	filterCloud
	// many more VMs of the list, all running in ru.AZ-1.
	many int
	// noImage is the VM whose card names no image; failing is the VM
	// whose card cannot be read, with failure (none: HTTP 502).
	noImage, failing string
	failure          error
	cards            []string
}

const (
	imgUbuntu = "00000000-0000-4000-8000-0000000000b1"
	imgAstra  = "00000000-0000-4000-8000-0000000000b2"
)

func (c *imageCloud) ListVMsInProject(ctx context.Context, p string) ([]*provision.VM, error) {
	vms, _ := c.filterCloud.ListVMsInProject(ctx, p)
	for i := 0; i < c.many; i++ {
		vms = append(vms, &provision.VM{ID: fmt.Sprintf("00000000-0000-4000-8000-0000000001%02x", i), Name: fmt.Sprintf("bulk-%d", i), Status: "running",
			AvailabilityZone: &provision.ZoneRef{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"}})
	}
	return vms, nil
}

func (c *imageCloud) GetVMDetail(_ context.Context, id string) (*provision.VMDetail, error) {
	c.cards = append(c.cards, id)
	if id == c.failing {
		if c.failure != nil {
			return nil, c.failure
		}
		return nil, errors.New("HTTP 502")
	}
	det := &provision.VMDetail{ID: id, Name: "vm", State: "running"}
	switch {
	case id == c.noImage:
	case strings.HasSuffix(id, "3"):
		det.Image = &provision.ZoneRef{ID: imgAstra, Name: "astra-1.8"}
	default:
		det.Image = &provision.ZoneRef{ID: imgUbuntu, Name: "ubuntu-22.04"}
	}
	return det, nil
}

// Release plan 1.9, Вп-10, «images»: the list of VMs selects by the image.
// The item of the list carries no image (the published specification), so
// evo reads the card of each VM that passed the other filters and checks
// the selection itself: by the id or by the name of the image, any case.
// The rows of the selection name the image they were selected by.
func TestVMList_SelectsByImages(t *testing.T) {
	const web1, web2, db1 = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"
	run := func(c *imageCloud, args map[string]any) (map[string]any, bool, string) {
		t.Helper()
		r, _ := targetRegistry(t, c)
		tl, _ := r.Lookup("vm_list")
		text, isErr := tl.Handler(context.Background(), args)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m, isErr, text
	}
	names := func(m map[string]any) string {
		vms, _ := m["vms"].([]any)
		var out []string
		for _, v := range vms {
			row, _ := v.(map[string]any)
			img, _ := row["image"].(map[string]any)
			out = append(out, stringArg(row, "name")+"="+stringArg(img, "name"))
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		args  map[string]any
		want  string
		cards int
	}{
		{map[string]any{"images": []any{"ubuntu-22.04"}}, "web-1=ubuntu-22.04,web-2=ubuntu-22.04", 3},
		{map[string]any{"images": []any{strings.ToUpper(imgAstra)}}, "db-1=astra-1.8", 3},
		{map[string]any{"images": []any{"UBUNTU-22.04", imgAstra}}, "db-1=astra-1.8,web-1=ubuntu-22.04,web-2=ubuntu-22.04", 3},
		{map[string]any{"images": []any{"no-such-image"}}, "", 3},
		// The cards are read only of what the other filters left.
		{map[string]any{"images": []any{"ubuntu-22.04"}, "state": "stopped"}, "web-2=ubuntu-22.04", 1},
		{map[string]any{"images": []any{"astra-1.8"}, "ids": []any{web1, db1}}, "db-1=astra-1.8", 2},
		{map[string]any{"images": []any{"astra-1.8"}, "name": "nothing"}, "", 0},
	} {
		cloud := &imageCloud{}
		m, isErr, text := run(cloud, c.args)
		if isErr {
			t.Errorf("%v: an error, want %q: %s", c.args, c.want, text)
			continue
		}
		if got := names(m); got != c.want {
			t.Errorf("%v: %q, want %q", c.args, got, c.want)
		}
		if len(cloud.cards) != c.cards {
			t.Errorf("%v: %d cards read, want %d: only of what the other filters left", c.args, len(cloud.cards), c.cards)
		}
		// The state goes to the cloud too (inCloud): then evo did not
		// see the whole list, and the answer names no total.
		_, inCloud := c.args["state"]
		if f, _ := m["filters"].(map[string]any); f["images"] == nil || (!inCloud && (m["filtered_by"] != "evo" || m["total"] != float64(3))) {
			t.Errorf("%v: the answer does not name the filter, who selected and of how many: %s", c.args, text)
		}
	}

	// Without the filter the list reads no card and its rows are as before.
	cloud := &imageCloud{}
	m, _, text := run(cloud, map[string]any{})
	if len(cloud.cards) != 0 || strings.Contains(text, `"image"`) || len(m["vms"].([]any)) != 3 {
		t.Errorf("vm_list without images read %d cards, or its rows changed: %s", len(cloud.cards), text)
	}

	// A card that cannot be read, or names no image, is no «not of this
	// image»: the selection is not answered in part.
	for _, c := range []*imageCloud{{failing: web2}, {noImage: web2}} {
		m, isErr, text := run(c, map[string]any{"images": []any{"ubuntu-22.04"}})
		if !isErr || m["vms"] != nil {
			t.Errorf("failing=%q noImage=%q: want an error and no part of the selection, got %s", c.failing, c.noImage, text)
		}
	}

	// The failure names what the bare error of the read does not («Риск»,
	// conclusion 149): the filter, the VM, how many cards were read of how
	// many, that nothing was selected — and a hint by the cause: a VM gone
	// since the list is «send again», a call out of its time is «narrow»,
	// not «retry later»; the reason stays the read's (429 is 429).
	for _, c := range []struct {
		failure      error
		reason, hint string
	}{
		{clierrors.New(clierrors.CodeVMNotFound, "vm not found", ""), "VM_NOT_FOUND", "Send the same call again"},
		// A read out of its own time while the call has time left keeps the
		// read's reason: the network, not the width of the selection.
		{context.DeadlineExceeded, "API_UNREACHABLE", "narrow the list with: zone"},
		{&provision.StatusError{Status: 429, Body: "{}"}, "API_RATE_LIMITED", ""},
	} {
		m, isErr, text := run(&imageCloud{failing: web2, failure: c.failure}, map[string]any{"images": []any{"ubuntu-22.04"}})
		msg := stringArg(m, "error")
		if !isErr || m["vms"] != nil || !strings.Contains(msg, "filter images") || !strings.Contains(msg, web2) ||
			!strings.Contains(msg, "(1 of 3 read)") || !strings.Contains(msg, "nothing was selected") {
			t.Errorf("%v: the failure does not name the filter, the VM, the count and that nothing was selected: %s", c.failure, text)
		}
		if c.reason != "" && stringArg(m, "reason") != c.reason {
			t.Errorf("%v: reason %v, want %s: %s", c.failure, m["reason"], c.reason, text)
		}
		if hint := stringArg(m, "hint"); !strings.Contains(hint, c.hint) || (c.failure == context.DeadlineExceeded && strings.Contains(strings.ToLower(hint), "later")) {
			t.Errorf("%v: hint %q, want %q", c.failure, hint, c.hint)
		}
		// A read out of its own time is not the call out of its time: the
		// words do not say what the reason does not («Риск», conclusion 231).
		if c.failure == context.DeadlineExceeded && (strings.Contains(msg, "call ran out") || strings.Contains(stringArg(m, "hint"), "will run out the same way")) {
			t.Errorf("a read out of its own time words itself as the call out of its time: %s", text)
		}
	}

	// More VMs than evo reads cards of in one call: a refusal that names
	// the number and the filters that narrow, with no card read.
	cloud = &imageCloud{many: cardReads}
	m, isErr, text := run(cloud, map[string]any{"images": []any{"ubuntu-22.04"}})
	if !isErr || stringArg(m, "reason") != "USAGE" || len(cloud.cards) != 0 || !strings.Contains(text, fmt.Sprint(cardReads+3)) || !strings.Contains(stringArg(m, "hint"), "zone") {
		t.Errorf("%d VMs: want USAGE naming the number and the filters, with no card read (%d read): %s", cardReads+3, len(cloud.cards), text)
	}
	// Narrowed below the limit, the same project answers.
	cloud = &imageCloud{many: cardReads}
	if m, isErr, text := run(cloud, map[string]any{"images": []any{"astra-1.8"}, "zone": "ru.AZ-2"}); isErr || names(m) != "db-1=astra-1.8" || len(cloud.cards) != 1 {
		t.Errorf("narrowed by the zone: %s (%d cards)", text, len(cloud.cards))
	}

	// An empty list and an empty value are USAGE, as of every list filter.
	for _, v := range []any{[]any{}, []any{""}, "ubuntu"} {
		if m, isErr, text := run(&imageCloud{}, map[string]any{"images": v}); !isErr || stringArg(m, "reason") != "USAGE" {
			t.Errorf("images %v: want USAGE, got %s", v, text)
		}
	}
}

// vm_get names the image of the VM: what vm_list selects by is what the
// card shows («what you see is what you filter by»).
func TestVMGet_NamesTheImage(t *testing.T) {
	r, _ := targetRegistry(t, &imageCloud{})
	tl, _ := r.Lookup("vm_get")
	text, isErr := tl.Handler(context.Background(), map[string]any{"vm_id": "00000000-0000-4000-8000-000000000003"})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	img, _ := m["image"].(map[string]any)
	if isErr || img["id"] != imgAstra || img["name"] != "astra-1.8" {
		t.Errorf("vm_get does not name the image: %s", text)
	}
}

// slowCards is imageCloud whose cards after the first come only when the
// call gives up on them: a cloud that answers each card in more time than
// the call has left (cloud-e2e 216: 50 cards ≈ 65 s, 2 of 2).
type slowCards struct{ imageCloud }

func (c *slowCards) GetVMDetail(ctx context.Context, id string) (*provision.VMDetail, error) {
	if len(c.cards) > 0 {
		c.cards = append(c.cards, id)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.imageCloud.GetVMDetail(ctx, id)
}

// The cards of a filter are read within the budget of an MCP call (D69):
// a call that would read past it answers «narrow» with how many it read,
// in time to reach the agent, not cut by the client at its own limit
// (release plan 1.9, «Фильтр по карточкам у vm_list укладывается в предел
// вызова клиента»). Nothing is selected in part («Риск», conclusion 149).
func TestVMList_CardsWithinTheCallBudget(t *testing.T) {
	cloud := &slowCards{}
	r, _ := targetRegistry(t, cloud)
	budgeted(r)
	tl, _ := r.Lookup("vm_list")
	got := make(chan string, 1)
	go func() {
		text, _ := tl.Handler(context.Background(), map[string]any{"images": []any{"ubuntu-22.04"}})
		got <- text
	}()
	var text string
	select {
	case text = <-got:
	// wall-clock: a margin over worldBudget, not a wait — a call that
	// answers is not waited out (budgetWatch).
	case <-time.After(budgetWatch):
		t.Fatal("vm_list read the cards past the budget of the call")
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	msg, hint := stringArg(m, "error"), stringArg(m, "hint")
	if m["vms"] != nil || !strings.Contains(msg, "ran out of its time") || !strings.Contains(msg, "(1 of 3 read)") ||
		!strings.Contains(msg, "nothing was selected") || !strings.Contains(hint, "narrow the list with: zone") {
		t.Errorf("want «ran out of its time», the count read and «narrow», nothing selected: %s", text)
	}
	// The cloud answered: the selection is too wide for one call, as a list
	// over cardReads is — USAGE, not API_UNREACHABLE, which an agent reads
	// as «Cloud.ru is down» and retries («Риск», conclusion 225).
	if r := stringArg(m, "reason"); r != clierrors.CodeUsage {
		t.Errorf("reason %s, want %s: %s", r, clierrors.CodeUsage, text)
	}
	if len(cloud.cards) != 2 {
		t.Errorf("%d cards asked for, want 2: none after the budget ran out", len(cloud.cards))
	}
}
