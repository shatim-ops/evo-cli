// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package receipt reads the answer of a create the cloud took (2xx): the
// receipt is a JSON object that names what was made — the field the
// specification marks required and scripts/api-contract.json lists in the
// request's reads. A 2xx without it is no success: the outcome is unknown.
package receipt

import (
	"encoding/json"
	stderrors "errors"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// ErrUnusable marks a 2xx whose body is no receipt: an empty or cut body,
// not an object, or an object without the field that names what was made
// (Астра, the full audit of 14.11, findings 06 and 07).
var ErrUnusable = stderrors.New("no receipt")

// Decode reads data, the body of a create's 2xx, as a receipt named by
// field (dotted: "version.id"). Without one it answers API_ERROR wrapping
// ErrUnusable, which leaves the record of the call open
// (intent.OutcomeUnknown): the cloud may have made it.
func Decode(data []byte, field string) (map[string]any, error) {
	var out map[string]any
	if json.Unmarshal(data, &out) != nil || out == nil || lookup(out, field) == "" {
		return nil, clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("the API took the create (2xx) but its answer names no %s: whether it was made is not known",
				"API приняло создание (2xx), но его ответ не называет %s: создано ли, неизвестно", field),
			"", ErrUnusable)
	}
	return out, nil
}

// lookup is the string at the dotted path of m, "" when there is none.
func lookup(m map[string]any, path string) string {
	keys := strings.Split(path, ".")
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			return ""
		}
		m = next
	}
	s, _ := m[keys[len(keys)-1]].(string)
	return s
}
