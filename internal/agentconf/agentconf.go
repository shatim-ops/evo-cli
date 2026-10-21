// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package agentconf edits the files 'evo agent setup' writes for an AI
// agent: the configuration of its MCP client (JSON or TOML) and its rules
// (SKILL.md, a section of AGENTS.md). The files belong to the user and to
// the client, so nothing is rewritten wholesale: one server entry, one
// table or one marked section is added or replaced, and every other byte of
// the file stays as it was — keys, order, indentation, comments. The caller
// decides what a replacement needs (--force) and writes the result.
package agentconf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Field is one key of an entry, in the order the file shows them. Value is
// a string, a []string or an Entry (a nested object, an inline table).
type Field struct {
	Key   string
	Value any
}

// Entry is a server entry: an object with its keys in order.
type Entry []Field

// MarshalJSON writes the entry as a JSON object in field order.
func (e Entry) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range e {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshalJSON(f.Key, "", "")
		if err != nil {
			return nil, err
		}
		v, err := marshalJSON(f.Value, "", "")
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// plain is the entry as encoding/json decodes it into an any: the form both
// formats compare an entry already in a file with.
func (e Entry) plain() map[string]any {
	m := make(map[string]any, len(e))
	for _, f := range e {
		m[f.Key] = plainValue(f.Value)
	}
	return m
}

func plainValue(v any) any {
	switch v := v.(type) {
	case Entry:
		return v.plain()
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	}
	return v
}

// State says what the file had at the place of the entry.
type State int

const (
	// Added: no entry there; Result.Data adds it.
	Added State = iota
	// Same: the same entry is there; Result.Data is the file unchanged.
	Same
	// Replaced: another entry is there; Result.Data replaces it. The caller
	// asks for that explicitly (--force).
	Replaced
)

// Result is the file with the entry in place.
type Result struct {
	Data  []byte
	State State
}

// FileError refuses a file that cannot be edited safely: broken syntax, or
// a shape this package does not edit. Line and Col (1-based) say where;
// Line is 0 when there is no place to name. The file is left alone.
type FileError struct {
	Line, Col int
	Msg       string
}

func (e *FileError) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return i18n.Tf("line %d, column %d: %s", "строка %d, столбец %d: %s", e.Line, e.Col, e.Msg)
}

// errorAt is a FileError at the byte offset off of data.
func errorAt(data []byte, off int, msg string) *FileError {
	off = min(max(off, 0), len(data))
	line, start := 1, 0
	for i := 0; i < off; i++ {
		if data[i] == '\n' {
			line, start = line+1, i+1
		}
	}
	return &FileError{Line: line, Col: utf8.RuneCount(data[start:off]) + 1, Msg: msg}
}

// newline is the line ending of data: "\r\n" when it has one, else "\n".
func newline(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// splice replaces data[from:to] with text, in a new slice.
func splice(data []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(data)-(to-from)+len(text))
	out = append(out, data[:from]...)
	out = append(out, text...)
	return append(out, data[to:]...)
}

// marshalJSON encodes v without HTML escaping ("<" stays "<"), indented
// with prefix and indent when indent is set, without the final newline.
func marshalJSON(v any, prefix, indent string) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent(prefix, indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// normalizeNewlines turns "\r\n" into "\n", to compare texts whatever the
// line endings of the file.
func normalizeNewlines(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}
