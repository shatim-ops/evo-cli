// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// previewForm are the keys of a preview's details that are its form, not
// a value the person agrees to: the human preview says them in its own
// words (the summary, «cost: not estimated…»), or they name a field.
var previewForm = map[string]string{
	"message":   "the summary line says it in the language of the CLI",
	"status":    "confirmation_required / dry_run: the prompt or «Dry run» says it",
	"resource":  "the type of the resource: the summary names it in words",
	"key":       "the name of a params entry: the line's label",
	"cost_note": "«cost: not estimated by evo…» says it in words",
	"source":    "where the cost comes from: «estimate» on the cost line",
}

// humanUnnamed are the values of a preview's details that its human form
// does not show — "tool path" with the class (as bodyUnnamed) and why. The
// list only shrinks: the human preview shows the value and the line goes
// (С2; the line of CLI in 1.6 builds the lines from the details in one
// function, D). A line the human preview shows now fails the test.
var humanUnnamed = map[string]struct{ class, why string }{
	"deploy preview.project_from":                     {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"deploy preview.security_group.id":                {unnamedNoise, "id того, что превью называет по имени"},
	"deploy preview.subnet_id":                        {unnamedNoise, "id того, что превью называет по имени"},
	"deploy preview.vm.disk.type_id":                  {unnamedNoise, "id того, что превью называет по имени"},
	"deploy preview.vm.flavor.id":                     {unnamedNoise, "id того, что превью называет по имени"},
	"deploy project_from":                             {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"deploy security_group.id":                        {unnamedNoise, "id того, что превью называет по имени"},
	"deploy subnet_id":                                {unnamedNoise, "id того, что превью называет по имени"},
	"deploy update":                                   {unnamedWork, "обновит ли deploy существующую ВМ"},
	"deploy vm.disk.type_id":                          {unnamedNoise, "id того, что превью называет по имени"},
	"deploy vm.flavor.id":                             {unnamedNoise, "id того, что превью называет по имени"},
	"deploy will_deploy.compose_file":                 {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy will_deploy.disk_size":                    {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy will_deploy.flavor":                       {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy will_deploy.force":                        {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy will_deploy.image_id":                     {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy will_deploy.region":                       {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"deploy_apply expires_at":                         {unnamedNoise, "срок плана"},
	"deploy_apply plan_id":                            {unnamedNoise, "id того, что превью называет по имени"},
	"deploy_apply preview.cost.hourly_rub":            {unnamedPrice, "почасовая цена; человеку — месячная"},
	"deploy_apply preview.vm.disk.type_id":            {unnamedNoise, "id того, что превью называет по имени"},
	"deploy_apply preview.vm.flavor.id":               {unnamedNoise, "id того, что превью называет по имени"},
	"disk_create preview.project_from":                {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"disk_resize preview.project_from":                {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"image_create will_create.has_user_data_template": {unnamedWork, "есть ли шаблон cloud-init"},
	"k8s_add_addon preview.project_from":              {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"k8s_add_addon version_from":                      {unnamedWork, "версия плагина — последняя"},
	"k8s_add_nodepool preview.project_from":           {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"k8s_create preview.project_from":                 {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"kafka_create will_create.specification_id":       {unnamedNoise, "id того, что превью называет по имени"},
	"pg_create preview.project_from":                  {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"pg_create will_create.specification_id":          {unnamedNoise, "спецификация, которую превью называет параметрами"},
	"pg_create will_create.zone":                      {unnamedWork, "зона кластера"},
	"provision preview.cost.hourly_rub":               {unnamedPrice, "почасовая цена; человеку — месячная"},
	"provision preview.project_from":                  {unnamedWork, "откуда проект: из окружения, профиля или аргумента"},
	"provision preview.vm.flavor.id":                  {unnamedNoise, "id того, что превью называет по имени"},
	"provision will_create.disk_size":                 {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"provision will_create.flavor":                    {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"provision will_create.image_id":                  {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"provision will_create.region":                    {unnamedNoise, "«по умолчанию: окружение, профиль или автоподбор» — превью называет выбранное"},
	"provision will_create.subnet_id":                 {unnamedNoise, "id того, что превью называет по имени"},
	"redis_create will_create.specification_id":       {unnamedNoise, "id того, что превью называет по имени"},
}

// Release plan 1.5, С2: what the agent sees in a preview (preview.details,
// the same in MCP and --json), the person at the terminal sees too — the
// lines of a preview are written by hand beside the details, and nothing
// made one follow the other. For the command of every mutating tool on the
// world of TestParity_JSONEqualsTool: every value of the details (a string
// or a number; true/false by the label of its field) is in the human
// preview of --dry-run, at a terminal 40 columns wide and 200 wide alike
// (nothing cut), or is a form key (previewForm) or a line of humanUnnamed.
// Off a terminal the human preview has no ANSI escape; at a terminal the
// whole preview comes before the question, in stderr, and nothing in
// stdout.
func TestPreview_HumanShowsEveryValue(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	tools, seen := map[string]bool{}, map[string]bool{}
	var bad []string
	byClass := map[string]int{}
	for _, c := range cases {
		if !c.tool.Mutating || tools[c.tool.Name] || c.state != "" {
			continue
		}
		tools[c.tool.Name] = true
		run := func(stdin string, extra ...string) (int, string, string) {
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
			code, out, errOut := runCLI(t, stdin, append(append([]string{}, extra...), line...)...)
			done()
			_ = os.Remove(state.DefaultPath(w.dir))
			return code, out, errOut
		}
		_, jsonOut, _ := run(c.stdin, "--dry-run", "--json")
		jsonOut = humanVolatile(jsonOut)
		var env struct {
			Preview struct {
				Summary string `json:"summary"`
				Details any    `json:"details"`
			} `json:"preview"`
		}
		if json.Unmarshal([]byte(jsonOut), &env) != nil || env.Preview.Details == nil {
			bad = append(bad, c.tool.Name+": no preview.details from --dry-run --json")
			continue
		}
		for _, cols := range []string{"40", "200"} {
			t.Setenv("COLUMNS", cols)
			_, out, errOut := run(c.stdin, "--dry-run")
			human := humanVolatile(out + errOut)
			if ansi.MatchString(human) {
				bad = append(bad, c.tool.Name+": an ANSI escape in the preview off a terminal (COLUMNS="+cols+")")
			}
			for _, miss := range humanMisses(env.Preview.Details, human) {
				key := c.tool.Name + " " + miss.path
				if d, ok := humanUnnamed[key]; ok {
					if !seen[key] {
						byClass[d.class]++
					}
					seen[key] = true
					continue
				}
				bad = append(bad, fmt.Sprintf("%s = %s: in the details, not in the human preview (COLUMNS=%s) — show it, or a line of humanUnnamed with its class", key, miss.value, cols))
			}
		}
		// At a terminal: the preview whole, then the question, in stderr.
		oldTTY, oldIn := confirm.StdinIsTTY, confirm.Stdin
		confirm.StdinIsTTY, confirm.Stdin = func() bool { return true }, strings.NewReader("n\n")
		_, out, errOut := run(c.stdin)
		confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldIn
		errOut = humanVolatile(errOut)
		q := strings.LastIndex(errOut, strings.TrimSpace(confirm.Prompt()))
		switch {
		case q < 0:
			bad = append(bad, c.tool.Name+": at a terminal, no question in stderr")
		case !strings.Contains(errOut[:q], env.Preview.Summary):
			bad = append(bad, c.tool.Name+": at a terminal, the question comes before the preview")
		case strings.TrimSpace(out) != "" && !isEngineCommand(c.tool.Name):
			bad = append(bad, c.tool.Name+": at a terminal, the preview or a result in stdout before the answer: "+clipLine(out))
		}
	}
	for _, tl := range reg.Tools() {
		if tl.Mutating && !tools[tl.Name] {
			bad = append(bad, tl.Name+": a mutating tool without a case in parityArgs")
		}
	}
	for key, d := range humanUnnamed {
		if !seen[key] {
			bad = append(bad, key+": in humanUnnamed, but the human preview shows it: remove the line")
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
	t.Logf("mutating tools: %d; values the person does not see, by class: %v", len(tools), byClass)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

var ansi = regexp.MustCompile("\x1b\\[")

// humanVolatile reads as one the ids each call makes anew: a job of a
// stack gets a new name in each run, as a plan does (sameVolatile).
func humanVolatile(s string) string {
	return stackJob.ReplaceAllString(sameVolatile(s), "stk-<id>")
}

var stackJob = regexp.MustCompile(`stk-[0-9a-f]{6,}`)

// isEngineCommand: an engine command prints its steps to stdout as it
// always did (v1.0); its preview and question are in stderr as anywhere.
func isEngineCommand(tool string) bool {
	for _, ec := range engineCommands {
		if ec.Tool == tool {
			return true
		}
	}
	return false
}

type humanMiss struct{ path, value string }

// humanMisses are the values of details the human text does not show: a
// string or a number not in it, true/false whose label (the key, in any of
// its forms, or its engine.ParamLabel) is not on any line. A params entry {key, value} counts as the
// value of its key. Form keys (previewForm) are skipped.
func humanMisses(details any, human string) []humanMiss {
	flat := normKey(strings.Join(strings.Fields(human), ""))
	var out []humanMiss
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch x := v.(type) {
		case map[string]any:
			if k, ok := x["key"].(string); ok {
				if val, ok := x["value"]; ok && len(x) == 2 {
					walk(val, path+"."+k)
					return
				}
			}
			for k, e := range x {
				if _, form := previewForm[k]; form {
					continue
				}
				p := k
				if path != "" {
					p = path + "." + k
				}
				walk(e, p)
			}
		case []any:
			for _, e := range x {
				walk(e, path)
			}
		case bool:
			// The label is the key, or the words the preview shows for it
			// (engine.ParamLabel: «API извне» for kube_api_internet).
			label := path[strings.LastIndex(path, ".")+1:]
			shown := normKey(strings.Join(strings.Fields(engine.ParamLabel(label)), ""))
			if !strings.Contains(flat, normKey(label)) && !strings.Contains(flat, shown) {
				out = append(out, humanMiss{path, fmt.Sprint(x)})
			}
		case nil:
		default:
			s := fmt.Sprint(x)
			if s != "" && !strings.Contains(human, s) {
				out = append(out, humanMiss{path, s})
			}
		}
	}
	walk(details, "")
	return out
}

func TestHumanMisses_ParamsBoolsAndForm(t *testing.T) {
	details := map[string]any{
		"message": "not shown, a form key",
		"preview": map[string]any{
			"params": []any{
				map[string]any{"key": "min_instances", "value": 0},
				map[string]any{"key": "public", "value": true},
				map[string]any{"key": "zone", "value": "ru.AZ-1"},
			},
		},
		"kube_api_internet": true,
	}
	human := "  min-instances: 0\n  public:    yes\n"
	misses := humanMisses(details, human)
	sort.Slice(misses, func(i, j int) bool { return misses[i].path < misses[j].path })
	// min_instances and public are shown (the label in another form);
	// zone and kube_api_internet are not; message is the form.
	if got, want := fmt.Sprint(misses), "[{kube_api_internet true} {preview.params.zone ru.AZ-1}]"; got != want {
		t.Errorf("humanMisses = %s, want %s", got, want)
	}
	// A flag shown under the words of its label, not its key, is shown.
	human = "  " + engine.ParamLabel("kube_api_internet") + ": yes\n"
	if misses := humanMisses(map[string]any{"kube_api_internet": true}, human); len(misses) != 0 {
		t.Errorf("humanMisses = %v under the label %q", misses, engine.ParamLabel("kube_api_internet"))
	}
}
