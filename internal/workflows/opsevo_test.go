// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// opsEvoOps are the operations the area opsevo and the area k8sevo of
// cloud-live.sh pass through the code of evo (release plan 1.9, the area
// ops-evo, gate of rc.1 p. 7).
var opsEvoOps = []string{"vm_stop", "vm_start", "vm_reboot", "vm_resize", "vm_console",
	"sg_rule_create", "sg_rule_delete", "backup_create", "backup_list", "backup_restore", "backup_delete",
	"k8s_nodepool_scale", "k8s_nodepool_delete"}

// opsEvoSection is the text of cloud-live.sh from the area opsevo to the
// dispatch of the areas: its helpers, area_opsevo and area_k8sevo.
func opsEvoSection(t *testing.T) (live, section string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	live = string(b)
	i := strings.Index(live, "\n# --- Область opsevo")
	j := strings.Index(live, "\ncase \"$area\" in")
	if i < 0 || j < i {
		t.Fatal("cloud-live.sh: no section of the area opsevo before the dispatch — the area moved, update the test")
	}
	return live, live[i:j]
}

// The area opsevo runs the day-two operations through evo, not by raw
// requests (the probes Г-10…Г-16 of the area ops do that): every
// operation of the plan line goes through the CLI command and through MCP,
// the CLI command is the one CLISpec gives the tool, and the only raw
// write is the create of the area's own security group (evo has no tool
// for it). The area is started by its word in live-plan, the cleanup of
// the area knows every kind it creates, and the part of Kubernetes runs
// only in the job k8s-smoke, by its own word, within a step limit that
// leaves the delete of the cluster its time.
func TestWorkflows_OpsEvoArea(t *testing.T) {
	live, sec := opsEvoSection(t)

	// Every operation goes through op — the CLI command or MCP; which
	// surface makes and which repeats each, in what order, the run of the
	// script on a fake evo pins (cloud_live_ops_test.sh, «поверхности по
	// порядку»). A line of a list of steps names the surface and the tool.
	for _, tool := range opsEvoOps {
		via := regexp.MustCompile(`(?m)^\s*(op "?(\$how|\$1|cli|mcp)"?|vm_step (cli|mcp)) ` + tool + `\b|"(cli|mcp) ` + tool + ` `)
		if !via.MatchString(sec) {
			t.Errorf("cloud-live.sh: the area opsevo does not run %s through op", tool)
		}
	}
	// The repeat is checked: a repeated delete answers already_deleted
	// (D26), a repeated create already_exists.
	for _, want := range []string{"already_deleted", "already_exists", "already_stopped", "already_running", "already_resized", "already_scaled"} {
		if !strings.Contains(sec, want) {
			t.Errorf("cloud-live.sh: the area opsevo does not check the repeat answer %s", want)
		}
	}

	// The CLI command of a tool is the one of its CLISpec.
	reg := tools.New(nil)
	spec := map[string]tools.CLISpec{}
	for _, tl := range reg.Tools() {
		spec[tl.Name] = tl.CLI
	}
	i := strings.Index(sec, "\nevo_cmd() {")
	if i < 0 {
		t.Fatal("cloud-live.sh: no evo_cmd — update the test")
	}
	table := sec[i:]
	table = table[:strings.Index(table, "\n}\n")]
	mapped := map[string]bool{}
	line := regexp.MustCompile(`(?m)^\s*([a-z0-9_ |]+)\) echo "([^"]+)" ;;`)
	for _, m := range line.FindAllStringSubmatch(table, -1) {
		for _, tool := range strings.Split(m[1], "|") {
			tool = strings.TrimSpace(tool)
			mapped[tool] = true
			s, ok := spec[tool]
			if !ok {
				t.Errorf("evo_cmd: %s is not a tool of the registry", tool)
				continue
			}
			// Expand the shell of the line for this tool: ${1#vm_},
			// ${1//_/-}, ${1#backup_}, ${1#k8s_nodepool_}.
			got := m[2]
			for _, p := range []string{"vm_", "backup_", "k8s_nodepool_"} {
				got = strings.ReplaceAll(got, "${1#"+p+"}", strings.TrimPrefix(tool, p))
			}
			got = strings.ReplaceAll(got, "${1//_/-}", strings.ReplaceAll(tool, "_", "-"))
			pos := "-"
			if len(s.Args) > 0 {
				pos = strings.SplitN(strings.TrimSuffix(s.Args[0], "?"), "|", 2)[0]
			}
			if want := s.Group + " " + s.Verb + " " + pos; got != want {
				t.Errorf("evo_cmd %s: %q, CLISpec gives %q", tool, got, want)
			}
		}
	}
	for _, tool := range opsEvoOps {
		if !mapped[tool] {
			t.Errorf("evo_cmd: no command for %s", tool)
		}
	}

	// No raw write but the create of the group: the operations are evo's.
	write := regexp.MustCompile(`\bapi (POST|PUT|PATCH|DELETE) (\S+)`)
	for _, m := range write.FindAllStringSubmatch(sec, -1) {
		if m[1] != "POST" || m[2] != `"$C1/security-groups"` {
			t.Errorf("cloud-live.sh: the area opsevo sends a raw %s %s — the operation must go through evo", m[1], m[2])
		}
	}

	// The cleanup: the kinds of the area are known to the sweep of
	// cleanup_only (KINDS) and hold every kind the area creates.
	kinds := regexp.MustCompile(`(?m)^KINDS="([^"]+)"`).FindStringSubmatch(live)
	area := regexp.MustCompile(`(?m)^\s*opsevo\) echo "([^"]+)"`).FindStringSubmatch(live)
	if kinds == nil || area == nil {
		t.Fatal("cloud-live.sh: no KINDS or no kinds of the area opsevo — update the test")
	}
	for _, k := range []string{"vm", "fip", "disk", "backup", "sg"} {
		if !strings.Contains(" "+area[1]+" ", " "+k+" ") {
			t.Errorf("area_kinds opsevo: no %s — what the area creates of it the cleanup would not find", k)
		}
	}
	for _, k := range strings.Fields(area[1]) {
		if !strings.Contains(" "+kinds[1]+" ", " "+k+" ") {
			t.Errorf("area_kinds opsevo: %s is not in KINDS — cleanup_only would not sweep it", k)
		}
	}
	dispatch := regexp.MustCompile(`(?m)^disk \| net \|[^\n]*\)$`).FindString(live)
	for _, a := range []string{"opsevo", "k8sevo"} {
		if !strings.Contains(dispatch+" ", " "+a+" ") && !strings.Contains(dispatch, " "+a+")") {
			t.Errorf("cloud-live.sh: the dispatch does not run the area %s", a)
		}
	}

	// The word opsevo starts the area in the matrix of the job live.
	wf := readWorkflows(t)["cloud-e2e.yml"]
	var plan string
	for _, s := range wf.Jobs["live-plan"].Steps {
		if s.ID == "areas" {
			plan = s.Run
		}
	}
	if !regexp.MustCompile(`(?m)^\s*opsevo\) a\+=\(opsevo\) ;;`).MatchString(plan) {
		t.Error("live-plan: the word opsevo does not add the area opsevo")
	}
	if strings.Contains(plan, "k8sevo") {
		t.Error("live-plan: k8sevo is the job k8s-smoke's, not an area of the job live")
	}
	// The worst run of the area — its budget, the waits of a part begun
	// before it (up to 25 min), the last wait of a step (10) and the sweep
	// (up to 20) — within the limit of the job live.
	budget := regexp.MustCompile(`EV_BUDGET=\$\{EV_BUDGET:-(\d+)\}`).FindAllStringSubmatch(sec, -1)
	if len(budget) != 2 {
		t.Fatalf("cloud-live.sh: %d budgets of the areas opsevo and k8sevo, want 2 — update the test", len(budget))
	}
	if worst := atoi(t, budget[0][1])/60 + 25 + 10 + 20; worst > wf.Jobs["live"].TimeoutMinutes {
		t.Errorf("job live: timeout-minutes %d, the worst run of opsevo is %d min", wf.Jobs["live"].TimeoutMinutes, worst)
	}

	// The part of Kubernetes: one step of k8s-smoke, by the word k8sevo,
	// with a limit of its own; budget and the last wait within it.
	var k8s int
	for name, job := range wf.Jobs {
		for _, s := range job.Steps {
			if !strings.Contains(s.Run, "cloud-live.sh k8sevo") {
				continue
			}
			k8s++
			if name != "k8s-smoke" {
				t.Errorf("job %s runs the area k8sevo — it is the job k8s-smoke's", name)
			}
			if !strings.Contains(s.If, "' k8sevo '") {
				t.Errorf("k8s-smoke, %s: runs without the word k8sevo: %q", s.Name, s.If)
			}
			if !strings.Contains(s.Run, `K8S_CLUSTER_ID="$CLUSTER_ID"`) {
				t.Errorf("k8s-smoke, %s: no K8S_CLUSTER_ID of the job's cluster", s.Name)
			}
			b := atoi(t, s.Env["EV_BUDGET"])
			if s.TimeoutMinutes == 0 || b/60+25 > s.TimeoutMinutes {
				t.Errorf("k8s-smoke, %s: timeout-minutes %d, EV_BUDGET %d s and the last wait of 25 min do not fit", s.Name, s.TimeoutMinutes, b)
			}
			// 40 (cluster) + 30 (pool) + 35 (checks) + 35 (pod) + the step
			// + 30 (delete).
			if 40+30+35+35+s.TimeoutMinutes+30 > job.TimeoutMinutes {
				t.Errorf("k8s-smoke: timeout-minutes %d leaves no room for the delete after the step %s (%d min)", job.TimeoutMinutes, s.Name, s.TimeoutMinutes)
			}
		}
	}
	if k8s != 1 {
		t.Errorf("%d steps run the area k8sevo, want one in the job k8s-smoke", k8s)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("%q is not a number", s)
		}
		n = n*10 + int(c-'0')
	}
	if s == "" {
		t.Fatal("an empty number")
	}
	return n
}
