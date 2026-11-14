// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// consoleFields are the fields of a VM — its card, an item of the list of
// VMs, the answer of PUT, rebuild and create — that hold its console: access
// to the VM. No type evo reads a VM into keeps them
// (TestConsoleFieldsAreNotRead), and hideConsole drops their values from
// every body evo does not parse (D110; «Риск», conclusions 204, 229, 240).
// A new field with the same meaning goes here.
var consoleFields = []string{"vnc_url", "vnc_ws", "remote_console_url", "remote_console_ws", "remote_console_protocol"}

// consoleKey finds a console field as a key, in any case, plain or inside
// JSON escaped into a string any number of times (`\"vnc_url\": …`):
// group 1 holds the backslashes before its quote. Around the colon JSON
// whitespace may stand as is or escaped (\n, \r, \t; Астра #1946).
var consoleKey = regexp.MustCompile(`(?i)(\\*)"(?:` + strings.Join(consoleFields, "|") + `)\\*"` + jsonSpace + `:` + jsonSpace)

// jsonSpace is JSON whitespace, as is or escaped at any depth.
const jsonSpace = `(?:\s|\\+[nrt])*`

// consolePlain finds a console field outside JSON — a name unquoted or in
// single quotes followed by = or : («vnc_url=wss://…», a proxy's or a
// log's text, {'vnc_url': …} of a Python dict) — with
// its value: quoted to its closing quote, else up to a space or a quote.
// A quoted JSON key is followed by its quote, not by = or :, and is
// consoleKey's.
var consolePlain = regexp.MustCompile(`(?i)\b((?:` + strings.Join(consoleFields, "|") + `)'?\s*[=:]\s*)(?:"[^"]*"?|'[^']*'?|[^\s"'<>]*)`)

// hiddenValue replaces the value of a console field.
const hiddenValue = "hidden"

// hideConsole returns body with the value of every console field replaced,
// as JSON or as text outside it (consolePlain): it is for bodies evo puts into an error unparsed — a refusal of
// the platform, an answer of an unknown shape — which may be cut at
// maxErrorBody inside a value or not be JSON at all. A value cut short is
// hidden up to the end of the body.
//
// Fail-closed (release plan 1.9; Астра #1945, #1946): a body that still
// names a console field with a value evo did not hide — an escape deeper
// than escapeDepth reads, the name in \u escapes or in free text — is
// withheld whole (consoleBodyWithheld).
func hideConsole(body []byte) string {
	s := hideConsoleValues(string(body))
	if consoleLeft(s) {
		return consoleBodyWithheld()
	}
	return s
}

// consoleBodyWithheld is what an error carries instead of a body that names
// a console field evo could not hide.
func consoleBodyWithheld() string {
	return i18n.T("(the answer names a console field of the VM; evo withholds it whole)",
		"(ответ называет поле консоли ВМ; evo не показывает его целиком)")
}

// consoleLeft reports whether s names a console field — in any case, with
// its \u escapes read — whose value is not hiddenValue: what lies after the
// name, past quotes, backslashes, JSON whitespace (escaped or not) and the
// separator, does not start with it.
func consoleLeft(s string) bool {
	l := strings.ToLower(unescapeU(s))
	for _, f := range consoleFields {
		for i := 0; ; {
			j := strings.Index(l[i:], f)
			if j < 0 {
				break
			}
			k := i + j + len(f)
			for k < len(l) {
				switch c := l[k]; {
				case c == '\\' && k+1 < len(l) && strings.IndexByte("nrt", l[k+1]) >= 0:
					k += 2
					continue
				case strings.IndexByte(`"'\\ :=`+"\t\r\n", c) >= 0:
					k++
					continue
				}
				break
			}
			if !strings.HasPrefix(l[k:], hiddenValue) {
				return true
			}
			i = k
		}
	}
	return false
}

// unescapeU reads every \uXXXX escape of s, at any depth, as its rune.
func unescapeU(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+5 < len(s) && s[i+1] == 'u' {
			if r, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
				b.WriteRune(rune(r))
				i += 5
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// hideConsoleValues replaces the value of every console field it reads.
func hideConsoleValues(s string) string {
	s = consolePlain.ReplaceAllString(s, "${1}"+hiddenValue)
	locs := consoleKey.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, l := range locs {
		if l[0] < last {
			continue // inside a value already hidden
		}
		depth, ok := escapeDepth(l[3] - l[2])
		if !ok {
			continue
		}
		b.WriteString(s[last:l[1]])
		end := valueEnd(s, l[1], depth)
		q := strings.Repeat(`\`, 1<<depth-1) + `"`
		b.WriteString(q + hiddenValue + q)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// escapeDepth is how many times JSON was escaped into a string when a quote
// carries n backslashes: 0 → 0, 1 → 1, 3 → 2, 7 → 3.
func escapeDepth(n int) (int, bool) {
	for d := 0; d < 8; d++ {
		if n == 1<<d-1 {
			return d, true
		}
	}
	return 0, false
}

// valueEnd returns the end of the JSON value that starts at i in s, escaped
// depth times, or len(s) when the body ends inside it.
func valueEnd(s string, i, depth int) int {
	quote := 1<<depth - 1 // backslashes before a quote of this depth
	if open := i + quote; open < len(s) && s[open] == '"' && strings.Count(s[i:open], `\`) == quote {
		if end := stringEnd(s, open, depth); end > 0 {
			return end
		}
		return len(s)
	}
	// Anything else — a number, null, an object — ends at a comma or a
	// closing bracket outside the brackets it opens and outside its
	// strings («Риск», conclusion 244, residual 4).
	level := 0
	for j := i; j < len(s); j++ {
		if s[j] == '"' && isQuote(s, i, j, depth) {
			end := stringEnd(s, j, depth)
			if end < 0 {
				return len(s)
			}
			j = end - 1
			continue
		}
		switch s[j] {
		case '{', '[':
			level++
		case '}', ']':
			if level == 0 {
				return trimEscapes(s, i, j)
			}
			level--
		case ',':
			if level == 0 {
				return trimEscapes(s, i, j)
			}
		}
	}
	return len(s)
}

// stringEnd returns the end of the string whose opening quote of depth is at
// open: past the next quote of its own depth (one escaped deeper is inside
// it), or -1 when the body ends inside it.
func stringEnd(s string, open, depth int) int {
	for j := open + 1; j < len(s); j++ {
		if s[j] == '"' && isQuote(s, open+1, j, depth) {
			return j + 1
		}
	}
	return -1
}

// isQuote reports whether the quote at j, with no text before from, is a
// quote of depth: it has 2^depth-1 backslashes after the escaped
// backslashes of the text (2<<depth each).
func isQuote(s string, from, j, depth int) bool {
	k := j
	for k > from && s[k-1] == '\\' {
		k--
	}
	return (j-k)%(2<<depth) == 1<<depth-1
}

// trimEscapes moves the end j of a value back over trailing backslashes and
// spaces, which belong to the text around it.
func trimEscapes(s string, i, j int) int {
	for j > i && (s[j-1] == ' ' || s[j-1] == '\\' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return j
}
