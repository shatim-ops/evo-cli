// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// B-6 (R-49): the catalog of actions is read only after a false of
// evaluate, and at most once a login — whatever the number of falses
// (actions × projects) and whether the catalog answered or failed.
func TestLogin_CatalogReadAtMostOnce(t *testing.T) {
	for _, c := range []struct {
		name string
		v    loginworld.Variant
		want int // 0: never, 1: once
	}{
		{"allowed", loginworld.Variant{}, 0},
		{"no right", loginworld.Variant{Denied: true}, 1},
		{"unknown action", loginworld.Variant{UnknownActionFalse: true}, 1},
		{"unknown action, catalog fails", loginworld.Variant{UnknownActionFalse: true, CatalogFails: true}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, c.v)
			login(t, w, &browser{}, Choice{}, nil, nil)
			n := 0
			for _, call := range w.Calls() {
				if call == "GET /api/v1/actions" {
					n++
				}
			}
			if n != c.want {
				t.Errorf("the catalog read %d times, want %d: %v", n, c.want, w.Calls())
			}
		})
	}
}
