// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	"github.com/shatim-ops/evo-cli/internal/output"
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
	return englishWordRun(s, 3)
}

func englishWordRun(s string, minimum int) string {
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
			if len(run) >= minimum {
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

// successEnglishLeft is what a success or an answer «under way» still says
// in English in the Russian CLI: «tool field» → why it waits. The CLI's
// table shows these fields as they are (release plan 1.9, «Язык CLI в
// успешных ответах…»); the walk found them when it first read successes.
// The list only shrinks: a line whose text speaks Russian fails the test.
// Closes by 1.9 (owner's decision 14.11: the whole translation in 1.9,
// line «Язык CLI: весь перевод — в 1.9»).
var successEnglishLeft = map[string]string{}

// A service/status marker or a raw response body starts a verbatim quote.
// The transport's localized status phrase is itself evo text; only the
// body following it is exempt from the language check.
var cloudSegment = regexp.MustCompile(`^(?:[\w.-]+ API \d{3}|HTTP \d{3})$`)
// This registered product name occurs verbatim in the Russian marketplace error.
var apiProductName = regexp.MustCompile(`\bManaged Kubernetes\b`)
var serviceLabel = regexp.MustCompile(`^[\w.-]+ API$`)
var requestIDSuffix = regexp.MustCompile(requestIDNamed.String() + "$")
var transportStatus = regexp.MustCompile(`^API (?:returned status \d{3}(?: after \d+ retries)?|вернул статус \d{3}(?: после \d+ повторов)?)$`)

func apiErrorPrefix(msg string) string {
	// withRequestID appends this metadata to the final response body.
	msg = requestIDSuffix.ReplaceAllString(msg, "")
	var own []string
	for {
		seg, rest, more := strings.Cut(msg, ": ")
		seg = strings.TrimSpace(seg)
		// Some clients return an unwrapped SDK or Go error. Recognize
		// its complete text, without relying on the first letter.
		quotedBody := strings.HasPrefix(msg, "«") && strings.HasSuffix(msg, "»")
		rawJSON := (strings.HasPrefix(msg, "{") || strings.HasPrefix(msg, "[")) && json.Valid([]byte(msg))
		libraryError := msg == io.EOF.Error() || msg == "Object name cannot be empty" ||
			(msg != "" && foreignRE.FindString(msg) == msg)
		if cloudSegment.MatchString(seg) || rawJSON || quotedBody || libraryError {
			break
		}
		// A nested CLIError code or a service name may precede more
		// text written by evo. Skip the label but keep checking its tail.
		if !clerrors.IsCode(seg) && !serviceLabel.MatchString(seg) {
			own = append(own, seg)
		}
		quotedReply := strings.HasSuffix(seg, "Ответ облака") && strings.HasPrefix(rest, "«") && strings.HasSuffix(rest, "»")
		if !more || quotedReply || transportStatus.MatchString(seg) {
			break
		}
		msg = rest
	}
	return strings.Join(own, ": ")
}

// apiLanguageIssue checks the words surrounding a service response. Dropping
// the whole message at an HTTP marker must not turn a missing Russian frame
// into a successful check.
func apiLanguageIssue(lang, prefix string) string {
	if lang == i18n.RU {
		if prefix == "" {
			return "missing Russian context"
		}
		return latinSegment(prefix)
	}
	if hasCyrillic(prefix) {
		return "cyrillic"
	}
	return ""
}

// latinSegment finds a wholly untranslated segment («create zone») or an
// English phrase left inside the Russian text that introduces a service quote.
func latinSegment(prefix string) string {
	if prefix == "" {
		return ""
	}
	for _, seg := range strings.Split(prefix, ": ") {
		if !hasCyrillic(seg) {
			return seg
		}
		if phrase := englishWordRun(apiProductName.ReplaceAllString(seg, " · "), 2); phrase != "" {
			return phrase
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

func TestQA11_APIErrorLanguageBoundary(t *testing.T) {
	for _, tc := range []struct{ message, wantEnglish string }{
		{"Cannot load example: HTTP 500: fixture", "Cannot load example"},
		{"cannot load example: HTTP 500: fixture", "cannot load example"},
		{"Не удалось загрузить: Cannot load example: dns API 500: fixture", "Cannot load example"},
		{"[evo] Cannot load example: HTTP 500: fixture", "[evo] Cannot load example"},
		{`Не удалось прочитать: {"message": "Cannot load example"}`, ""},
		{`Не удалось прочитать: {"error":"refused"} (X-Request-Id example)`, ""},
		{"Organization API: Cannot load example: HTTP 500: fixture", "Cannot load example"},
		{"Не удалось прочитать ответ: EOF", ""},
		{"API_ERROR: Cannot load example: HTTP 500: fixture", "Cannot load example"},
		{"API returned status 500: fixture", "API returned status 500"},
		{"API returned unexpected format: HTTP 500: fixture", "API returned unexpected format"},
		{"Не удалось загрузить пример: HTTP 500: Cannot load example", ""},
		{"Не удалось загрузить пример: dns API 500: Cannot load example", ""},
		{"Не удалось загрузить пример: API вернул статус 500: Cannot load example", ""},
		{"Не удалось прочитать объект: Object name cannot be empty", ""},
		{"Не удалось прочитать объект: Object name cannot be shown", "Object name cannot be shown"},
	} {
		if got := latinSegment(apiErrorPrefix(tc.message)); got != tc.wantEnglish {
			t.Errorf("English in API error %q = %q, want %q", tc.message, got, tc.wantEnglish)
		}
	}
}

func TestQA11_APIErrorFrame(t *testing.T) {
	const bare = "HTTP 500: Cannot load example"
	for _, tc := range []struct{ name, lang, msg, want string }{
		{"bare-ru", i18n.RU, bare, "missing Russian context"},
		{"bare-en", i18n.EN, bare, ""},
		{"framed-ru", i18n.RU, "Не удалось загрузить пример: " + bare, ""},
		{"english-ru", i18n.RU, "Cannot load example: " + bare, "Cannot load example"},
		{"lowercase-ru", i18n.RU, "cannot load example: " + bare, "cannot load example"},
		{"mixed-ru", i18n.RU, "Ошибка Cannot load example: " + bare, "Cannot load"},
		{"untranslated", i18n.RU, "Request failed: HTTP 500: fixture", "Request failed"},
		{"mixed-short", i18n.RU, "Ошибка Request failed: HTTP 500: fixture", "Request failed"},
		{"product-name", i18n.RU, "Каталог Managed Kubernetes не прочитан: " + bare, ""},
		{"product-name-and-english", i18n.RU, "Каталог Managed Kubernetes Request failed: " + bare, "Request failed"},
		{"http-body", i18n.RU, "Не удалось выполнить запрос: HTTP 500: Request failed", ""},
		{"service-quote", i18n.RU, "Сервис ответил: «Request failed»", ""},
		{"bare-quote", i18n.RU, "«Request failed»", "missing Russian context"},
		{"quoted-ru", i18n.RU, "Cloud.ru отклонил запрос (test API 500). Ответ облака: «Cannot load example»", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Read the same error field a caller receives from an API_ERROR.
			var response struct{ Error, Reason string }
			if err := json.Unmarshal([]byte(errResult(clerrors.New(clerrors.CodeAPIError, tc.msg, ""))), &response); err != nil {
				t.Fatal(err)
			}
			got := apiLanguageIssue(tc.lang, apiErrorPrefix(response.Error))
			if response.Reason != clerrors.CodeAPIError || got != tc.want {
				t.Errorf("%s/%s %q: got %q, want %q", tc.lang, response.Reason, response.Error, got, tc.want)
			}
			t.Logf("%s/%s %q: finding=%q", tc.lang, response.Reason, response.Error, got)
		})
	}
}

// Each world runs a tool with a local failure and checks the resulting error,
// including tools that classify a service refusal as something besides API_ERROR.
func TestQA11_RefusalWorlds(t *testing.T) {
	old := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(old) })
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Run(lang, func(t *testing.T) {
			i18n.SetLang(lang)
			check := func(t *testing.T, text string, failed bool, code string) {
				t.Helper()
				var r struct{ Reason, Error, Hint string }
				if err := json.Unmarshal([]byte(text), &r); err != nil {
					t.Fatal(err)
				}
				if !failed || r.Reason == "" || (code != "" && r.Reason != code) || r.Error == "" {
					t.Fatalf("unexpected tool response: %s", text)
				}
				if issue := apiLanguageIssue(lang, apiErrorPrefix(r.Error)); issue != "" {
					t.Errorf("%s error %q: %s", r.Reason, r.Error, issue)
				}
				if r.Hint != "" && (lang == i18n.RU) != hasCyrillic(r.Hint) {
					t.Errorf("%s hint in wrong language: %q", r.Reason, r.Hint)
				}
			}
			t.Run("dns-service-body", func(t *testing.T) {
			rt := &contractRT{kind: "500text"}
			r := contractReg(t, rt)
			_, text, failed := callNamed(t, r, "dns_record_create", map[string]any{
				"zone_id": contractID, "name": "web", "record_type": "a", "value": "192.0.2.1", "type": "public",
			})
			if reqs := rt.take(); len(reqs) == 0 || contractWrites(reqs) != 0 || !strings.Contains(text, "dns API 500: Request failed") {
				t.Fatalf("expected the preflight read and its verbatim service response: %v, %s", reqs, text)
			}
			check(t, text, failed, clerrors.CodeAPIError)
		})
		t.Run("container-apps-499", func(t *testing.T) {
				rt := &contractRT{kind: "ca499"}
				r := contractReg(t, rt)
				_, text, failed := callNamed(t, r, "ca_deploy", map[string]any{"name": "web", "image": "example.invalid/app", "confirmed": true})
				if writes := contractWrites(rt.take()); writes != 1 || !strings.Contains(text, "Instance type by resources not found") {
					t.Fatalf("expected the quoted 499 from the create, writes=%d: %s", writes, text)
				}
				check(t, text, failed, "")
			})
			t.Run("vm-timeout", func(t *testing.T) {
				text, failed, _, _ := provisionRun(t, context.Background(), &progressCloud{states: []string{"creating"}}, false, output.ModeJSON, 20*time.Millisecond, nil)
				var r struct{ Status string }
				if err := json.Unmarshal([]byte(text), &r); err != nil || r.Status != "timeout" {
					t.Fatalf("expected VM timeout, got %s", text)
				}
				check(t, text, failed, clerrors.CodeVMProvisionFailed)
			})
			t.Run("invalid-compose", func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "compose.yaml")
				if err := os.WriteFile(path, []byte("services: ["), 0600); err != nil {
					t.Fatal(err)
				}
				r := contractRegCfg(t, &contractRT{kind: "200"}, func(c *config.Config) { c.ComposeFile = path })
				_, text, failed := callNamed(t, r, "deploy", map[string]any{"compose_file": path})
				check(t, text, failed, clerrors.CodeComposeInvalid)
			})
		})
	}
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
	for _, set := range tl.Decl.RequiredOneOf {
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
	i18n.SetLang(lang)
	bad = map[string]string{}
	left = map[string]bool{}
	var mu sync.Mutex
	n = walkAnswers(t, kind, edit, func(c langCall, m map[string]any) {
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
		case reason == "" && m["status"] != nil && m["error"] == nil:
			// A success and an answer «under way» (release plan 1.9,
			// «Язык CLI в успешных ответах…»): the CLI's table shows
			// their notes as they are.
			reason = fmt.Sprint("status ", m["status"])
			fields = map[string]string{}
			for _, f := range []string{"note", "next", "warning", "resource_status_note"} {
				if v, ok := m[f].(string); ok {
					// The caller's own words (a name it passed) are not
					// evo's text: «Container Apps web отсутствует».
					for _, a := range c.args {
						if w, ok := a.(string); ok && w != "" {
							v = strings.ReplaceAll(v, w, "")
						}
					}
					fields[f] = v
				}
			}
		case reason == clerrors.CodeAPIError:
			// The cloud's own text is the cloud's; what evo puts
			// before it is evo's (#319, D6).
			msg, _ := m["error"].(string)
			fields = map[string]string{"error prefix": apiErrorPrefix(msg)}
		default:
			return
		}
		for field, s := range fields {
			what := ""
			switch {
			case field == "error prefix":
				what = apiLanguageIssue(lang, s)
			case lang == i18n.RU:
				what = englishPhrase(s)
			case hasCyrillic(s):
				what = "cyrillic"
			}
			if what == "" {
				continue
			}
			if strings.HasPrefix(reason, "status ") && successEnglishLeft[c.tool+" "+field] != "" {
				mu.Lock()
				left[c.tool+" "+field] = true
				mu.Unlock()
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
	})
	return bad, n, left
}

// walkAnswers calls every tool of a registry whose cloud answers every
// request as kind (contractRT), its configuration edited by edit, with each
// of its langArgSets, and hands each JSON object answer to check (called
// from several goroutines). It returns the number of calls.
func walkAnswers(t *testing.T, kind string, edit func(*config.Config), check func(c langCall, m map[string]any)) int {
	rt := &contractRT{kind: kind}
	reg := contractRegCfg(t, rt, edit)
	var calls []langCall
	for _, tl := range reg.Tools() {
		for _, a := range langArgSets(tl) {
			calls = append(calls, langCall{tl.Name, a})
		}
	}
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
				check(c, m)
			}
		}()
	}
	for _, c := range calls {
		ch <- c
	}
	close(ch)
	wg.Wait()
	return len(calls)
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
		for key := range successEnglishLeft {
			if !stillEnglish[key] {
				t.Errorf("successEnglishLeft names %q, which speaks Russian now: remove the line", key)
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
