// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/spf13/cobra"
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
var readUnnamed = map[string]struct{ class, why string }{}

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
	root := newRootCmd()
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
		if c.tool.Name == "billing_consumption" || c.tool.Name == "billing_finops" {
			if err := checkBillingRead(c.tool.Name, answer.(map[string]any), human); err != nil {
				bad = append(bad, c.path+": "+err.Error())
			}
		}
		if obj, ok := answer.(map[string]any); ok {
			switch c.tool.Name {
			case "doctor":
				code, verbose, errOut := run("--verbose")
				if code != 0 {
					bad = append(bad, errOut)
				} else if err := checkDoctorHTTP(obj, human, verbose); err != nil {
					bad = append(bad, err.Error())
				}
			case "image_get", "k8s_addon_versions", "k8s_addons":
				if err := checkDeployRoutes(root, c.tool.Name, obj, human); err != nil {
					bad = append(bad, err.Error())
				}
			case "metrics_scrape":
				if err := checkScrapeAnswer(obj, out, pvProject, `{__name__=~".+"}`); err != nil {
					bad = append(bad, err.Error())
				}
			case "marketplace_list":
				if err := checkDeployRoutes(root, c.tool.Name, obj, human); err != nil {
					bad = append(bad, err.Error())
				}
				if err := checkMarketFlags(obj, human); err != nil {
					bad = append(bad, err.Error())
				}
			case "whoami":
				if ttl, ok := obj["token_ttl_seconds"].(float64); ok {
					remaining := (time.Duration(ttl) * time.Second).Truncate(time.Minute).String()
					if !strings.Contains(out, remaining+" left,") {
						bad = append(bad, "whoami: incorrect token duration")
					} else {
						delete(obj, "token_ttl_seconds")
					}
				}
			case "status":
				if checked, ok := obj["cloud_checked"].(bool); ok {
					state, _ := obj["cloud_state"].(string)
					want := "not checked:"
					if checked {
						want = state
						if want == "" {
							want = "found, state unknown"
						}
						if obj["not_found_in_cloud"] == true {
							want = "not found (deleted outside evo)"
						}
					}
					if !strings.Contains(strings.Join(strings.Fields(human), " "), "In the cloud "+want) {
						bad = append(bad, "status: wrong cloud check row")
					} else {
						delete(obj, "cloud_checked")
					}
				}
			}
		}
		if c.tool.Name == "list_zones" {
			// Check the actual cell, even if another row or the header
			// contains "enabled". Only verified booleans leave the walk.
			bad = append(bad, checkZoneReadCells(c.path, answer, out, i18n.EN)...)
		}
		if c.tool.Name == "k8s_addon_versions" {
			var versions addonVersionsAnswer
			if err := json.Unmarshal([]byte(jsonOut), &versions); err != nil {
				t.Fatal(err)
			}
			if err := checkAddonReleaseCells(versions, human); err != nil {
				bad = append(bad, err.Error())
			} else {
				obj := answer.(map[string]any)
				releases, _ := obj["releases"].([]any)
				for _, item := range releases {
					row := item.(map[string]any)
					delete(row, "has_schema")
					delete(row, "latest")
				}
			}
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

func checkAddonReleaseCells(a addonVersionsAnswer, human string) error {
	for _, release := range a.Releases {
		version := release.Version
		if release.Latest {
			version += " *"
		}
		found := false
		for _, line := range strings.Split(human, "\n") {
			cells := zoneTableGap.Split(strings.TrimSpace(line), -1)
			if len(cells) == 7 && cells[0] == version && cells[6] == schemaSize(release.HasSchema, release.SchemaBytes) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("release %s: incorrect version marker or schema size", release.Version)
		}
	}
	return nil
}

func TestAddonReadValuesInBothLanguages(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		for _, tc := range []struct{ field, label string }{
			{",\"billable\":true", i18n.T("Billable: yes", "Платный: да")},
			{",\"billable\":false", i18n.T("Billable: no", "Платный: нет")},
			{"", ""},
		} {
			var buf strings.Builder
			raw := `{"addon":"test"` + tc.field + `,"releases":[{"version":"v1","latest":true,"has_schema":true,"schema_bytes":42},{"version":"v2","latest":false,"has_schema":false}]}`
			if err := renderAddonVersions(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
				t.Fatal(err)
			}
			text := buf.String()
			if tc.label == "" && strings.Contains(text, i18n.T("Billable:", "Платный:")) || tc.label != "" && !strings.Contains(text, tc.label+"\n") {
				t.Fatal(text)
			}
			var a addonVersionsAnswer
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				t.Fatal(err)
			}
			if err := checkAddonReleaseCells(a, text); err != nil {
				t.Fatal(err)
			}
			for _, wrong := range []string{strings.Replace(text, "v1 *", "v1", 1), strings.Replace(text, i18n.T("42 B", "42 Б"), "—", 1)} {
				if checkAddonReleaseCells(a, wrong) == nil {
					t.Fatal("lost release information", wrong)
				}
			}
		}
		for _, categories := range []string{`,"categories":["network","security"]`, ""} {
			var buf strings.Builder
			raw := `{"addons":[{"name":"addon","categories":["network"]}]` + categories + `}`
			if err := renderAddonList(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
				t.Fatal(err)
			}
			want := i18n.T("Categories (--category): network, security.", "Категории (--category): network, security.")
			if strings.Contains(buf.String(), want) != (categories != "") {
				t.Fatal(buf.String())
			}
		}
	}
}

func TestRead_SelectedFieldsInBothLanguages(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		cmds := commandsByTool()
		statusView := func(p *output.Printer, _ io.Writer, text string) error { return showStatus(p, text, false) }
		for _, tc := range []struct {
			view            renderFunc
			present, absent string
			want            []string
		}{
			{cmds["list_flavors"].cmd.show, `{"flavors":[{"name":"a","zones":["az-red","az-blue"]},{"name":"b","zones":["az-green"]}]}`, `{"flavors":[{"name":"a"}]}`, []string{"az-red,az-blue", "az-green"}},
			{cmds["pg_specs"].cmd.show, `{"specifications":[{"id":"a","min_storage_gb":173},{"id":"b","min_storage_gb":349}]}`, `{"specifications":[{"id":"a"}]}`, []string{"173", "349"}},
			{renderProjectList, `{"active":"kept-project","projects":[]}`, `{"projects":[]}`, []string{i18n.T("Active project: kept-project", "Активный проект: kept-project")}},
			{statusView, `{"id":"d","project_id":"project-blue","ssh_user":"user-green"}`, `{"id":"d"}`, []string{"project-blue", "user-green"}},
			{renderImageCard, `{"image":{"id":"img","name":"img"},"fields":[{"name":"ssh","field_type":"key","required":true,"mandatory_group":"auth-entry"}]}`, `{"image":{"id":"img","name":"img"},"fields":[{"name":"ssh","required":true}]}`, []string{i18n.T("group auth-entry", "группа auth-entry")}},
		} {
			for _, raw := range []string{tc.present, tc.absent} {
				var buf strings.Builder
				if err := tc.view(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
					t.Fatal(err)
				}
				for _, want := range tc.want {
					if strings.Contains(buf.String(), want) != (raw == tc.present) {
						t.Fatalf("%s %s: expected field %q presence=%v in %q", lang, raw, want, raw == tc.present, buf.String())
					}
				}
			}
		}
	}
}

// The VAT map describes the wire amounts. A person reads VAT-inclusive
// service and growth rows and both period totals (billing_views.go).
func checkBillingRead(tool string, answer map[string]any, human string) error {
	consumption := tool == "billing_consumption"
	totalKey, totalLabel := "total_cost", i18n.T("Costs of the period", "Расход за период")
	if consumption {
		totalKey, totalLabel = "total", i18n.T("Total", "Итого")
	}
	flat := strings.Join(strings.Fields(human), " ")
	pair := func(label string, inc, exc any) bool {
		a, oka := num(inc)
		b, okb := num(exc)
		return oka && okb && strings.Contains(flat, label+": "+withAndWithoutVAT(a, b))
	}
	if !pair(totalLabel, answer[totalKey+"_with_nds"], answer[totalKey]) {
		return fmt.Errorf("incorrect period totals or VAT labels")
	}
	if currency := answer["currency"]; currency != "RUB" {
		return fmt.Errorf("unexpected currency %v", currency)
	}
	if !consumption && !pair(i18n.T("Per day", "В день"), answer["daily_burn_with_nds"], answer["daily_burn"]) {
		return fmt.Errorf("incorrect daily totals")
	}
	meta, ok := answer["vat"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing VAT metadata")
	}
	expected := map[string]string{
		totalKey: "excluding VAT: sum of amount", totalKey + "_with_nds": "including VAT: sum of amount_nds",
		"by_service": "excluding VAT: amount", "by_service.cost_with_nds": "including VAT: amount_nds",
	}
	if !consumption {
		expected["daily_burn_with_nds"] = "including VAT: total_cost_with_nds per day"
		expected["anomalies.now_with_nds"] = "including VAT: amount_nds; the growth is found by the sums excluding VAT"
		expected["anomalies.prev_with_nds"] = "including VAT: amount_nds of the previous period"
	}
	for key, want := range expected {
		if meta[key] != want {
			return fmt.Errorf("VAT metadata for %s = %v, want %q", key, meta[key], want)
		}
	}
	for _, collection := range []string{"by_service", "anomalies"} {
		rows, _ := answer[collection].([]any)
		for _, item := range rows {
			row := item.(map[string]any)
			keys := []string{"cost_with_nds"}
			if collection == "anomalies" {
				keys = []string{"prev_with_nds", "now_with_nds"}
			}
			want := fmt.Sprint(row["service"])
			for _, key := range keys {
				value, ok := num(row[key])
				if !ok {
					return fmt.Errorf("%s lacks %s", want, key)
				}
				want += " " + rub(value)
			}
			if !strings.Contains(flat, want) {
				return fmt.Errorf("missing VAT amounts for %s", want)
			}
		}
	}
	if note, ok := answer["runway_note"]; ok {
		if note != "runway not computed: the balance is the agreement's, the costs are the project's" || !strings.Contains(flat, i18n.T("Runway: not computed: the balance is the agreement's, the costs are the project's", "Запас: не рассчитан: баланс — договора, расход — проекта")) {
			return fmt.Errorf("incorrect runway note")
		}
		delete(answer, "runway_note")
	}
	for key := range expected {
		delete(meta, key)
	}
	delete(answer, "currency")
	return nil
}

func TestBillingReadCurrencyAndVAT(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	const raw = `{"period":{"start":"2026-01-01","end":"2026-01-02"},"total":17,"total_with_nds":23,"currency":"RUB","vat":{"total":"excluding VAT: sum of amount","total_with_nds":"including VAT: sum of amount_nds","by_service":"excluding VAT: amount","by_service.cost_with_nds":"including VAT: amount_nds"},"by_service":[{"service":"db","cost":17,"cost_with_nds":23}]}`
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		var buf strings.Builder
		if err := renderBillingConsumption(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			input, render string
			ok            bool
		}{
			{raw, buf.String(), true}, {strings.Replace(raw, "RUB", "USD", 1), buf.String(), false},
			{strings.Replace(raw, "excluding VAT: sum", "including VAT: sum", 1), buf.String(), false},
			{raw, strings.ReplaceAll(buf.String(), "23 ₽", "17 ₽"), false},
			{strings.Replace(raw, `"currency":"RUB",`, "", 1), buf.String(), false},
		} {
			if err := checkBillingRead("billing_consumption", parseObject(tc.input), tc.render); (err == nil) != tc.ok {
				t.Fatalf("%s: %v", lang, err)
			}
		}
	}
}

func checkScrapeAnswer(a map[string]any, human, project, match string) error {
	text, ok := a["exposition"].(string)
	if !ok || a["format"] != "prometheus-text" || a["project"] != project || a["match"] != match {
		return fmt.Errorf("metrics_scrape: scope, filter, format or exposition mismatch")
	}
	want := i18n.T("No metrics.", "Метрик нет.") + "\n"
	if strings.TrimSpace(text) != "" {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		for i, line := range lines {
			lines[i] = output.SanitizeCell(line)
		}
		want = strings.Join(lines, "\n") + "\n"
	}
	if human != want {
		return fmt.Errorf("metrics_scrape: stdout differs from exposition")
	}
	for _, k := range []string{"format", "project", "match"} {
		delete(a, k)
	}
	return nil
}

func TestMetricsScopeAndFilterReachTheRequest(t *testing.T) {
	withJSONWorld(t)
	prev := httpretry.DefaultBase
	defer func() { httpretry.DefaultBase = prev }()
	var path, match string
	httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "monitoring.api.cloud.ru" {
			path, match = r.URL.Path, r.URL.Query().Get("match")
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("test_metric 7\n"))}, nil
		}
		return prev.RoundTrip(r)
	})
	tl, _ := newRegistry().Lookup("metrics_scrape")
	for _, filter := range []string{"", `{job="specific"}`} {
		raw, failed := tl.Handler(context.Background(), map[string]any{"match": filter})
		if failed {
			t.Fatal(raw)
		}
		want := filter
		if want == "" {
			want = `{__name__=~".+"}`
		}
		if path != "/v2/"+pvProject+"/scrape_metrics" || match != want {
			t.Fatal(path, match)
		}
		a := parseObject(raw)
		if err := checkScrapeAnswer(a, "test_metric 7\n", pvProject, want); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"project", "format", "match"} {
			a = parseObject(raw)
			delete(a, key)
			if checkScrapeAnswer(a, "test_metric 7\n", pvProject, want) == nil {
				t.Fatal(key)
			}
		}
		path, match = "", ""
	}
}

func checkMarketFlags(a map[string]any, human string) error {
	items, _ := a["items"].([]any)
	for _, item := range items {
		row := item.(map[string]any)
		want := []string{"—", "—"}
		for i, key := range []string{"available_in_project", "billable"} {
			if flag, ok := row[key].(bool); ok {
				want[i] = yesNo(flag)
			}
		}
		found := false
		for _, line := range strings.Split(human, "\n") {
			cells := zoneTableGap.Split(strings.TrimSpace(line), -1)
			n := len(cells)
			if n >= 4 && cells[0] == row["kind"] && cells[1] == row["name"] && cells[n-2] == want[0] && cells[n-1] == want[1] {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("marketplace: incorrect availability or price for %v", row["name"])
		}
		delete(row, "available_in_project")
		delete(row, "billable")
	}
	return nil
}

func TestMarketplaceFlagsFollowTheirRows(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	const raw = `{"items":[{"kind":"vm-os","name":"os","available_in_project":true},{"kind":"k8s-addon","name":"addon","billable":false}]}`
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		var buf strings.Builder
		if err := renderMarketplaceList(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
			t.Fatal(err)
		}
		if err := checkMarketFlags(parseObject(raw), buf.String()); err != nil {
			t.Fatal(err)
		}
		if checkMarketFlags(parseObject(strings.Replace(raw, "false", "true", 1)), buf.String()) == nil {
			t.Fatal("changed price was accepted")
		}
	}
}

func checkDoctorHTTP(a map[string]any, plain, verbose string) error {
	checks, _ := a["checks"].([]any)
	for _, item := range checks {
		row := item.(map[string]any)
		status, ok := row["http_status"].(float64)
		if !ok {
			continue
		}
		if status < 100 || status > 599 || row["status"] != "ok" {
			return fmt.Errorf("doctor: unexpected response %v", row)
		}
		detail, _ := row["detail"].(string)
		host, _, _ := strings.Cut(detail, " ")
		if host == "" {
			return fmt.Errorf("doctor: missing probe host")
		}
		reachable := host + i18n.T(" reachable (", " доступен (")
		if !strings.Contains(plain, reachable) || !strings.Contains(verbose, fmt.Sprintf("%sHTTP %.0f,", reachable, status)) {
			return fmt.Errorf("doctor: HTTP %.0f for %s absent from verbose probe", status, host)
		}
		delete(row, "http_status")
	}
	return nil
}

// Verify the actual command annotation, as well as the spelling printed
// to the terminal. A command for a different resource must not pass.
func checkDeployRoutes(root *cobra.Command, name string, a map[string]any, human string) error {
	check := func(obj map[string]any, key, command, context string) error {
		action, _ := obj[key].(map[string]any)
		if action == nil {
			return nil
		}
		cmd, args, err := root.Find(strings.Fields(command))
		if err != nil || len(args) != 0 || cmd.CommandPath() != "evo "+command || cmd.Annotations[toolAnnotation] != action["tool"] || !strings.Contains(context, "evo "+command+" ") {
			return fmt.Errorf("%s: %v lacks matching CLI route %q", name, action["tool"], command)
		}
		delete(action, "tool")
		return nil
	}
	switch name {
	case "image_get":
		if err := check(a, "create", "vm create", human); err != nil {
			return err
		}
		fields, _ := a["fields"].([]any)
		for _, entry := range fields {
			f := entry.(map[string]any)
			role, ok := f["role"].(string)
			if !ok {
				continue
			}
			label, ok := map[string]string{images.RoleKey: i18n.T("evo: the SSH key", "evo: ключ SSH"), images.RoleLogin: i18n.T("evo: the login", "evo: логин"), images.RoleHostname: i18n.T("evo: the VM name", "evo: имя ВМ"), images.RolePassword: i18n.T("evo: a random password", "evo: случайный пароль")}[role]
			found := false
			for _, line := range strings.Split(human, "\n") {
				cells := zoneTableGap.Split(strings.TrimSpace(line), -1)
				if len(cells) >= 2 && cells[0] == f["name"] && cells[len(cells)-1] == label {
					found = true
					break
				}
			}
			if !ok || !found {
				return fmt.Errorf("image field %v: missing role %s", f["name"], role)
			}
			delete(f, "role")
		}
	case "k8s_addon_versions":
		return check(a, "deploy_with", "k8s add-addon", human)
	case "k8s_addons":
		rows, _ := a["addons"].([]any)
		for _, v := range rows {
			row := v.(map[string]any)
			if err := check(row, "deploy_with", "k8s add-addon", human); err != nil {
				return err
			}
		}
	case "marketplace_list":
		items, _ := a["items"].([]any)
		for _, v := range items {
			row := v.(map[string]any)
			command := "vm create"
			if row["kind"] == "k8s-addon" {
				command = "k8s add-addon"
			}
			line := ""
			for _, l := range strings.Split(human, "\n") {
				cells := zoneTableGap.Split(strings.TrimSpace(l), -1)
				if len(cells) >= 2 && cells[0] == row["kind"] && cells[1] == row["name"] {
					line = l
					break
				}
			}
			if err := check(row, "deploy", command, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestDeploymentHintMustResolveToItsTool(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		root := newRootCmd()
		const raw = `{"image":{"name":"test-os","id":"img"},"create":{"tool":"provision"},"fields":[{"name":"ssh","field_type":"key","filled_by":"evo","role":"ssh_key"}]}`
		var buf strings.Builder
		if err := renderImageCard(output.NewWithWriter(&buf, output.ModePlain, false), &buf, raw); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			input string
			ok    bool
		}{{raw, true}, {strings.Replace(raw, "provision", "k8s_add_addon", 1), false}, {strings.Replace(raw, "ssh_key", "login", 1), false}} {
			if err := checkDeployRoutes(root, "image_get", parseObject(tc.input), buf.String()); (err == nil) != tc.ok {
				t.Fatalf("%s: %v", lang, err)
			}
		}
	}
}

func TestDoctorProbeMatchesHostInVerboseOutput(t *testing.T) {
	old := i18n.Lang()
	defer i18n.SetLang(old)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		host := "test.invalid" + i18n.T(" reachable (", " доступен (")
		raw := `{"checks":[{"status":"ok","detail":"test.invalid reachable","http_status":404}]}`
		for _, code := range []string{"404", "200"} {
			err := checkDoctorHTTP(parseObject(raw), host+"1ms)", host+"HTTP "+code+", 1ms)")
			if (err == nil) != (code == "404") {
				t.Fatal(code, err)
			}
		}
	}
}
