// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// An empty since is not «the last 30 days»: it is refused USAGE, as an
// empty project_id beside it is (D100), over MCP and in the CLI — an agent
// that built the argument from an empty variable reads another window than
// it meant (A19-33, #2435).
func TestHistory_EmptySinceIsUsage(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	for _, arg := range []string{"since", "project_id"} {
		m := stdioDetails(t, srv.stdio, "history", map[string]any{arg: ""})
		if m["reason"] != "USAGE" || m["argument"] != arg {
			t.Errorf("mcp %s: \"\": want USAGE naming it: %v", arg, m)
		}
		forgetRun(w)
		code, out, errOut := runCLI(t, "", "history", "--"+strings.ReplaceAll(arg, "_", "-"), "", "--json")
		if code == 0 || !strings.Contains(out+errOut, "USAGE") {
			t.Errorf("cli --%s \"\": want USAGE, code %d: %s%s", arg, code, out, errOut)
		}
	}
}
