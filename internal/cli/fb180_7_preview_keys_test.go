// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// fb180UserKey is the key of .evo/user_authorized_keys in the test: a public
// key made for it, of no one.
const fb180UserKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAqpH6CquGwydHHTrIDK/mcZXCUnNqEnugLa3ugpoznp parity@example.test"

// Audit Fable 1.8, FB180-7 (2): the keys of the user deploy_apply appends
// are in the preview of the tool and in the human preview of the command
// alike (axis 4, С2). The world of parity has no user keys: a consented
// apply there would SSH to its VM to append them, and the world waits for
// no production interval (CLAUDE.md §5); this preview is checked here,
// under --dry-run, which creates nothing.
func TestDeployApply_HumanPreviewShowsTheUserKeys(t *testing.T) {
	w, _ := withJSONWorld(t)
	keys := filepath.Join(config.DataDir(w.dir), "user_authorized_keys")
	if err := os.MkdirAll(filepath.Dir(keys), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keys, []byte(fb180UserKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, _ := newRegistry().Lookup("deploy_plan")
	text, isErr := plan.Handler(context.Background(), map[string]any{})
	id, _ := parseObject(text)["plan_id"].(string)
	if isErr || id == "" {
		t.Fatalf("deploy_plan: %s", text)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fb180UserKey))
	if err != nil {
		t.Fatal(err)
	}
	fp := ssh.FingerprintSHA256(pub)
	for _, json := range []bool{true, false} {
		args := []string{"--dry-run", "deploy", "apply", id}
		if json {
			args = append([]string{"--json"}, args...)
		}
		code, out, errOut := runCLI(t, "", args...)
		if code != 0 || !strings.Contains(out+errOut, fp) || !strings.Contains(out+errOut, "parity@example.test") {
			t.Errorf("evo %s: exit %d, want 0 and the user's key %s in the preview\n%.600s\n%.600s", strings.Join(args, " "), code, fp, out, errOut)
		}
	}
}
