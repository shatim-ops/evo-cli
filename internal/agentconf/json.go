// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package agentconf

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"reflect"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// jsonIndent is the indentation of a JSON file evo creates, and of one whose
// own indentation cannot be told (its root object is empty).
const jsonIndent = "  "

// JSONDocument is a new JSON file holding only the entry, at servers.name:
// what --print shows and what an absent or empty file becomes.
func JSONDocument(servers, name string, e Entry) []byte {
	b, _ := marshalJSON(Entry{{servers, Entry{{name, e}}}}, "", jsonIndent)
	return append(b, '\n')
}

// JSON puts the entry at servers.name of the JSON object old (e.g.
// mcpServers.evo). An absent servers object is added at the end of the root
// object, an absent entry at the end of the servers object, an entry already
// there is replaced in place; the rest of the file keeps its bytes, and what
// is added follows the file's indentation and line endings. Only the keys
// evo writes (owned, see ownedSet) are compared and replaced: the entry
// becomes evo's keys, then the user's keys it had. With comments the file
// may have // and /* */ comments and trailing commas (JSONC, as VS Code
// reads mcp.json); they are kept too, but for those inside a replaced entry.
func JSON(old []byte, servers, name string, e Entry, owned []string, comments bool) (Result, error) {
	if len(bytes.TrimSpace(old)) == 0 {
		return Result{Data: JSONDocument(servers, name, e), State: Added}, nil
	}
	src := old
	if comments {
		var err error
		if src, err = stripComments(old); err != nil {
			return Result{}, err
		}
	}
	if err := checkJSONObject(src); err != nil {
		return Result{}, err
	}
	d := &jsonDoc{old: old, src: src, nl: newline(old)}
	root := d.object(d.space(0))
	d.layout(root)

	sm, err := d.only(root, servers)
	if err != nil || sm == nil {
		if err != nil {
			return Result{}, err
		}
		return Result{Data: d.insert(root, servers, Entry{{name, e}}), State: Added}, nil
	}
	if src[sm.valStart] != '{' {
		return Result{}, errorAt(old, sm.valStart, i18n.Tf("%q is not an object", "%q — не объект", servers))
	}
	so := d.object(sm.valStart)
	em, err := d.only(so, name)
	switch {
	case err != nil:
		return Result{}, err
	case em == nil:
		return Result{Data: d.insert(so, name, e), State: Added}, nil
	}
	own := ownedSet(e, owned)
	keep, err := d.usersKeys(em, own)
	switch {
	case err != nil:
		return Result{}, err
	case sameJSON(src[em.valStart:em.valEnd], e, own):
		return Result{Data: old, State: Same}, nil
	}
	return Result{Data: d.replace(em, append(slices.Clone(e), keep...)), State: Replaced}, nil
}

// usersKeys are the members of the entry m that are not evo's, with their
// values as they are (comments blanked out); none when the entry is not an
// object. A key there twice is refused.
func (d *jsonDoc) usersKeys(m *jsonMember, own map[string]bool) (Entry, error) {
	if d.src[m.valStart] != '{' {
		return nil, nil
	}
	var keep Entry
	o := d.object(m.valStart)
	for _, mm := range o.members {
		if _, err := d.only(o, mm.key); err != nil {
			return nil, err
		}
		if !own[mm.key] {
			keep = append(keep, Field{Key: mm.key, Value: json.RawMessage(d.src[mm.valStart:mm.valEnd])})
		}
	}
	return keep, nil
}

// checkJSONObject refuses src that is not one valid JSON object, naming the
// place encoding/json stopped at.
func checkJSONObject(src []byte) error {
	var v any
	if err := json.Unmarshal(src, &v); err != nil {
		var syn *json.SyntaxError
		if stderrors.As(err, &syn) {
			return errorAt(src, int(syn.Offset)-1, i18n.Tf("invalid JSON: %v", "неверный JSON: %v", syn))
		}
		return &FileError{Msg: i18n.Tf("invalid JSON: %v", "неверный JSON: %v", err)}
	}
	if _, ok := v.(map[string]any); !ok {
		return errorAt(src, len(src)-len(bytes.TrimLeft(src, " \t\r\n")), i18n.T("the file is not a JSON object", "файл — не объект JSON"))
	}
	return nil
}

// sameJSON reports whether evo's keys (own) of the JSON value raw are the
// entry, by value: key order, layout and the user's keys do not count.
func sameJSON(raw []byte, e Entry, own map[string]bool) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && reflect.DeepEqual(ownedPart(v, own), any(e.plain()))
}

// jsonDoc is a JSON file being edited: old as read, src the same bytes with
// comments blanked out (equal offsets), which are known to be valid JSON.
type jsonDoc struct {
	old, src []byte
	nl       string
	pretty   bool   // members start lines (else the file is on one line)
	unit     string // one level of indentation
}

type jsonMember struct {
	key                        string
	keyStart, valStart, valEnd int
}

type jsonObject struct {
	open, close int
	members     []jsonMember
}

// object reads the members of the object whose "{" is at open.
func (d *jsonDoc) object(open int) jsonObject {
	o := jsonObject{open: open}
	i := d.space(open + 1)
	for d.src[i] != '}' {
		ks := i
		ke := d.skipString(ks)
		var key string
		_ = json.Unmarshal(d.src[ks:ke], &key)
		vs := d.space(d.space(ke) + 1) // past ":"
		ve := d.skipValue(vs)
		o.members = append(o.members, jsonMember{key: key, keyStart: ks, valStart: vs, valEnd: ve})
		i = d.space(ve)
		if d.src[i] == ',' {
			i = d.space(i + 1)
		}
	}
	o.close = i
	return o
}

// only returns the member named key, nil when there is none; a key that is
// there twice is refused: parsers differ on which one counts.
func (d *jsonDoc) only(o jsonObject, key string) (*jsonMember, error) {
	var found *jsonMember
	for i := range o.members {
		if o.members[i].key != key {
			continue
		}
		if found != nil {
			return nil, errorAt(d.old, o.members[i].keyStart, i18n.Tf("key %q appears twice", "ключ %q встречается дважды", key))
		}
		found = &o.members[i]
	}
	return found, nil
}

// layout learns the file's style from its root object: whether members
// start lines and by how much the first one is indented.
func (d *jsonDoc) layout(root jsonObject) {
	d.pretty, d.unit = true, jsonIndent
	if len(root.members) == 0 {
		return
	}
	ind, ok := d.lineIndent(root.members[0].keyStart)
	d.pretty = ok
	if ok && ind != "" {
		d.unit = ind
	}
}

// insert adds the member key: value at the end of the object o.
func (d *jsonDoc) insert(o jsonObject, key string, value any) []byte {
	if n := len(o.members); n > 0 {
		last := o.members[n-1]
		if ind, ok := d.lineIndent(last.keyStart); ok {
			return splice(d.old, last.valEnd, last.valEnd, ","+d.nl+ind+d.member(key, value, ind, true))
		}
		return splice(d.old, last.valEnd, last.valEnd, ","+d.member(key, value, "", false))
	}
	if !d.pretty {
		return splice(d.old, o.open+1, o.open+1, d.member(key, value, "", false))
	}
	lead := d.lineLead(o.open)
	ind := lead + d.unit
	text := d.nl + ind + d.member(key, value, ind, true)
	if !bytes.Contains(d.old[o.open+1:o.close], []byte("\n")) {
		text += d.nl + lead // "{}" becomes "{\n  …\n}"
	}
	return splice(d.old, o.open+1, o.open+1, text)
}

// replace puts the entry in place of the value of the member m.
func (d *jsonDoc) replace(m *jsonMember, e Entry) []byte {
	ind, ok := d.lineIndent(m.keyStart)
	return splice(d.old, m.valStart, m.valEnd, d.value(e, ind, ok))
}

// member is `"key": value`, the value laid out at the indentation ind, or
// on one line where the object around it is.
func (d *jsonDoc) member(key string, value any, ind string, pretty bool) string {
	k, _ := marshalJSON(key, "", "")
	sep := ": "
	if !pretty {
		sep = ":"
	}
	return string(k) + sep + d.value(value, ind, pretty)
}

func (d *jsonDoc) value(v any, ind string, pretty bool) string {
	if !pretty {
		b, _ := marshalJSON(v, "", "")
		return string(b)
	}
	b, _ := marshalJSON(v, ind, d.unit)
	return strings.ReplaceAll(string(b), "\n", d.nl)
}

// lineIndent is the whitespace before pos on its line, and whether only
// whitespace is there (pos starts the line's content).
func (d *jsonDoc) lineIndent(pos int) (string, bool) {
	j := pos
	for j > 0 && (d.old[j-1] == ' ' || d.old[j-1] == '\t') {
		j--
	}
	if j > 0 && d.old[j-1] != '\n' {
		return "", false
	}
	return string(d.old[j:pos]), true
}

// lineLead is the leading whitespace of the line holding pos.
func (d *jsonDoc) lineLead(pos int) string {
	start := bytes.LastIndexByte(d.old[:pos], '\n') + 1
	end := start
	for end < pos && (d.old[end] == ' ' || d.old[end] == '\t') {
		end++
	}
	return string(d.old[start:end])
}

func (d *jsonDoc) space(i int) int {
	for i < len(d.src) && (d.src[i] == ' ' || d.src[i] == '\t' || d.src[i] == '\r' || d.src[i] == '\n') {
		i++
	}
	return i
}

// skipString returns the offset after the string starting at i.
func (d *jsonDoc) skipString(i int) int {
	for j := i + 1; j < len(d.src); j++ {
		switch d.src[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return len(d.src)
}

// skipValue returns the offset after the value starting at i.
func (d *jsonDoc) skipValue(i int) int {
	switch d.src[i] {
	case '"':
		return d.skipString(i)
	case '{', '[':
		depth := 0
		for j := i; j < len(d.src); j++ {
			switch d.src[j] {
			case '"':
				j = d.skipString(j) - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return j + 1
				}
			}
		}
		return len(d.src)
	}
	j := i
	for j < len(d.src) && !strings.ContainsRune(",}] \t\r\n", rune(d.src[j])) {
		j++
	}
	return j
}

// stripComments blanks out the comments and trailing commas of a JSONC
// file with spaces, keeping line breaks, so offsets and positions in the
// result are those of data.
func stripComments(data []byte) ([]byte, error) {
	src := bytes.Clone(data)
	blank := func(from, to int) {
		for k := from; k < to; k++ {
			if src[k] != '\n' && src[k] != '\r' {
				src[k] = ' '
			}
		}
	}
	var commas []int
	for i := 0; i < len(src); i++ {
		switch {
		case src[i] == '"':
			for i++; i < len(src) && src[i] != '"'; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case src[i] == ',':
			commas = append(commas, i)
		case bytes.HasPrefix(src[i:], []byte("//")):
			end := bytes.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			blank(i, i+end)
			i += end - 1
		case bytes.HasPrefix(src[i:], []byte("/*")):
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return nil, errorAt(data, i, i18n.T("unterminated /* comment", "комментарий /* не закрыт"))
			}
			blank(i, i+2+end+2)
			i += 2 + end + 1
		}
	}
	for _, c := range commas {
		next := c + 1
		for next < len(src) && strings.ContainsRune(" \t\r\n", rune(src[next])) {
			next++
		}
		if next < len(src) && (src[next] == '}' || src[next] == ']') {
			src[c] = ' '
		}
	}
	return src, nil
}
