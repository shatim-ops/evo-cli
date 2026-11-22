// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"testing"
)

// The person at the terminal sees what the agent sees of the project a
// change addressed by id lands in (release plan 1.9, «Превью называет
// проект записи»): the project of the resource with its origin; the note
// beside a project that is not the call's; and, where no project could be
// read, the note in the project's line — never an empty line that reads as
// "the project of the profile".
func TestShowCreatePreview_ProjectOfTheResource(t *testing.T) {
	const project = "00000000-0000-4000-8000-0000000000bb"
	other := "disk x is in another project, not the call's; the change goes there"
	unknown := "not determined: the call's project does not hold disk x, and its API names no project"
	for _, c := range []struct {
		name string
		p    CreatePreview
		want []string
	}{
		{"the resource's project", CreatePreview{Resource: "disk_resize", ProjectID: project, ProjectFrom: OriginResource},
			[]string{project + " (the project of the resource)"}},
		{"another project", CreatePreview{Resource: "disk_resize", ProjectID: project, ProjectFrom: OriginResource, ProjectNote: other},
			[]string{project + " (the project of the resource)", "warning:", other}},
		{"not determined", CreatePreview{Resource: "disk_resize", ProjectNote: unknown},
			[]string{"project:", unknown}},
	} {
		lines := strings.Join(ShowCreatePreview("resize", c.p).Lines, "\n")
		for _, w := range c.want {
			if !strings.Contains(lines, w) {
				t.Errorf("%s: the human preview lacks %q:\n%s", c.name, w, lines)
			}
		}
	}
}

func TestShowCreatePreview_QuotesNetworkValues(t *testing.T) {
	const name = "name\nsecond"
	const id = "sub-\"one"
	cases := []CreatePreview{
		{SubnetID: id},
		{SubnetID: id, SubnetName: name},
		{SubnetID: id, SecurityGroup: &NetworkRef{ID: name}},
		{SubnetName: name, SecurityGroup: &NetworkRef{Name: id}},
	}
	for _, p := range cases {
		p.Resource = ResourceVM
		p.VM = &deployplan.VMSpec{}
		lines := strings.Join(ShowCreatePreview("create", p).Lines, "\n")
		if !strings.Contains(lines, strconv.Quote(id)) || strings.Contains(lines, name) {
			t.Errorf("unquoted network in preview: %q", lines)
		}
		if (p.SubnetName != "" || p.SecurityGroup != nil) && !strings.Contains(lines, strconv.Quote(name)) {
			t.Errorf("missing subnet or security group: %q", lines)
		}
	}
}
