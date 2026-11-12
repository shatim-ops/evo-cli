// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
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
