// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// notInCatalogue is the refusal of a value the catalogue answered a list of
// without it (D54, #543, option A; the class of D50): the call would fail
// after the question, so it is NOT_FOUND before it, naming what there is
// and the read tool that lists it. Only a list that was read and names
// something decides: no value, no list, an empty one — nil, the call goes
// as before. A leading "v" is not a difference ("1.34.6" is "v1.34.6").
func notInCatalogue(what, value string, known []string, listTool string) error {
	if value == "" || len(known) == 0 {
		return nil
	}
	bare := strings.TrimPrefix(value, "v")
	for _, k := range known {
		if strings.TrimPrefix(k, "v") == bare {
			return nil
		}
	}
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("%s %q is not in the catalogue. Available: %s", "%s %q нет в каталоге. Есть: %s", what, value, strings.Join(known, ", ")),
		i18n.Tf("Pick one of %s.", "Выберите из %s.", listTool))
}

// listed are the non-empty string values of field in items: what a list
// names, for notInCatalogue.
func listed(items []map[string]any, field string) []string {
	var out []string
	for _, it := range items {
		if v, _ := it[field].(string); v != "" {
			out = append(out, v)
		}
	}
	return out
}
