// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// QA-11 (QA 20.10): при русском языке CLI в половине ошибок message был
// английским при русском hint («provision: pass image or image_id, not
// both» + «Проверьте аргументы»), а в части — и hint («1 — a zonal control
// plane…»). docs/exit-codes.md: сообщения и подсказки — на языке CLI.
//
// Обход: каждый из 152 инструментов вызывается с пустыми, неполными и
// неверными аргументами и с каждым значением enum (облако — транспорт,
// отвечающий 500, 404 и 200 с пустыми списками; ещё раз — без ключей S3,
// образа и проекта). В ответах USAGE, CONFIG_INVALID и NOT_FOUND в режиме
// ru нет английской фразы (englishPhrase), в режиме en — кириллицы. Текст
// ответа API, Go и библиотеки S3 (foreignRE) остаётся как есть.

// quotedRE is what a message may keep in another language: a command or a
// value in quotes, a URL, an evo command.
var quotedRE = regexp.MustCompile("`[^`]*`|'[^']*'|\"[^\"]*\"|«[^»]*»|https?://\\S+|\\bevo( [a-z][a-z0-9_-]*)+")

// foreignRE is the text of the API, of Go and of the S3 library a message
// passes on as it is (the cause after "…: ").
var foreignRE = regexp.MustCompile(`json: cannot unmarshal .*|unexpected end of JSON input|illegal base64 data at input byte \d+|` +
	`open \S+ no such file or directory|The specified (bucket|key) does not exist\.|` +
	`Bucket name (cannot be empty|cannot be longer than \d+ characters|cannot be shorter than \d+ characters|contains invalid characters)|` +
	`Last-Modified time format is invalid.*`)

// englishPhrase returns the first run of three or more Latin words in s
// outside quotes, or "". A word is letters only; an identifier (snake_case,
// kebab-case, CamelCase, UPPER, with digits or dots) breaks the run, and so
// does punctuation: "tcp, udp, icmp" is a list of values, not a phrase.
func englishPhrase(s string) string {
	s = quotedRE.ReplaceAllString(foreignRE.ReplaceAllString(s, " · "), " · ")
	var run []string
	for _, tok := range strings.Fields(s) {
		core := strings.TrimLeft(tok, "([{")
		if core != tok {
			run = nil
		}
		trimmed := strings.TrimRight(core, ",.;:!?)]}…")
		if plainWord(trimmed) {
			run = append(run, trimmed)
			if len(run) >= 3 {
				return strings.Join(run, " ")
			}
		} else {
			run = nil
		}
		if trimmed != core {
			run = nil
		}
	}
	return ""
}

// apiErrorEnglishLeft are the tools whose API_ERROR still starts with
// English words of evo in the Russian CLI (1.3.1; prefixes left are from
// service clients — part 2 of englishWraps). The list only shrinks: a
// tool not here fails the test, and so does a line whose tool answers
// in Russian.
var apiErrorEnglishLeft = map[string]bool{}

// cloudSegment marks where an API_ERROR leaves evo's words for the
// cloud's: the service and status the client names («dns API 500»,
// «HTTP 500», «Organization API»), the transport's «API returned status»
// in either language, an error code (API_ERROR), the body itself, or a
// sentence of a client library («Object name cannot be empty», minio):
// evo's own segments start with a lower-case word.
var cloudSegment = regexp.MustCompile(`^(?:[\w.-]+ API(?: \d{3})?|HTTP \d{3}|API (?:returned|вернул)\b.*|[A-Z_]+|[{\[].*|[A-Z][a-z].*)$`)

// apiErrorPrefix is the part of an API_ERROR message evo wrote: the
// segments («a: b: …») before the first of the cloud's.
func apiErrorPrefix(msg string) string {
	var own []string
	for _, seg := range strings.Split(msg, ": ") {
		if cloudSegment.MatchString(strings.TrimSpace(seg)) {
			break
		}
		own = append(own, seg)
	}
	return strings.Join(own, ": ")
}

// latinSegment is the first segment of an evo prefix with no Cyrillic in
// it — a phrase of the English CLI («create zone»), or "".
func latinSegment(prefix string) string {
	if prefix == "" {
		return ""
	}
	for _, seg := range strings.Split(prefix, ": ") {
		if !hasCyrillic(seg) {
			return seg
		}
	}
	return ""
}

func plainWord(w string) bool {
	if w == "" {
		return false
	}
	for i, r := range w {
		if r > unicode.MaxASCII || !unicode.IsLetter(r) {
			return false
		}
		if i > 0 && unicode.IsUpper(r) { // CamelCase, UPPER
			return false
		}
	}
	return true
}

func TestEnglishPhrase(t *testing.T) {
	for s, want := range map[string]bool{
		"provision: pass image or image_id, not both":        true,
		"1 — a zonal control plane, 3 — a regional one":      true,
		"Pass record_type as one of: A, AAAA, CNAME":         true,
		"Check the id: vpc_list shows the networks.":         true,
		"unknown argument(s) bogus":                          false, // "argument(s)" is not a word
		"Укажите image или image_id, не оба":                 false,
		"Проверьте аргументы: 'evo tools describe vm_get'.":  false,
		"protocol \"bad value\" — допустимо: tcp, udp, icmp": false,
		"VPC API 404: refused":                               false,
		"Запустите `evo s3 keys create` и повторите":         false,
	} {
		if got := englishPhrase(s) != ""; got != want {
			t.Errorf("englishPhrase(%q) = %q, want found=%v", s, englishPhrase(s), want)
		}
	}
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// langArgSets are the calls of the walk: empty, only what is required,
// everything, and everything with one argument missing or wrong.
func langArgSets(tl Tool) []map[string]any {
	full := contractArgs(tl)
	props := schemaProps(tl)
	for k, raw := range props {
		p, _ := raw.(map[string]any)
		if items, _ := p["items"].(map[string]any); p["type"] == "array" && items["type"] == "string" {
			full[k] = []any{"web.example.invalid"}
		}
	}
	with := func(base map[string]any, k string, v any) map[string]any {
		m := make(map[string]any, len(base)+1)
		for kk, vv := range base {
			m[kk] = vv
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	req := map[string]any{}
	for _, k := range schemaRequired(tl) {
		req[k] = full[k]
	}
	for _, set := range tl.RequiredOneOf {
		req[set[0]] = full[set[0]]
	}
	sets := []map[string]any{nil, {"confirmed": true}, {"bogus_arg": 1}, req, full,
		with(req, "confirmed", true), with(full, "confirmed", true)}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, _ := props[k].(map[string]any)
		var bad []any
		switch p["type"] {
		case "integer", "number":
			bad = []any{float64(-1), float64(0), float64(1e6), "abc"}
		case "boolean":
			bad = []any{"yes", true}
		case "array":
			bad = []any{"a,b", []any{}, []any{"bad value!"}, []any{float64(0)}, float64(1)}
		case "object":
			bad = []any{"{", map[string]any{}, map[string]any{"bad key": float64(1)}}
		default:
			bad = []any{"", " ", "bad value!", "../x", "-1", "nope", strings.Repeat("a", 300),
				"00000000-0000-4000-8000-000000000002", "a.b", "10.0.0.0/33"}
		}
		sets = append(sets, with(full, k, nil))
		enum, _ := p["enum"].([]any)
		for _, e := range enum {
			sets = append(sets, with(full, k, e), with(req, k, e))
		}
		for _, v := range bad {
			sets = append(sets, with(full, k, v), with(req, k, v))
		}
	}
	return sets
}

type langCall struct {
	tool string
	args map[string]any
}

// walkLang calls every tool with every set of langArgSets and returns the
// complaints: in ru an English phrase, in en Cyrillic, in the message or
// the hint of a local refusal.
func walkLang(t *testing.T, lang, kind string, edit func(*config.Config)) (bad map[string]string, n int, left map[string]bool) {
	rt := &contractRT{kind: kind}
	reg := contractRegCfg(t, rt, edit)
	i18n.SetLang(lang)
	var calls []langCall
	for _, tl := range reg.Tools() {
		for _, a := range langArgSets(tl) {
			calls = append(calls, langCall{tl.Name, a})
		}
	}
	bad = map[string]string{}
	left = map[string]bool{}
	var mu sync.Mutex
	ch := make(chan langCall)
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range ch {
				tl, _ := reg.Lookup(c.tool)
				text, _, pan := contractCall(tl, c.args, 150*time.Millisecond)
				if pan != "" {
					continue
				}
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil {
					continue
				}
				reason, _ := m["reason"].(string)
				var fields map[string]string
				switch {
				case m["status"] == "confirmation_required":
					// The question before a change: its message and
					// warning are in the CLI's language too (break r4,
					// R4-20: «provision creates a billable VM…» in ru).
					reason = "confirmation_required"
					msg, _ := m["message"].(string)
					warn, _ := m["warning"].(string)
					fields = map[string]string{"message": msg, "warning": warn}
				case reason == clerrors.CodeUsage || reason == clerrors.CodeConfigInvalid || reason == clerrors.CodeNotFound:
					msg, _ := m["error"].(string)
					hint, _ := m["hint"].(string)
					fields = map[string]string{"error": msg, "hint": hint}
				case reason == clerrors.CodeAPIError:
					// The cloud's own text is the cloud's; what evo puts
					// before it is evo's (#319, D6).
					msg, _ := m["error"].(string)
					fields = map[string]string{"error prefix": apiErrorPrefix(msg)}
				default:
					continue
				}
				for field, s := range fields {
					what := ""
					switch {
					case field == "error prefix" && lang == i18n.RU:
						what = latinSegment(s)
					case lang == i18n.RU:
						what = englishPhrase(s)
					case hasCyrillic(s):
						what = "cyrillic"
					}
					if what == "" {
						continue
					}
					if field == "error prefix" && apiErrorEnglishLeft[c.tool] {
						mu.Lock()
						left[c.tool] = true
						mu.Unlock()
						continue
					}
					key := fmt.Sprintf("%s %s %s: %s", c.tool, reason, field, s)
					mu.Lock()
					if _, ok := bad[key]; !ok {
						b, _ := json.Marshal(c.args)
						bad[key] = fmt.Sprintf("[%s] args=%.200s", what, b)
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, c := range calls {
		ch <- c
	}
	close(ch)
	wg.Wait()
	return bad, len(calls), left
}

func TestQA11_ToolErrorsSpeakCLILanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	stillEnglish := map[string]bool{}
	defer func() {
		for tool := range apiErrorEnglishLeft {
			if !stillEnglish[tool] {
				t.Errorf("apiErrorEnglishLeft names %s, whose API_ERROR speaks Russian: remove the line", tool)
			}
		}
	}()
	// No S3 keys, no image, no project: the CONFIG_INVALID answers.
	bare := func(c *config.Config) { c.S3 = config.S3Config{}; c.ImageID = ""; c.ProjectID = "" }
	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, run := range []struct {
			kind string
			edit func(*config.Config)
		}{{"500", nil}, {"404", nil}, {"200", nil}, {"500", bare}} {
			kind := run.kind
			if run.edit != nil {
				kind += "-bare"
			}
			bad, n, left := walkLang(t, lang, run.kind, run.edit)
			for tool := range left {
				stillEnglish[tool] = true
			}
			if n < 3000 {
				t.Fatalf("only %d calls", n)
			}
			keys := make([]string, 0, len(bad))
			for k := range bad {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				t.Errorf("%s/%s %s %s", lang, kind, k, bad[k])
			}
		}
	}
}

// QA-11, the same round: vm_get with vm_id and name, image_get with image
// and image_id took one of them silently. Both are USAGE before the cloud,
// as release_floating_ip with id and ip_address (QA-8).
func TestQA11_BothArgumentsOfAPair(t *testing.T) {
	runUsageCases(t, []usageCase{
		{"vm_get", map[string]any{"vm_id": qaID1, "name": "web"}, "pass vm_id or name, not both"},
		{"vm_get", map[string]any{"vm_id": qaID1, "name": qaID1}, "not both"},
		{"image_get", map[string]any{"image": "Ubuntu-24.04", "image_id": qaID1}, "pass image or image_id, not both"},
	})
}
