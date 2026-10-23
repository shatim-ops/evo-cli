// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// evo deploy --dry-run without an access key shows what it can of the
// preview, as 'evo tools call deploy --dry-run' does: the missing settings
// are named in it (D31, REL12-7). It was AUTH_REQUIRED, exit 2: a dry run
// to learn what deploy would do needed the key first. Without --dry-run
// the key is still required.
func TestDeploy_DryRunWithoutKey(t *testing.T) {
	setupCLI(t)
	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, "", "deploy", "--dry-run", "--json")
	var m map[string]any
	if code != 0 || json.Unmarshal([]byte(stdout), &m) != nil || m["status"] != "dry_run" {
		t.Fatalf("deploy --dry-run without a key: exit %d\n%s\n%s", code, stdout, stderr)
	}
	newProcess()
	_, tstdout, _ := runCLI(t, "", "tools", "call", "deploy", "--dry-run", "--json")
	var tm map[string]any
	if json.Unmarshal([]byte(tstdout), &tm) != nil || tm["status"] != "dry_run" {
		t.Fatalf("tools call deploy --dry-run: %s", tstdout)
	}
	if code, _, stderr := runCLI(t, "", "deploy", "--yes"); code != clerrors.ExitAuth {
		t.Errorf("deploy --yes without a key: exit %d, want 2: %s", code, stderr)
	}
}
