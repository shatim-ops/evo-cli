// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// allTreeGroups are the command trees the CLI builds from the registry's
// CLISpecs.
func allTreeGroups() []treeGroup { return treeGroupsOf(tools.New(nil)) }

// treeCmdOf is the command verb of the tree group.
func treeCmdOf(t *testing.T, group, verb string) treeCmd {
	t.Helper()
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if g.Name == group && c.Verb == verb {
				return c
			}
		}
	}
	t.Fatalf("no %s %s", group, verb)
	return treeCmd{}
}

// The trees must not drift from the registry: every command names an
// existing tool, its positional arguments are arguments of that tool, every
// required argument is reachable, and every tool of the tree groups has a
// command.
func TestTrees_MatchRegistry(t *testing.T) {
	reg := tools.New(nil)
	covered := map[string]bool{}
	for _, g := range allTreeGroups() {
		verbs := map[string]bool{}
		for _, c := range g.Cmds {
			t.Run(g.Name+" "+c.Verb, func(t *testing.T) {
				if verbs[c.Verb] {
					t.Fatalf("duplicate verb")
				}
				verbs[c.Verb] = true
				tl, ok := reg.Lookup(c.Tool)
				if !ok {
					t.Fatalf("no tool %q in the registry", c.Tool)
				}
				covered[c.Tool] = true
				schema := map[string]param{}
				for _, p := range schemaParams(tl) {
					schema[p.Name] = p
				}
				reachable := map[string]bool{}
				for _, p := range parsePositionals(c.Args) {
					for _, n := range p.names {
						if _, ok := schema[n]; !ok {
							t.Errorf("positional %q is not an argument of %s", n, c.Tool)
						}
						reachable[n] = true
					}
				}
				for _, p := range c.flagArgs(tl) {
					reachable[p.Name] = true
				}
				for arg := range c.Rename {
					if _, ok := schema[arg]; !ok {
						t.Errorf("rename of %q: not an argument of %s", arg, c.Tool)
					}
				}
				for _, p := range schema {
					if p.Required && !reachable[p.Name] {
						t.Errorf("required argument %q of %s has neither a positional nor a flag", p.Name, c.Tool)
					}
				}
			})
		}
	}
	for _, g := range []string{"vm", "disk", "vpc", "k8s", "pg", "s3", "secret", "dns", "cert", "nlb", "snat", "kafka", "redis", "ar", "ca",
		"billing", "audit", "iam", "monitoring", "stack"} {
		for _, tl := range reg.Tools() {
			if tl.Group == g && !covered[tl.Name] {
				t.Errorf("tool %s (group %s) has no command in the tree", tl.Name, g)
			}
		}
	}
}

// Building the root command defines every flag: a clash with a root flag
// panics here rather than for a user.
func TestTrees_BuildAndHelp(t *testing.T) {
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if _, _, code := execCLI(t, g.Name, c.Verb, "--help"); code != 0 {
				t.Errorf("%s %s --help: exit %d", g.Name, c.Verb, code)
			}
		}
	}
}

func TestTrees_ConfirmationAndUsage(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"s3", "bucket-create", "bk1"}, clerrors.ExitConfirmationRequired},
		{[]string{"s3", "bucket-create", "bk1", "--dry-run"}, clerrors.ExitOK},
		{[]string{"s3", "bucket-create", "bk1", "--yes"}, clerrors.ExitAuth}, // past the gate, no credentials
		// The preview names the VM, so it looks it up first: no project, no
		// question (release audit, R-04). So does disk delete's (D26).
		{[]string{"vm", "delete", "web"}, clerrors.ExitAuth},
		{[]string{"disk", "delete", "00000000-0000-4000-8000-000000000001"}, clerrors.ExitAuth},
		{[]string{"disk", "delete", "d-1"}, clerrors.ExitGeneral},                  // not a UUID: USAGE before the cloud
		{[]string{"disk", "create", "d1"}, clerrors.ExitGeneral},                   // --size-gb is required
		{[]string{"disk", "create", "d1", "--size-gb", "x"}, clerrors.ExitGeneral}, // not an int
		{[]string{"disk", "delete"}, clerrors.ExitGeneral},                         // no id
		{[]string{"disk", "delete", "a", "b"}, clerrors.ExitGeneral},               // too many
		{[]string{"vm", "list"}, clerrors.ExitAuth},
	}
	for _, c := range cases {
		if _, errOut, code := execCLI(t, c.args...); code != c.want {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.want, errOut)
		}
	}
	_, errOut, _ := execCLI(t, "disk", "create", "d1")
	if !strings.Contains(errOut, "--size-gb") {
		t.Errorf("missing flag not named: %s", errOut)
	}
}

// findLeaf returns the built command for "group verb".
func findLeaf(t *testing.T, group, verb string) (*cobra.Command, treeCmd, tools.Tool) {
	t.Helper()
	root := newRootCmd()
	cmd, _, err := root.Find([]string{group, verb})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if g.Name == group && c.Verb == verb {
				tl, _ := tools.New(nil).Lookup(c.Tool)
				return cmd, c, tl
			}
		}
	}
	t.Fatalf("no %s %s", group, verb)
	return nil, treeCmd{}, tools.Tool{}
}

func TestTreeArgs(t *testing.T) {
	parse := func(group, verb string, argv ...string) map[string]any {
		t.Helper()
		cmd, c, tl := findLeaf(t, group, verb)
		if err := cmd.ParseFlags(argv); err != nil {
			t.Fatal(err)
		}
		got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := parse("vm", "get", "web"); got["name"] != "web" || got["vm_id"] != nil {
		t.Errorf("vm get by name: %v", got)
	}
	id := "00000000-0000-4000-8000-0000000000a1"
	if got := parse("vm", "get", id); got["vm_id"] != id {
		t.Errorf("vm get by id: %v", got)
	}
	got := parse("disk", "create", "d1", "--size-gb", "20", "--attach-to", "web")
	if got["name"] != "d1" || got["size_gb"] != float64(20) || got["attach_to"] != "web" {
		t.Errorf("disk create: %v", got)
	}
	got = parse("k8s", "kubectl", "c-1", "get", "pods", "-n", "kube-system")
	if got["cluster_id"] != "c-1" || got["args"] != "get pods -n kube-system" {
		t.Errorf("kubectl: %v", got)
	}
	got = parse("k8s", "helm", "c-1", "install", "--set", "a=1", "--set", "b=2")
	if got["action"] != "install" || len(got["set"].([]any)) != 2 {
		t.Errorf("helm: %v", got)
	}
	got = parse("s3", "bucket-create", "b1", "--s3-region", "ru-central-1")
	if got["region"] != "ru-central-1" {
		t.Errorf("s3 region rename: %v", got)
	}
	if got := parse("vpc", "fip-release"); len(got) != 0 {
		t.Errorf("optional positional: %v", got)
	}
}

func TestRenderAnswer(t *testing.T) {
	render := func(text string, cols []string) string {
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModePlain, false)
		if err := renderAnswer(p, &buf, text, tableView{Columns: cols}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	got := render(`{"disks":[{"id":"d-1","name":"boot","size_gb":40,"zone":"ru.AZ-1","attached_to":["web"]}]}`,
		[]string{"id", "name", "size_gb", "attached_to"})
	if !strings.HasPrefix(got, "ID") || !strings.Contains(got, "d-1") || !strings.Contains(got, "40") || !strings.Contains(got, "web") ||
		strings.Contains(got, "ru.AZ-1") {
		t.Errorf("list with columns:\n%s", got)
	}

	got = render(`{"count":1,"clusters":[{"status":"RUNNING","id":"c-1","name":"pg","extra":{"name":"spec"}}]}`, nil)
	if first := strings.Fields(strings.SplitN(got, "\n", 2)[0]); strings.Join(first, " ") != "ID NAME STATUS EXTRA" {
		t.Errorf("auto columns: %q", first)
	}
	if !strings.Contains(got, "spec") {
		t.Errorf("nested object should show its name:\n%s", got)
	}

	if got := render(`{"clusters":[]}`, nil); got != "No items.\n" {
		t.Errorf("empty list: %q", got)
	}

	got = render(`{"id":"vm-1","name":"web","private_ip":"10.0.0.2","interfaces":[{"id":"i-1"}]}`, nil)
	if !strings.HasPrefix(got, "FIELD") || !strings.Contains(got, "private_ip") || !strings.Contains(got, `[{"id":"i-1"}]`) {
		t.Errorf("object:\n%s", got)
	}

	if got := render(`not json`, nil); got != "not json\n" {
		t.Errorf("not JSON: %q", got)
	}
}

// evo flags after <cluster-id> would go to kubectl; they are refused with
// the right order, except after "--" (a command inside a container).
func TestKubectlEvoFlagsAfterCluster(t *testing.T) {
	run := func(argv ...string) (map[string]any, error) {
		t.Helper()
		cmd, c, tl := findLeaf(t, "k8s", "kubectl")
		if err := cmd.ParseFlags(argv); err != nil {
			t.Fatal(err)
		}
		return treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
	}
	for _, f := range []string{"--json", "--yes", "-y", "--json=true"} {
		_, err := run("c-1", "get", "pods", f)
		var ce *clerrors.CLIError
		if !errors.As(err, &ce) || ce.Code != clerrors.CodeUsage || !strings.Contains(ce.Hint, "<cluster-id>") {
			t.Errorf("%s: err = %v, want USAGE with the right order", f, err)
		}
	}
	got, err := run("c-1", "exec", "p", "--", "apt-get", "install", "-y", "curl")
	if err != nil || got["args"] != "exec p -- apt-get install -y curl" {
		t.Errorf("after --: %v %v", got, err)
	}
	got, err = run("c-1", "apply", "-f", "x.yaml", "--dry-run=server")
	if err != nil || got["args"] != "apply -f x.yaml --dry-run=server" {
		t.Errorf("kubectl --dry-run passes through: %v %v", got, err)
	}
	cmd, _, _ := findLeaf(t, "k8s", "kubectl")
	if !strings.Contains(cmd.Long, "go before the arguments") {
		t.Error("help does not say where evo flags go")
	}
}

// k8s kubectl/apply/helm print what kubectl printed, not a FIELD/VALUE
// table with the output squeezed into one cell; a failure's output goes to
// stderr (k8s-smoke read the table and never found .items).
func TestPrintRaw(t *testing.T) {
	for _, verb := range []string{"kubectl", "apply", "helm"} {
		if _, c, _ := findLeaf(t, "k8s", verb); !c.Raw {
			t.Errorf("k8s %s: not Raw", verb)
		}
	}
	var out, errw bytes.Buffer
	pods := "NAMESPACE     NAME        READY\nkube-system   coredns-1   1/1\n"
	if !printRaw(&out, &errw, `{"status":"ok","output":"`+strings.ReplaceAll(pods, "\n", `\n`)+`"}`, nil) ||
		out.String() != pods || errw.Len() != 0 {
		t.Errorf("ok: stdout %q stderr %q", out.String(), errw.String())
	}
	out.Reset()
	fail := errors.New("k8s_kubectl: exit status 1")
	if !printRaw(&out, &errw, `{"status":"error","output":"Error from server (NotFound): pods \"x\" not found","error":"exit status 1"}`, fail) ||
		out.Len() != 0 || errw.String() != "Error from server (NotFound): pods \"x\" not found\n" {
		t.Errorf("error: stdout %q stderr %q", out.String(), errw.String())
	}
	if printRaw(&out, &errw, `{"error":"API_ERROR: no cluster"}`, fail) || printRaw(&out, &errw, "", nil) {
		t.Error("an answer without output is printed raw")
	}
}

// A list with a note (list_flavors) is a table with the note under it; a
// composite answer without id/name (k8s_options) shows its lists of objects
// as tables of their own (BACKLOG [audit-final]).
func TestRenderAnswer_NotesAndSections(t *testing.T) {
	render := func(text string) string {
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModePlain, false)
		if err := renderAnswer(p, &buf, text, tableView{}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	got := render(`{"flavors":[{"name":"low-1-2","cpu":1,"ram":2}],"note":"rate-card estimate"}`)
	if !strings.HasPrefix(got, "NAME") || !strings.Contains(got, "low-1-2") ||
		!strings.HasSuffix(got, "note: rate-card estimate\n") || strings.Contains(got, `[{`) {
		t.Errorf("list with note:\n%s", got)
	}

	got = render(`{"flavors":[{"name":"m-2-4","cpu":2,"ram":4}],"zones":["ru.AZ-1","ru.AZ-2"],"defaults":{"zone":"ru.AZ-1"}}`)
	if !strings.HasPrefix(got, "FIELD") || !strings.Contains(got, "ru.AZ-1,ru.AZ-2") ||
		!strings.Contains(got, "\nFLAVORS:\n") || !strings.Contains(got, "m-2-4") || strings.Contains(got, `[{`) {
		t.Errorf("composite answer:\n%s", got)
	}

	// Only lists: no empty FIELD/VALUE table and no leading blank line.
	got = render(`{"flavors":[{"name":"a"}],"zones":[{"name":"z"}]}`)
	if !strings.HasPrefix(got, "FLAVORS:\n") || !strings.Contains(got, "\n\nZONES:\n") {
		t.Errorf("lists only:\n%q", got)
	}

	// Other string fields keep the FIELD/VALUE form.
	got = render(`{"status":"creating","items":[{"id":"x"}]}`)
	if !strings.HasPrefix(got, "FIELD") {
		t.Errorf("status + list is not a plain list:\n%s", got)
	}
}

// С --json ошибка инструмента — только {"error":…} в stderr: тело ответа
// инструмента в stdout было бы вторым, лишним объектом ошибки для jq.
// Превью подтверждения и --dry-run по-прежнему идут в stdout.
func TestTrees_JSONToolErrorNotOnStdout(t *testing.T) {
	setupCLI(t)
	cases := []struct {
		args   []string
		code   int
		stdout string // "" — stdout must be empty, else the status it must carry
	}{
		{[]string{"vm", "list", "--json"}, clerrors.ExitAuth, ""},
		{[]string{"s3", "bucket-create", "bk1", "--json"}, clerrors.ExitConfirmationRequired, "confirmation_required"},
		{[]string{"s3", "bucket-create", "bk1", "--dry-run", "--json"}, clerrors.ExitOK, "dry_run"},
	}
	for _, c := range cases {
		newProcess()
		code, stdout, stderr := runCLI(t, "", c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.code, stderr)
		}
		if c.stdout == "" {
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("%v: stdout %q, want empty", c.args, stdout)
			}
			if !strings.Contains(stderr, `"error"`) {
				t.Errorf("%v: no JSON error on stderr: %q", c.args, stderr)
			}
			continue
		}
		if !strings.Contains(stdout, `"status": "`+c.stdout+`"`) && !strings.Contains(stdout, `"status":"`+c.stdout+`"`) {
			t.Errorf("%v: stdout %q, want status %s", c.args, stdout, c.stdout)
		}
	}
}

// A boolean argument the tool takes as true when absent is a flag that
// defaults to true: the help says so, --x=false reaches the tool as false,
// and an unset flag is not passed at all (the tool's own default applies).
func TestTrees_BoolDefaultTrue(t *testing.T) {
	want := map[string]bool{ // "group verb --flag"
		"vm delete --release-ip":             true,
		"k8s plan --kube-api-internet":       true,
		"k8s create --kube-api-internet":     true,
		"k8s kubeconfig --embed-token":       true,
		"secret delete --force":              true,
		"lb create --external":               true,
		"ca deploy --public":                 true,
		"vpc subnet-create --routed-network": true,
	}
	reg := tools.New(nil)
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			tl, _ := reg.Lookup(c.Tool)
			cmd, _, _ := findLeaf(t, g.Name, c.Verb)
			for _, p := range c.flagArgs(tl) {
				if p.Type != "boolean" {
					continue
				}
				key := g.Name + " " + c.Verb + " --" + c.flagName(p.Name)
				f := cmd.Flags().Lookup(c.flagName(p.Name))
				on := f.DefValue == "true"
				if on != want[key] {
					t.Errorf("%s: flag default %s, want true: %v", key, f.DefValue, want[key])
				}
				if on != (p.Default == true) {
					t.Errorf("%s: flag default %s, schema default %v", key, f.DefValue, p.Default)
				}
				if on && (!strings.Contains(f.Usage, "--"+f.Name+"=false") || strings.Contains(f.Usage, "default")) {
					t.Errorf("%s: usage %q", key, f.Usage)
				}
				delete(want, key)
			}
		}
	}
	for key := range want {
		t.Errorf("%s: no such boolean flag", key)
	}

	parse := func(argv ...string) map[string]any {
		t.Helper()
		cmd, c, tl := findLeaf(t, "vm", "delete")
		if err := cmd.ParseFlags(argv); err != nil {
			t.Fatal(err)
		}
		got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := parse("web", "--release-ip=false"); got["release_ip"] != false {
		t.Errorf("--release-ip=false: %v", got)
	}
	if got := parse("web", "--release-ip"); got["release_ip"] != true {
		t.Errorf("--release-ip: %v", got)
	}
	if got := parse("web"); len(got) != 1 || got["name"] != "web" {
		t.Errorf("unset --release-ip is passed: %v", got)
	}

	out, _, code := execCLI(t, "vm", "delete", "--help")
	if code != 0 || !strings.Contains(out, "--release-ip   Also release the VM's floating IP. To turn off: --release-ip=false (default true)\n") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// The vm tree speaks Russian in its human tables: headers, да/нет and the
// price note; flavors go name, vCPU, RAM, price, free tier. --json keeps
// the tool's keys (first real run on macOS, v1.0.1).
func TestRenderAnswer_RussianTree(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	flavors := treeCmdOf(t, "vm", "flavors")
	text := `{"flavors":[{"name":"gen-1-1","vcpus":1,"ram_gb":1,"free_tier":false,"est_monthly_rub":512.3},` +
		`{"name":"free-1-1","vcpus":1,"ram_gb":1,"free_tier":true,"est_monthly_rub":0}],` +
		`"note":` + strconv.Quote(tools.FlavorsNote) + `,"tariff_url":"https://example.org/tariff"}`
	var buf bytes.Buffer
	p := output.NewWithWriter(&buf, output.ModePlain, false)
	if err := flavors.show(p, &buf, text); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(buf.String(), "\n")
	if h := strings.Join(strings.Fields(lines[0]), " "); h != "Имя vCPU Доля vCPU RAM, ГБ GPU Spot ≈ руб./мес Бесплатный уровень" {
		t.Errorf("header: %q", h)
	}
	if r := strings.Fields(lines[1]); strings.Join(r, " ") != "gen-1-1 1 1 512.3 нет" {
		t.Errorf("row: %q", r)
	}
	if !strings.Contains(lines[2], "да") || strings.Contains(buf.String(), "true") || strings.Contains(buf.String(), "false") {
		t.Errorf("booleans:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "оценка по тарифам, с диском SSD 10 ГБ; это не счёт") || strings.Contains(buf.String(), "est_monthly_rub") {
		t.Errorf("note:\n%s", buf.String())
	}
	// С5: the legend of the share under the table, a quote of the
	// specification, not evo's arithmetic.
	if !strings.Contains(buf.String(), "1:1 — 100 %, 1:3 — 30 %, 1:10 — 10 % (спецификация Cloud.ru)") {
		t.Errorf("legend of the vCPU share:\n%s", buf.String())
	}
	// Ц-5: the tariff of the true rates of a shared vCPU, under the table.
	if !strings.Contains(buf.String(), "Тарифы: https://example.org/tariff") {
		t.Errorf("tariff_url:\n%s", buf.String())
	}

	// A table without Russian titles keeps the keys of the tool, in
	// capitals; its booleans are да/нет, as in every Russian table.
	buf.Reset()
	if err := renderAnswer(p, &buf, `{"items":[{"id":"x","enabled":true}]}`, tableView{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "ID") || !strings.Contains(buf.String(), "ENABLED") || !strings.Contains(buf.String(), "да") {
		t.Errorf("table without titles:\n%s", buf.String())
	}
}

// The tables of the trees of v1.1 (image, vpc, k8s, pg, s3, secret) title
// their columns in Russian when the CLI speaks Russian, from the titles of
// the other trees, and say да/нет (owner's decision Q11). In English a
// generic table is the one without Russian titles, byte for byte, and every
// tree has its titles.
func TestRenderAnswer_TreesOfV11(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	show := func(c treeCmd, text string, view *tableView) string {
		t.Helper()
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModePlain, false)
		var err error
		if view != nil {
			err = renderAnswer(p, &buf, text, *view)
		} else {
			err = c.show(p, &buf, text)
		}
		if err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	line := func(out string, i int) string { return strings.Join(strings.Fields(strings.Split(out, "\n")[i]), " ") }
	for _, tc := range []struct {
		group, verb, text string
		ru, en            string // the titles
		row               string // the first row in Russian; "" when it is not checked
	}{
		{"image", "list", `{"images":[{"id":"i-1","name":"ubuntu-22.04","display_name":"Ubuntu 22.04","type":"os","zones_ready":["ru.AZ-1"],"min_disk_gb":10}],"shown":1,"total":1}`,
			"ИМЯ ОТОБРАЖАЕМОЕ ИМЯ ТИП ЗОНЫ МИН. ДИСК", "NAME DISPLAY NAME TYPE ZONES MIN DISK", ""},
		{"vpc", "list", `{"vpcs":[{"id":"vpc-1","name":"main","default":true,"type":"standard"}]}`,
			"ID Имя По умолчанию Тип", "ID NAME DEFAULT TYPE", "vpc-1 main да standard"},
		{"vpc", "fips", `{"floating_ips":[{"id":"f-1","name":"fip","ip_address":"192.0.2.10","state":"DOWN","available":true,"nat_gateway":false}]}`,
			"ID Имя Состояние Свободен IP-адрес На NAT-шлюзе", "ID NAME STATE AVAILABLE IP_ADDRESS NAT_GATEWAY", "f-1 fip DOWN да 192.0.2.10 нет"},
		{"k8s", "nodepools", `{"node_pools":[{"id":"np-1","name":"workers","status":"RUNNING","version":"1.30","desired":3,"flavor":"gen-2-4"}]}`,
			"ID Имя Состояние Версия Узлов Flavor", "ID NAME STATUS VERSION DESIRED FLAVOR", "np-1 workers RUNNING 1.30 3 gen-2-4"},
		{"pg", "list", `{"clusters":[{"id":"pg-1","name":"db","status":"RUNNING","version":"16","health":"HEALTHY"}]}`,
			"ID Имя Состояние Версия Работоспособность", "ID NAME STATUS VERSION HEALTH", "pg-1 db RUNNING 16 HEALTHY"},
		{"s3", "buckets", `{"buckets":[{"name":"assets","created":"2026-01-02T03:04:05Z"}]}`,
			"Имя Создан", "NAME CREATED", "assets 2026-01-02T03:04:05Z"},
		{"secret", "list", `{"secrets":[{"id":"s-1","name":"db","path":"app/db","description":"password of the app"}]}`,
			"ID Имя Путь Описание", "ID NAME PATH DESCRIPTION", "s-1 db app/db password of the app"},
	} {
		c := treeCmdOf(t, tc.group, tc.verb)
		i18n.SetLang(i18n.RU)
		ru := show(c, tc.text, nil)
		if h := line(ru, 0); h != tc.ru {
			t.Errorf("%s %s in Russian: titles %q, want %q", tc.group, tc.verb, h, tc.ru)
		}
		if r := line(ru, 1); tc.row != "" && r != tc.row {
			t.Errorf("%s %s in Russian: row %q, want %q", tc.group, tc.verb, r, tc.row)
		}
		i18n.SetLang(i18n.EN)
		en := show(c, tc.text, nil)
		if h := line(en, 0); h != tc.en {
			t.Errorf("%s %s in English: titles %q, want %q", tc.group, tc.verb, h, tc.en)
		}
		if c.render == nil {
			if plain := show(c, tc.text, &tableView{Columns: c.Columns}); en != plain {
				t.Errorf("%s %s in English is not the table without titles:\n%s\nwant:\n%s", tc.group, tc.verb, en, plain)
			}
		}
	}
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if c.headers == nil {
				t.Errorf("%s %s: no Russian titles (treeHeadersRU)", g.Name, c.Verb)
			}
		}
	}
}

// Release plan 1.6, «Линия CLI»: a reference whose thing the answer names
// ("<x>_name" beside "<x>_id") reads «name (id)» in a table and in a
// FIELD/VALUE table, and its name is not a row of its own.
func TestRenderAnswer_ReferenceByNameAndID(t *testing.T) {
	var buf bytes.Buffer
	p := output.NewWithWriter(&buf, output.ModePlain, false)
	list := `{"subnets":[{"id":"s1","name":"web","availability_zone_id":"z1","availability_zone_name":"ru.AZ-1"}]}`
	if err := renderAnswer(p, &buf, list, tableView{Columns: []string{"id", "name", "availability_zone_id"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ru.AZ-1 (z1)") {
		t.Errorf("list:\n%s", buf.String())
	}
	buf.Reset()
	obj := `{"id":"s1","name":"web","availability_zone_id":"z1","availability_zone_name":"ru.AZ-1"}`
	if err := renderAnswer(p, &buf, obj, tableView{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ru.AZ-1 (z1)") || strings.Contains(buf.String(), "availability_zone_name") {
		t.Errorf("object:\n%s", buf.String())
	}
}
