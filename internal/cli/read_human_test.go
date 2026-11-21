// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// readForm are the keys of a read answer that are its form, not a value
// of the thing read: the human output drops them on purpose.
var readForm = map[string]string{
	// The marker is for a client of the JSON (--json, MCP); a person reads
	// the table, whose cells are sanitized (renderAnswer).
	"_untrusted": "the fields with text of the cloud, for a client of the JSON",
}

// readUnnamed are the values of a read answer (a card, a list) that the
// human output of the same call does not show — "tool path" with the class
// (as humanUnnamed) and why. The list only shrinks: the human output shows
// the value and the line goes. A line the human output shows now, or whose
// value the answer no longer has, fails the test. Closes by 1.10: what
// stays is an exception declared with a reason «Риск» accepted (plan 1.9,
// phase 0, И1).
var readUnnamed = map[string]struct{ class, why string }{
	"billing_consumption currency":                     {unnamedNoise, "валюта; человеку — знак ₽ у каждой суммы"},
	"billing_consumption vat.by_service":               {unnamedNoise, "пояснение колонок; человеку — «with VAT / without VAT» у суммы"},
	"billing_consumption vat.by_service.cost_with_nds": {unnamedNoise, "пояснение колонок; человеку — «with VAT / without VAT» у суммы"},
	"billing_consumption vat.total":                    {unnamedNoise, "пояснение суммы; человеку — «without VAT» у суммы"},
	"billing_consumption vat.total_with_nds":           {unnamedNoise, "пояснение суммы; человеку — «with VAT» у суммы"},
	"billing_finops currency":                          {unnamedNoise, "валюта; человеку — знак ₽ у каждой суммы"},
	"billing_finops runway_note":                       {unnamedNoise, "строка Runway говорит то же своими словами"},
	"billing_finops vat.anomalies.now_with_nds":        {unnamedNoise, "пояснение колонок роста; человеку — «with VAT / without VAT»"},
	"billing_finops vat.anomalies.prev_with_nds":       {unnamedNoise, "пояснение колонок роста; человеку — «with VAT / without VAT»"},
	"billing_finops vat.by_service":                    {unnamedNoise, "пояснение колонок; человеку — «with VAT / without VAT» у суммы"},
	"billing_finops vat.by_service.cost_with_nds":      {unnamedNoise, "пояснение колонок; человеку — «with VAT / without VAT» у суммы"},
	"billing_finops vat.daily_burn_with_nds":           {unnamedNoise, "пояснение суммы в день; человеку — «with VAT» у суммы"},
	"billing_finops vat.total_cost":                    {unnamedNoise, "пояснение суммы; человеку — «without VAT» у суммы"},
	"billing_finops vat.total_cost_with_nds":           {unnamedNoise, "пояснение суммы; человеку — «with VAT» у суммы"},
	"doctor checks.http_status":                        {unnamedNoise, "код ответа пробы; человеку — «reachable» в строке проверки"},
	"image_get create.tool":                            {unnamedNoise, "инструмент MCP для агента; человеку — команда CLI"},
	"image_get fields.mandatory_group":                 {unnamedWork, "группа обязательных полей образа; человеку — только REQUIRED"},
	"image_get fields.role":                            {unnamedNoise, "роль поля; человеку — «evo: the SSH key» в FILLED BY"},
	"k8s_addon_versions billable":                      {unnamedPrice, "платный ли аддон: карточка версий не говорит, список аддонов говорит"},
	"k8s_addon_versions deploy_with.tool":              {unnamedNoise, "инструмент MCP для агента; человеку — команда CLI"},
	"k8s_addon_versions releases.has_schema":           {unnamedNoise, "есть ли схема настроек; человеку — размер схемы или «—»"},
	"k8s_addon_versions releases.latest":               {unnamedNoise, "последняя версия; человеку — «*» у версии"},
	"k8s_addons addons.deploy_with.tool":               {unnamedNoise, "инструмент MCP для агента; человеку — команда CLI"},
	"k8s_addons categories":                            {unnamedWork, "все категории каталога (значения отбора); человеку — только категории показанных аддонов"},
	"list_flavors flavors.zones":                       {unnamedWork, "зоны, где есть конфигурация: таблица — не больше семи колонок"},
	"marketplace_list items.available_in_project":      {unnamedWork, "доступен ли образ проекту"},
	"marketplace_list items.billable":                  {unnamedPrice, "платный ли аддон"},
	"marketplace_list items.deploy.tool":               {unnamedNoise, "инструмент MCP для агента; человеку — команда CLI"},
	"metrics_scrape format":                            {unnamedNoise, "формат выгрузки; человеку — сама выгрузка"},
	"metrics_scrape match":                             {unnamedWork, "отбор метрик, по которому сделана выгрузка"},
	"metrics_scrape project":                           {unnamedNoise, "проект выгрузки: тот, что в профиле"},
	"org_projects active":                              {unnamedWork, "текущий проект ключа: строка «The key sees no projects» его не называет"},
	"pg_specs specifications.min_storage_gb":           {unnamedPrice, "минимум диска спецификации: таблица — не больше семи колонок"},
	"status cloud_checked":                             {unnamedNoise, "сверено ли с облаком; человеку — строка «In the cloud»"},
	"status project_id":                                {unnamedWork, "проект развёртывания"},
	"status ssh_user":                                  {unnamedWork, "пользователь SSH ВМ развёртывания"},
	"whoami token_ttl_seconds":                         {unnamedNoise, "срок токена; человеку — «1h0m0s left»"},
}

// Release plan 1.9, phase 0, И1 («человек видит каждое поле карточки»):
// what the agent reads in a card or a list (--json, the same as MCP), the
// person at the terminal reads too — as С2 holds it for a preview
// (TestPreview_HumanShowsEveryValue). For the command of every read tool
// on the world of TestParity_JSONEqualsTool, in English — the language of
// the JSON (D6), whose texts a Russian output says in its own words: every
// value of the --json answer (a string or a number; true/false by
// the label of its field) is in the human output of the same call at a
// terminal 200 columns wide, or is in it in another form (shownAsWritten),
// or is the id of a thing the output names (namedBySibling), a form key
// (readForm) or a line of readUnnamed.
func TestRead_HumanShowsEveryValue(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	t.Setenv("COLUMNS", "200")
	tools, seen, verified := map[string]bool{}, map[string]bool{}, map[string]bool{}
	seenIn := map[string]string{} // tool → its answer and human output, for a stale line
	var bad []string
	byClass := map[string]int{}
	lang := i18n.Lang()
	i18n.SetLang(i18n.EN)
	defer i18n.SetLang(lang)
	for _, c := range cases {
		if c.tool.Mutating || c.state != "" || textAnswer[c.tool.Name] != "" {
			continue
		}
		tools[c.tool.Name] = true
		// --yes: a read that asks first (secret_value) shows what it read.
		run := func(extra ...string) (int, string, string) {
			args := copyArgs(c.args)
			done := func() {}
			if c.setup != nil {
				e, d := c.setup(t, w)
				for k, v := range e {
					args[k] = v
				}
				done = d
			}
			line := c.line
			if line == nil {
				cmd := commandsByTool()[c.tool.Name]
				line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
			}
			fake.reset()
			newProcess()
			code, out, errOut := runCLI(t, c.stdin, append(append([]string{"--yes"}, extra...), line...)...)
			done()
			forgetRun(w)
			return code, readVolatile(out), readVolatile(errOut)
		}
		code, jsonOut, jsonErr := run("--json")
		var answer any
		if code != 0 || json.Unmarshal([]byte(jsonOut), &answer) != nil {
			bad = append(bad, fmt.Sprintf("%s: --json fails or is no JSON: exit %d, %s %s", c.path, code, clipLine(jsonOut), clipLine(jsonErr)))
			continue
		}
		code, out, errOut := run()
		if code != 0 {
			bad = append(bad, fmt.Sprintf("%s: the human output fails: exit %d, %s", c.path, code, clipLine(errOut)))
			continue
		}
		human := out + errOut
		// What a stale line of readUnnamed is checked against: when the
		// line is not seen, the answer and the human output say why — the
		// value gone from the answer (a probe of doctor cut by its time
		// limit on a slow runner, C(7) on #2218), or its text found in the
		// output (hypotheses, one run).
		seenIn[c.tool.Name] = clipLong(jsonOut) + " | human: " + clipLong(human)
		if c.tool.Name == "list_zones" {
			// Check the actual cell, even if another row or the header
			// contains "enabled". Only verified booleans leave the walk.
			bad = append(bad, checkZoneReadCells(c.path, answer, out, i18n.EN)...)
		}
		for _, miss := range valueMisses(answer, human, readForm) {
			key := c.tool.Name + " " + miss.path
			if shownAsWritten(miss.value, human) {
				continue
			}
			if namedBySibling(answer, miss.path, miss.value, human) {
				verified[key] = true
				continue
			}
			if d, ok := readUnnamed[key]; ok {
				if !seen[key] {
					byClass[d.class]++
				}
				seen[key] = true
				continue
			}
			bad = append(bad, fmt.Sprintf("%s = %s: in --json of %s, not in its human output — show it, or a line of readUnnamed with its class", key, clipLine(miss.value), c.path))
		}
	}
	for _, tl := range newRegistry().Tools() {
		if !tl.Mutating && tl.CLI.Verb != "" && textAnswer[tl.Name] == "" && !tools[tl.Name] {
			bad = append(bad, tl.Name+": a read tool with a command and no case in parityArgs")
		}
	}
	for key, d := range readUnnamed {
		switch {
		case verified[key] && !seen[key]:
			bad = append(bad, key+": in readUnnamed, but the human output names its thing (namedBySibling): remove the line")
		case !seen[key]:
			tool, _, _ := strings.Cut(key, " ")
			bad = append(bad, key+": in readUnnamed, but the human output shows it, or the answer has it no more: remove the line ("+seenIn[tool]+")")
		}
		switch d.class {
		case unnamedAccess, unnamedPrice, unnamedWork, unnamedNoise:
		default:
			bad = append(bad, fmt.Sprintf("%s: class %q", key, d.class))
		}
		if d.why == "" {
			bad = append(bad, key+": a line without why")
		}
	}
	t.Logf("read tools: %d; values the person does not see, by class: %v", len(tools), byClass)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// checkZoneReadCells compares the JSON bool with its representation in the
// table of the same command. The legacy command returns a bare JSON array.
func checkZoneReadCells(path string, answer any, table, lang string) []string {
	legacy := path == "evo list-zones"
	rows, _ := answer.([]any)
	if !legacy {
		object, _ := answer.(map[string]any)
		rows, _ = object["zones"].([]any)
	}
	header, yes, no := "ENABLED", "true", "false"
	if legacy {
		header, yes, no = "New resources", "open", "closed (enabled=false)"
	}
	if lang == i18n.RU {
		header, yes, no = "Открыта", "да", "нет"
		if legacy {
			header, yes, no = "Новые ресурсы", "открыта", "закрыта (enabled=false)"
		}
	}
	lines := strings.Split(strings.TrimSpace(table), "\n")
	columns := zoneTableGap.Split(strings.TrimSpace(lines[0]), -1)
	column := -1
	for i, label := range columns {
		if label == header {
			column = i
		}
	}
	cells := map[string]string{}
	for _, line := range lines[1:] {
		fields := zoneTableGap.Split(strings.TrimSpace(line), -1)
		if column >= 0 && column < len(fields) {
			cells[fields[0]] = fields[column]
		}
	}
	var bad []string
	for _, item := range rows {
		row, _ := item.(map[string]any)
		enabled, present := row["enabled"].(bool)
		if !present {
			continue
		}
		id, _ := row["id"].(string)
		want := no
		if enabled {
			want = yes
		}
		if cells[id] != want {
			bad = append(bad, fmt.Sprintf("%s: zone %s enabled=%t: column %q shows %q, want %q", path, id, enabled, header, cells[id], want))
			continue
		}
		delete(row, "enabled") // Verified for this row. Check its remaining values as usual.
	}
	return bad
}

var zoneTableGap = regexp.MustCompile(` {2,}|\t+`)

func TestRead_ZoneEnabledBelongsToItsRow(t *testing.T) {
	for _, tc := range []struct {
		label, table string
		misses       int
	}{
		{"correct", "a  az-one  open\nb  az-two  closed (enabled=false)", 0},
		{"swapped", "a  az-one  closed (enabled=false)\nb  az-two  open", 2},
		{"missing", "a  az-one\nb  az-two  closed (enabled=false)", 1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			answer := []any{map[string]any{"id": "a", "enabled": true}, map[string]any{"id": "b", "enabled": false}}
			if bad := checkZoneReadCells("evo list-zones", answer, "ID  Name  New resources\n"+tc.table, i18n.EN); len(bad) != tc.misses {
				t.Fatalf("mismatches %v, want %d", bad, tc.misses)
			}
		})
	}
}

// A value is never «shown» by the digits of a temporary directory of the
// test.
func TestRead_VolatileHidesTheTestDir(t *testing.T) {
	human := readVolatile("warn config dir /tmp/TestRead_HumanShowsEveryValue3020022521/001 has mode 0755")
	if shownAsWritten("200", human) {
		t.Errorf("200 read as shown in %q", human)
	}
	if !shownAsWritten("0755", human) {
		t.Errorf("the rest of the line is lost: %q", human)
	}
	// On Windows the answer holds the path in JSON (\\ between the parts)
	// and the human output as it is (\): both read as the same <dir>.
	path := `C:\Users\RUNNER~1\AppData\Local\Temp\TestRead_HumanShowsEveryValue3020022521\001\helm.bat`
	inJSON, _ := json.Marshal(path)
	var answer string
	if err := json.Unmarshal([]byte(readVolatile(string(inJSON))), &answer); err != nil {
		t.Fatal(err)
	}
	if got := readVolatile(path); got != answer || !strings.HasPrefix(got, "<dir>") {
		t.Errorf("the human output reads as %q, the answer as %q: the same path", got, answer)
	}
}

// clipLong is s on one line, cut to what a log of CI shows in full.
func clipLong(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 4000 {
		return s[:4000] + "…"
	}
	return s
}

// readVolatile reads as one what two calls of the same read differ in
// (humanVolatile: the plan, the stack job, a time of the call, how long a
// probe of doctor took), and a time the way the human output writes it
// (2026-01-01 01:00:00 UTC) as a time of the JSON: a time is shown when the
// output has a time in its place, whatever its form.
func readVolatile(s string) string {
	// A temporary directory of the test (testDir): the human output of
	// doctor names its files, and its random digits held 200 on 1 run in
	// 12 — the http_status of a probe read as shown (C(7) on #2218, A(7) on
	// #2262).
	return testDir.ReplaceAllString(humanTime.ReplaceAllString(humanVolatile(s), "<time>"), "<dir>")
}

var humanTime = regexp.MustCompile(`\d{4}-\d\d-\d\d \d\d:\d\d(:\d\d)?( UTC)?`)

// shownAsWritten reports whether the human output shows value in the form
// a person reads it: a text on one line (a cell puts a text of several
// lines on one), a number with its thousands grouped (1,000 or 1 000), a
// word of an enum in lower case with spaces (END_OF_SUPPORT — «end of
// support») and a date as its day and month (19.10).
func shownAsWritten(value, human string) bool {
	oneLine := strings.Join(strings.Fields(human), " ")
	if strings.Contains(oneLine, strings.Join(strings.Fields(value), " ")) {
		return true
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil && n == float64(int64(n)) && (n >= 1000 || n <= -1000) {
		for _, sep := range []string{",", " ", "\u00a0", "\u202f"} {
			if strings.Contains(human, groupThousands(int64(n), sep)) {
				return true
			}
		}
	}
	if enumWord.MatchString(value) && strings.Contains(strings.ToLower(oneLine), strings.ToLower(strings.ReplaceAll(value, "_", " "))) {
		return true
	}
	if d, err := time.Parse(time.DateOnly, value); err == nil {
		return strings.Contains(human, d.Format("02.01"))
	}
	return false
}

// enumWord is a value of an enum the cloud names in capitals.
var enumWord = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$|^[A-Z]{3,}$`)

// groupThousands writes n with its thousands apart: 1000 — «1,000».
func groupThousands(n int64, sep string) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func TestShownAsWritten_Forms(t *testing.T) {
	for _, c := range []struct {
		value, human string
		want         bool
	}{
		{"line one\nline two", "kubeconfig  line one line two", true},
		{"1000", "Balance: 1,000 ₽", true},
		{"1000", "Баланс: 1 000 ₽", true},
		{"1000", "Balance: 100 ₽", false},
		{"END_OF_SUPPORT", "v1.13.0  end of support", true},
		{"ACTIVE", "v1.14.0  active", true},
		{"END_OF_SUPPORT", "v1.13.0  supported", false},
		{"2026-10-19", "19.10–17.11 (30 days)", true},
		{"2026-10-19", "20.10–17.11 (30 days)", false},
		{"ru.AZ-1", "zone ru.AZ-2", false},
	} {
		if got := shownAsWritten(c.value, c.human); got != c.want {
			t.Errorf("shownAsWritten(%q, %q) = %v, want %v", c.value, c.human, got, c.want)
		}
	}
}
