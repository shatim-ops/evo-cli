// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import "testing"

// Release plan 1.8, L3: «another key» only when it is sure — by the account
// when both know it (a new key of the same account sees the same), else by
// the Key ID; unknown on either side is not «another».
func TestMaker_Other(t *testing.T) {
	for _, c := range []struct {
		name     string
		rec, cur *Maker
		other    bool
	}{
		{"a record from before 1.8", nil, &Maker{KeyID: "a"}, false},
		{"no key now", &Maker{KeyID: "a"}, nil, false},
		{"the same account, a new key", &Maker{ServiceAccountID: "sa", KeyID: "a"}, &Maker{ServiceAccountID: "sa", KeyID: "b"}, false},
		{"another account", &Maker{ServiceAccountID: "sa1", KeyID: "a"}, &Maker{ServiceAccountID: "sa2", KeyID: "a"}, true},
		{"no account, the same key", &Maker{KeyID: "a"}, &Maker{ServiceAccountID: "sa", KeyID: "a"}, false},
		{"the record knows the account, the key does not: by the Key ID", &Maker{ServiceAccountID: "sa", KeyID: "a"}, &Maker{KeyID: "b"}, true},
		{"no account, another key", &Maker{KeyID: "a"}, &Maker{KeyID: "b"}, true},
		{"nothing known", &Maker{}, &Maker{KeyID: "b"}, false},
	} {
		if got := c.rec.Other(c.cur); got != c.other {
			t.Errorf("%s: Other %v, want %v", c.name, got, c.other)
		}
	}
}
