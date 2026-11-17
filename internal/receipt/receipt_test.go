// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package receipt

import (
	stderrors "errors"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// A receipt is an object that names what was made; anything else is
// ErrUnusable with API_ERROR (the outcome unknown).
func TestDecode(t *testing.T) {
	for _, c := range []struct {
		body, field string
		ok          bool
	}{
		{`{"name":"app","owner":"app"}`, "name", true},
		{`{"version":{"id":"v-1"}}`, "version.id", true},
		{`{}`, "name", false},
		{``, "name", false},
		{`{"name":`, "name", false},
		{`null`, "name", false},
		{`[]`, "name", false},
		{`{"name":""}`, "name", false},
		{`{"name":" "}`, "name", false},
		{`{"version":{"id":"\t "}}`, "version.id", false},
		{`{"name":7}`, "name", false},
		{`{"version":{}}`, "version.id", false},
		{`{"version":"v-1"}`, "version.id", false},
	} {
		out, err := Decode([]byte(c.body), c.field)
		if c.ok != (err == nil) || c.ok != (out != nil) {
			t.Errorf("Decode(%q, %s): %v, %v; want ok %v", c.body, c.field, out, err, c.ok)
		}
		if err != nil && (!stderrors.Is(err, ErrUnusable) || clierrors.Reason(err) != clierrors.CodeAPIError) {
			t.Errorf("Decode(%q, %s): %v; want API_ERROR wrapping ErrUnusable", c.body, c.field, err)
		}
	}
}
