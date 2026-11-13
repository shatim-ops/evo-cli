// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// stateDirWorld is a working directory with the record of a call an
// earlier run left in it, and the environment of a run of evo there.
func stateDirWorld(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	// The directory as the process names it (a temp dir may be a link).
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("CLOUDRU_KEY_ID", "k")
	t.Setenv("CLOUDRU_SECRET", "s")
	t.Setenv("CLOUDRU_PROJECT_ID", "00000000-0000-4000-8000-0000000000aa")
	if _, err := intent.Open(intent.Dir(dir), intent.Intent{Tool: "vm_reboot", Target: "vm_id=left-by-an-earlier-run"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// One function decides the directory evo works in, and both the engines
// and the registries of every build of the product have it: the CLI's
// registry, the server of evo mcp over stdio and over HTTP, with and
// without --allow-project, and an engine built with the overrides of a
// call (release plan 1.9, «Каталог записей намерений»; «Риск», conclusion
// 172, У2 and У4). A registry is asked by what it does: its first answer
// names the record left in the directory.
func TestStateDir_EveryBuildOfTheProductDeclaresTheEnginesDirectory(t *testing.T) {
	dir := stateDirWorld(t)
	decided, err := stateDir()
	if err != nil || decided != dir {
		t.Fatalf("stateDir() = %q, %v; want %q", decided, err, dir)
	}
	for name, over := range map[string]map[string]any{
		"no overrides":            {},
		"the overrides of a call": {"ProjectID": "00000000-0000-4000-8000-0000000000bb", "Region": "ru.AZ-2"},
	} {
		eng, err := buildEngine(over)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if eng.StateDir != dir {
			t.Errorf("%s: the engine stands in %q, want %q", name, eng.StateDir, dir)
		}
	}

	names := func(answer string) bool { return strings.Contains(answer, "left-by-an-earlier-run") }
	skill, _ := newRegistry().Lookup("agent_skill")
	if text, _ := skill.Handler(context.Background(), map[string]any{"group": "vm"}); !names(text) {
		t.Errorf("the CLI's registry does not read the records of %s:\n%s", dir, text)
	}
	for _, allow := range [][]string{nil, {"00000000-0000-4000-8000-0000000000aa"}} {
		for transport, call := range mcpTransports(t, allow) {
			if a := call("agent_skill", map[string]any{"group": "vm"}); !names(a.text) {
				t.Errorf("evo mcp over %s, --allow-project %v, does not read the records of %s:\n%s", transport, allow, dir, a.text)
			}
		}
	}
}
