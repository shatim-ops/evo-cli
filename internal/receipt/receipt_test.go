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
		body  string
		field Field
		ok    bool
	}{
		{`{"name":"app","owner":"app"}`, Text("name"), true},
		// v2AddVersionResponse of Secret Manager: version.id is an int32
		// (cloud-e2e of 1.9: the receipt of a real add was refused).
		{`{"secret":{"id":"s-1"},"version":{"id":2,"state":"ENABLED"}}`, Number("version.id"), true},
		{`{"version":{"id":1}}`, Number("version.id"), true},
		{`{}`, Text("name"), false},
		{``, Text("name"), false},
		{`{"name":`, Text("name"), false},
		{`null`, Text("name"), false},
		{`[]`, Text("name"), false},
		{`{"name":""}`, Text("name"), false},
		{`{"name":" "}`, Text("name"), false},
		{`{"name":7}`, Text("name"), false},
		{`{"version":{}}`, Number("version.id"), false},
		{`{"version":"v-1"}`, Number("version.id"), false},
		{`{"version":2}`, Number("version.id"), false},
		{`{"version":{"id":0}}`, Number("version.id"), false},
		{`{"version":{"id":-1}}`, Number("version.id"), false},
		{`{"version":{"id":1.5}}`, Number("version.id"), false},
		{`{"version":{"id":null}}`, Number("version.id"), false},
		{`{"version":{"id":"2"}}`, Number("version.id"), false},
		{`{"version":{"id":{"value":2}}}`, Number("version.id"), false},
	} {
		out, err := Decode([]byte(c.body), c.field)
		if c.ok != (err == nil) || c.ok != (out != nil) {
			t.Errorf("Decode(%q, %+v): %v, %v; want ok %v", c.body, c.field, out, err, c.ok)
		}
		if err != nil && (!stderrors.Is(err, ErrUnusable) || clierrors.Reason(err) != clierrors.CodeAPIError) {
			t.Errorf("Decode(%q, %+v): %v; want API_ERROR wrapping ErrUnusable", c.body, c.field, err)
		}
	}
}
