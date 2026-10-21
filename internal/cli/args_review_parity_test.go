// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// TestReviewParity_RP_8_IntegerScientificNotationDiverges: the same integer
// 1e3 is a usage error on the CLI flag of an integer argument and a float64
// 1000 that passes the tool's schema check on MCP (JSON number).
func TestReviewParity_RP_8_IntegerScientificNotationDiverges(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "00000000-0000-4000-8000-000000000001")

	code, _, errOut := runCLI(t, "", "--json", "disk", "create", "data", "--size-gb", "1e3", "--yes")
	tl, ok := tools.New(nil).Lookup("disk_create")
	if !ok {
		t.Fatal("disk_create")
	}
	var mcp map[string]any
	if err := json.Unmarshal([]byte(`{"name":"data","size_gb":1e3,"confirmed":true}`), &mcp); err != nil {
		t.Fatal(err)
	}
	mcpErr := tools.CheckArgs(tl, mcp)
	cliRejected := code == clerrors.ExitGeneral && strings.Contains(errOut, "1e3")
	mcpAccepted := mcpErr == nil && mcp["size_gb"] == float64(1000)
	if cliRejected && mcpAccepted {
		t.Fatalf("CLI rejects --size-gb 1e3 (exit %d: %s) while MCP accepts JSON 1e3 as %v", code, strings.TrimSpace(errOut), mcp["size_gb"])
	}
	t.Fatalf("divergence not in the expected shape: CLI exit %d %s; MCP check %v args %#v", code, errOut, mcpErr, mcp)
}
