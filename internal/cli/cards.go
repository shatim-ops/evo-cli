// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// A card is an answer shown as aligned "label: value" lines (image get,
// the billing reports).

// cardLine is a line of a card: its label and value.
type cardLine struct{ k, v string }

// printCard prints the lines as "label: value" with the values aligned;
// a line with no value is left out.
func printCard(w io.Writer, lines []cardLine) {
	width := 0
	for _, l := range lines {
		width = max(width, utf8.RuneCountInString(l.k))
	}
	for _, l := range lines {
		if l.v == "" {
			continue
		}
		fmt.Fprintf(w, "%s:%s %s\n", l.k, strings.Repeat(" ", width-utf8.RuneCountInString(l.k)), output.SanitizeCell(l.v))
	}
}
