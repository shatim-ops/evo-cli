// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// R-38 in the CLI: --yes with --compose-hash is the consent for the file of
// that hash; another file is not deployed under it — CONFIRMATION_REQUIRED,
// code 3, before anything else (no credentials are reached).
func TestRun_DeployComposeChangedSinceConsentIsAskedAgain(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_KEY_ID", "k")
	t.Setenv("CLOUDRU_SECRET", "s")
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })
	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{"deploy", "--yes", "--compose-hash", "0000"}); got != clerrors.ExitConfirmationRequired {
		t.Errorf("evo deploy --yes with the hash of another file = %d, want %d", got, clerrors.ExitConfirmationRequired)
	}
}
