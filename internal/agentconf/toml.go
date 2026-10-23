// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package agentconf

import (
	"bytes"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The TOML of a client (Codex: ~/.codex/config.toml) is edited without a
// TOML library: the file is scanned for its structure — table headers and
// key/value lines, with every kind of string, array and inline table read in
// full — and only the table of the entry is added or replaced, as whole
// lines. A table defined any other way (dotted keys, an inline table, an
// array of tables) is refused instead of guessed at, and the result is
// scanned again: everything outside the entry must come out as it was.

// TOMLDocument is a new TOML file holding only the entry, as the table
// [table…].
func TOMLDocument(table []string, e Entry) []byte {
	return []byte(tomlBlock(table, e, "\n"))
}

// TOML puts the entry at the table of the TOML document old (e.g.
// [mcp_servers.evo]): an absent table is added after the last table under
// the same parent ([mcp_servers.*]) or at the end of the file. In a table
// already there only the keys evo writes (owned, see ownedSet) are compared
// and replaced: evo's key lines and sub-tables go, the entry's lines follow
// the header, and the user's keys and sub-tables ([mcp_servers.evo.env])
// stay where they are.
func TOML(old []byte, table []string, e Entry, owned []string) (Result, error) {
	if len(bytes.TrimSpace(old)) == 0 {
		return Result{Data: TOMLDocument(table, e), State: Added}, nil
	}
	items, err := scanTOML(old)
	if err != nil {
		return Result{}, err
	}
	parts, err := tomlParts(old, items, table)
	if err != nil {
		return Result{}, err
	}
	nl := newline(old)
	block := tomlBlock(table, e, nl)
	if len(parts) == 0 {
		at := len(old)
		for _, p := range tomlTables(items, table[:len(table)-1]) {
			at = p.end
		}
		text := nl + block
		if old[at-1] != '\n' {
			text = nl + text
		}
		return checkedTOML(old, items, splice(old, at, at, text), table, e, ownedSet(e, owned), Added)
	}
	own := ownedSet(e, owned)
	if reflect.DeepEqual(ownedPart(tomlEntry(items, parts, table), own), e.plain()) {
		return Result{Data: old, State: Same}, nil
	}
	// The edits do not overlap; they go from the end of the file, so the
	// offsets before each hold.
	type edit struct {
		from, to int
		text     string
	}
	var edits []edit
	main := -1
	for i, p := range parts {
		if slices.Equal(items[p.header].path, table) {
			main = i
			continue
		}
		if !own[items[p.header].path[len(table)]] {
			continue
		}
		// A sub-table of evo's key goes with the blank lines before it.
		start, floor := p.start, 0
		if i > 0 {
			floor = parts[i-1].end
		}
		for start > floor && len(bytes.TrimSpace(old[lineStart(old, start-1):start])) == 0 {
			start = lineStart(old, start-1)
		}
		edits = append(edits, edit{start, p.end, ""})
	}
	if main < 0 {
		// Only sub-tables: the table goes before the first one that stays,
		// or after the last one when all go (not inside a removed one).
		at := parts[len(parts)-1].end
		for _, p := range parts {
			if !own[items[p.header].path[len(table)]] {
				at = p.start
				break
			}
		}
		edits = append(edits, edit{at, at, block})
	} else {
		h := parts[main].header
		edits = append(edits, edit{parts[main].start, items[h].end, block})
		for j := h + 1; j < len(items) && items[j].kind == tomlKeyValue; j++ {
			if own[items[j].key[0]] {
				edits = append(edits, edit{items[j].start, items[j].end, ""})
			}
		}
	}
	slices.SortFunc(edits, func(a, b edit) int { return b.from - a.from })
	data := old
	for _, ed := range edits {
		data = splice(data, ed.from, ed.to, ed.text)
	}
	state := Replaced
	if main < 0 && len(edits) == 1 {
		state = Added // the user's sub-tables only: nothing of evo's to replace
	}
	return checkedTOML(old, items, data, table, e, own, state)
}

// tomlBlock is the table of the entry as lines ending with nl.
func tomlBlock(table []string, e Entry, nl string) string {
	var b strings.Builder
	b.WriteString("[" + tomlKeyPath(table) + "]" + nl)
	for _, f := range e {
		b.WriteString(tomlKey(f.Key) + " = " + tomlValue(f.Value) + nl)
	}
	return b.String()
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return tomlString(k)
}

func tomlKeyPath(path []string) string {
	keys := make([]string, len(path))
	for i, k := range path {
		keys[i] = tomlKey(k)
	}
	return strings.Join(keys, ".")
}

// tomlString is a TOML basic string: the escapes of a JSON string are TOML
// escapes too.
func tomlString(s string) string {
	b, _ := marshalJSON(s, "", "")
	return string(b)
}

func tomlValue(v any) string {
	switch v := v.(type) {
	case string:
		return tomlString(v)
	case bool:
		return strconv.FormatBool(v)
	case []string:
		items := make([]string, len(v))
		for i, s := range v {
			items[i] = tomlString(s)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case Entry:
		if len(v) == 0 {
			return "{}"
		}
		items := make([]string, len(v))
		for i, f := range v {
			items[i] = tomlKey(f.Key) + " = " + tomlValue(f.Value)
		}
		return "{ " + strings.Join(items, ", ") + " }"
	}
	panic("agentconf: no TOML form for " + reflect.TypeOf(v).String())
}

type tomlKind int

const (
	tomlTable tomlKind = iota
	tomlArrayTable
	tomlKeyValue
)

// tomlItem is a table header or a key/value of a TOML file.
type tomlItem struct {
	kind tomlKind
	// path: of a header, the table; of a key/value, table and key.
	path []string
	// key: of a key/value, its own (dotted) key.
	key   []string
	value any
	// start is the first byte of the item's line; end the byte after its
	// last line (after the line break, if any).
	start, end int
}

// tomlPart is the lines of one table of the entry: its header and its
// key/values, without the blank lines and comments that follow them.
type tomlPart struct{ header, start, end int }

// tomlTables are the tables under parent ([mcp_servers.*], parent itself
// included) with their lines, in file order.
func tomlTables(items []tomlItem, parent []string) []tomlPart {
	var out []tomlPart
	for i, it := range items {
		if it.kind == tomlKeyValue || !hasPrefix(it.path, parent) {
			continue
		}
		p := tomlPart{header: i, start: it.start, end: it.end}
		for j := i + 1; j < len(items) && items[j].kind == tomlKeyValue; j++ {
			p.end = items[j].end
		}
		out = append(out, p)
	}
	return out
}

// tomlParts are the tables of the entry: [table] and its sub-tables. A
// table defined some other way is refused, since adding or replacing the
// header would then break the file or lose the definition.
func tomlParts(data []byte, items []tomlItem, table []string) ([]tomlPart, error) {
	name := tomlKeyPath(table)
	var current []string // the table of the key/values that follow
	headers := 0
	for _, it := range items {
		switch {
		case it.kind == tomlArrayTable && (hasPrefix(it.path, table) || isProperPrefix(it.path, table)):
			return nil, errorAt(data, it.start, i18n.Tf("[[%s]] is an array of tables: edit the file by hand",
				"[[%s]] — массив таблиц: исправьте файл вручную", tomlKeyPath(it.path)))
		case it.kind == tomlKeyValue && isProperPrefix(it.path, table):
			return nil, errorAt(data, it.start, i18n.Tf("%s is set as a value, so [%s] cannot be added: edit the file by hand",
				"%s задан значением, и [%s] не добавить: исправьте файл вручную", tomlKeyPath(it.path), name))
		case it.kind == tomlKeyValue && hasPrefix(it.path, table) && !hasPrefix(current, table):
			return nil, errorAt(data, it.start, i18n.Tf("%s is set with a dotted key or an inline table: edit the file by hand",
				"%s задан через точку или встроенной таблицей: исправьте файл вручную", name))
		case it.kind != tomlKeyValue:
			current = it.path
			if slices.Equal(it.path, table) {
				if headers++; headers > 1 {
					return nil, errorAt(data, it.start, i18n.Tf("[%s] appears twice", "[%s] встречается дважды", name))
				}
			}
		}
	}
	return tomlTables(items, table), nil
}

// tomlEntry is the entry the parts define, as encoding/json would decode it.
func tomlEntry(items []tomlItem, parts []tomlPart, table []string) map[string]any {
	out := map[string]any{}
	for _, p := range parts {
		at := ensureTable(out, items[p.header].path[len(table):])
		for j := p.header + 1; j < len(items) && items[j].kind == tomlKeyValue; j++ {
			kv := items[j]
			parent := ensureTable(at, kv.key[:len(kv.key)-1])
			parent[kv.key[len(kv.key)-1]] = kv.value
		}
	}
	return out
}

func ensureTable(m map[string]any, path []string) map[string]any {
	for _, k := range path {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	return m
}

// checkedTOML scans the edited file again and refuses the edit unless it
// is valid as far as the scanner sees, every item outside the entry's
// tables reads as before, evo's keys (own) of the entry's tables define
// exactly the entry, and the user's keys there are as they were.
func checkedTOML(old []byte, before []tomlItem, data []byte, table []string, e Entry, own map[string]bool, state State) (Result, error) {
	refuse := &FileError{Msg: i18n.T("the file cannot be edited safely: edit it by hand",
		"файл не удаётся изменить безопасно: исправьте его вручную")}
	after, err := scanTOML(data)
	if err != nil {
		return Result{}, refuse
	}
	parts, err := tomlParts(data, after, table)
	if err != nil {
		return Result{}, refuse
	}
	got := tomlEntry(after, parts, table)
	users := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			if !own[k] {
				out[k] = v
			}
		}
		return out
	}
	var had map[string]any
	if partsBefore, err := tomlParts(old, before, table); err == nil {
		had = tomlEntry(before, partsBefore, table)
	}
	if !reflect.DeepEqual(ownedPart(got, own), e.plain()) || !reflect.DeepEqual(users(got), users(had)) ||
		!slices.EqualFunc(outside(old, before, table), outside(data, after, table), bytes.Equal) {
		return Result{}, refuse
	}
	return Result{Data: data, State: state}, nil
}

// outside is the text of every item not of the entry's tables.
func outside(data []byte, items []tomlItem, table []string) [][]byte {
	var out [][]byte
	var current []string
	for _, it := range items {
		if it.kind != tomlKeyValue {
			current = it.path
		}
		if hasPrefix(current, table) {
			continue
		}
		out = append(out, bytes.TrimRight(data[it.start:it.end], "\r\n"))
	}
	return out
}

// lineStart is the offset of the line holding data[i].
func lineStart(data []byte, i int) int {
	return bytes.LastIndexByte(data[:i], '\n') + 1
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

func isProperPrefix(path, of []string) bool {
	return len(path) < len(of) && hasPrefix(of, path)
}

// tomlScanner reads the structure of a TOML document (TOML 1.0, with the
// newlines in inline tables 1.1 allows). Values are decoded as far as an
// entry needs — strings, arrays, inline tables, booleans; another scalar
// (number, date) is checked for its characters and kept as tomlScalar.
type tomlScanner struct {
	data []byte
	i    int
}

// tomlScalar is a number or a date, as written.
type tomlScalar string

func scanTOML(data []byte) ([]tomlItem, error) {
	s := &tomlScanner{data: data}
	var items []tomlItem
	var table []string
	for s.i < len(s.data) {
		start := s.i
		s.blank()
		switch {
		case s.i == len(s.data):
		case s.lineEnd():
			if err := s.eol(); err != nil {
				return nil, err
			}
		case s.data[s.i] == '[':
			kind, closing := tomlTable, "]"
			if s.at("[[") {
				kind, closing = tomlArrayTable, "]]"
			}
			s.i += len(closing)
			path, err := s.key()
			if err != nil {
				return nil, err
			}
			s.blank()
			if !s.at(closing) {
				return nil, s.errorf(i18n.Tf("expected %q", "ожидалось %q", closing))
			}
			s.i += len(closing)
			if err := s.eol(); err != nil {
				return nil, err
			}
			table = path
			items = append(items, tomlItem{kind: kind, path: path, start: start, end: s.i})
		default:
			key, err := s.key()
			if err != nil {
				return nil, err
			}
			s.blank()
			if !s.at("=") {
				return nil, s.errorf(i18n.T(`expected "=" after the key`, `после ключа ожидалось "="`))
			}
			s.i++
			s.blank()
			v, err := s.value()
			if err != nil {
				return nil, err
			}
			if err := s.eol(); err != nil {
				return nil, err
			}
			path := append(slices.Clone(table), key...)
			items = append(items, tomlItem{kind: tomlKeyValue, path: path, key: key, value: v, start: start, end: s.i})
		}
	}
	return items, nil
}

func (s *tomlScanner) errorf(msg string) error { return errorAt(s.data, s.i, msg) }

func (s *tomlScanner) at(p string) bool { return bytes.HasPrefix(s.data[s.i:], []byte(p)) }

// blank skips spaces and tabs.
func (s *tomlScanner) blank() {
	for s.i < len(s.data) && (s.data[s.i] == ' ' || s.data[s.i] == '\t') {
		s.i++
	}
}

// lineEnd reports whether only a comment or the line break is left.
func (s *tomlScanner) lineEnd() bool {
	return s.i == len(s.data) || s.at("#") || s.at("\n") || s.at("\r\n")
}

// eol reads the rest of a line: blanks, a comment, the line break or the end.
func (s *tomlScanner) eol() error {
	s.blank()
	if s.at("#") {
		for s.i < len(s.data) && s.data[s.i] != '\n' && !s.at("\r\n") {
			s.i++
		}
	}
	switch {
	case s.i == len(s.data):
	case s.at("\n"):
		s.i++
	case s.at("\r\n"):
		s.i += 2
	default:
		return s.errorf(i18n.T("expected the end of the line", "ожидался конец строки"))
	}
	return nil
}

// space skips blanks, line breaks and comments (inside arrays and tables).
func (s *tomlScanner) space() {
	for {
		s.blank()
		switch {
		case s.at("#"):
			for s.i < len(s.data) && s.data[s.i] != '\n' {
				s.i++
			}
		case s.at("\n"):
			s.i++
		case s.at("\r\n"):
			s.i += 2
		default:
			return
		}
	}
}

// key reads a key: simple keys joined by dots.
func (s *tomlScanner) key() ([]string, error) {
	var parts []string
	for {
		s.blank()
		k, err := s.simpleKey()
		if err != nil {
			return nil, err
		}
		parts = append(parts, k)
		s.blank()
		if !s.at(".") {
			return parts, nil
		}
		s.i++
	}
}

func (s *tomlScanner) simpleKey() (string, error) {
	switch {
	case s.at(`"""`), s.at("'''"):
		return "", s.errorf(i18n.T("a key cannot be a multi-line string", "ключ не может быть многострочной строкой"))
	case s.at(`"`):
		return s.basicString()
	case s.at("'"):
		return s.literalString()
	}
	start := s.i
	for s.i < len(s.data) && isBareKeyByte(s.data[s.i]) {
		s.i++
	}
	if s.i == start {
		return "", s.errorf(i18n.T("expected a key", "ожидался ключ"))
	}
	return string(s.data[start:s.i]), nil
}

func isBareKeyByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func (s *tomlScanner) value() (any, error) {
	switch {
	case s.i == len(s.data):
		return nil, s.errorf(i18n.T("expected a value", "ожидалось значение"))
	case s.at(`"""`):
		return s.multilineString('"')
	case s.at("'''"):
		return s.multilineString('\'')
	case s.at(`"`):
		return s.basicString()
	case s.at("'"):
		return s.literalString()
	case s.at("["):
		return s.array()
	case s.at("{"):
		return s.inlineTable()
	}
	return s.scalar()
}

func (s *tomlScanner) array() (any, error) {
	s.i++
	out := []any{}
	for {
		s.space()
		if s.at("]") {
			s.i++
			return out, nil
		}
		v, err := s.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		s.space()
		switch {
		case s.at(","):
			s.i++
		case s.at("]"):
		default:
			return nil, s.errorf(i18n.T(`expected "," or "]" in the array`, `в массиве ожидалось "," или "]"`))
		}
	}
}

func (s *tomlScanner) inlineTable() (any, error) {
	s.i++
	out := map[string]any{}
	for {
		s.space()
		if s.at("}") {
			s.i++
			return out, nil
		}
		key, err := s.key()
		if err != nil {
			return nil, err
		}
		s.blank()
		if !s.at("=") {
			return nil, s.errorf(i18n.T(`expected "=" after the key`, `после ключа ожидалось "="`))
		}
		s.i++
		s.blank()
		v, err := s.value()
		if err != nil {
			return nil, err
		}
		ensureTable(out, key[:len(key)-1])[key[len(key)-1]] = v
		s.space()
		switch {
		case s.at(","):
			s.i++
		case s.at("}"):
		default:
			return nil, s.errorf(i18n.T(`expected "," or "}" in the inline table`, `во встроенной таблице ожидалось "," или "}"`))
		}
	}
}

// datePrefix is the date of a date-time written with a space: the scalar
// goes on past the space (1979-05-27 07:32:00Z).
var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *tomlScanner) scalar() (any, error) {
	start := s.i
	for s.i < len(s.data) {
		c := s.data[s.i]
		if c == ' ' && datePrefix.Match(s.data[start:s.i]) && s.i+1 < len(s.data) && s.data[s.i+1] >= '0' && s.data[s.i+1] <= '9' {
			s.i++
			continue
		}
		if !(isBareKeyByte(c) || c == '+' || c == '.' || c == ':') {
			break
		}
		s.i++
	}
	switch tok := string(s.data[start:s.i]); tok {
	case "":
		return nil, s.errorf(i18n.T("expected a value", "ожидалось значение"))
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return tomlScalar(tok), nil
	}
}

func (s *tomlScanner) literalString() (string, error) {
	start := s.i
	s.i++
	for s.i < len(s.data) {
		switch s.data[s.i] {
		case '\'':
			s.i++
			return string(s.data[start+1 : s.i-1]), nil
		case '\n':
			return "", s.errorf(i18n.T("line break in a string", "перевод строки в строке"))
		}
		s.i++
	}
	return "", errorAt(s.data, start, i18n.T("unterminated string", "строка не закрыта"))
}

func (s *tomlScanner) basicString() (string, error) {
	start := s.i
	s.i++
	var b strings.Builder
	for s.i < len(s.data) {
		switch c := s.data[s.i]; c {
		case '"':
			s.i++
			return b.String(), nil
		case '\n':
			return "", s.errorf(i18n.T("line break in a string", "перевод строки в строке"))
		case '\\':
			if err := s.escape(&b, false); err != nil {
				return "", err
			}
		default:
			b.WriteByte(c)
			s.i++
		}
	}
	return "", errorAt(s.data, start, i18n.T("unterminated string", "строка не закрыта"))
}

// multilineString reads """…""" or ”'…”': a line break right after the
// opening is dropped, and a basic one also takes escapes and the
// line-ending backslash. Up to two quotes may stand before the closing three.
func (s *tomlScanner) multilineString(q byte) (string, error) {
	start := s.i
	delim := strings.Repeat(string(q), 3)
	s.i += 3
	if s.at("\r\n") {
		s.i += 2
	} else if s.at("\n") {
		s.i++
	}
	var b strings.Builder
	for s.i < len(s.data) {
		if s.at(delim) {
			s.i += 3
			for extra := 0; extra < 2 && s.i < len(s.data) && s.data[s.i] == q; extra++ {
				b.WriteByte(q)
				s.i++
			}
			return b.String(), nil
		}
		if q == '"' && s.data[s.i] == '\\' {
			if err := s.escape(&b, true); err != nil {
				return "", err
			}
			continue
		}
		b.WriteByte(s.data[s.i])
		s.i++
	}
	return "", errorAt(s.data, start, i18n.T("unterminated string", "строка не закрыта"))
}

// tomlEscapes are the escapes of one character in a basic string.
var tomlEscapes = map[byte]string{'b': "\b", 't': "\t", 'n': "\n", 'f': "\f", 'r': "\r", 'e': "\x1b", '"': `"`, '\\': `\`}

// escape reads the escape at s.i (a backslash) into b; multiline allows
// the line-ending backslash.
func (s *tomlScanner) escape(b *strings.Builder, multiline bool) error {
	if s.i+1 >= len(s.data) {
		return s.errorf(i18n.T("unterminated string", "строка не закрыта"))
	}
	c := s.data[s.i+1]
	if r, ok := tomlEscapes[c]; ok {
		b.WriteString(r)
		s.i += 2
		return nil
	}
	digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
	if digits > 0 && s.i+2+digits <= len(s.data) {
		if n, err := strconv.ParseUint(string(s.data[s.i+2:s.i+2+digits]), 16, 32); err == nil && utf8.ValidRune(rune(n)) {
			b.WriteRune(rune(n))
			s.i += 2 + digits
			return nil
		}
	}
	// The line-ending backslash of a multi-line basic string: the break and
	// the blanks after it go.
	j := s.i + 1
	for j < len(s.data) && (s.data[j] == ' ' || s.data[j] == '\t') {
		j++
	}
	if multiline && j < len(s.data) && (s.data[j] == '\n' || bytes.HasPrefix(s.data[j:], []byte("\r\n"))) {
		for j < len(s.data) && strings.ContainsRune(" \t\r\n", rune(s.data[j])) {
			j++
		}
		s.i = j
		return nil
	}
	return s.errorf(i18n.T("invalid escape in a string", "неверная escape-последовательность в строке"))
}
