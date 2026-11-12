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
	// whose card cannot be read.
	noImage, failing string
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
