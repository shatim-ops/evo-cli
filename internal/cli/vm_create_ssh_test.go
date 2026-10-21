// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Dogfood 1.2.1, feedback 3: after evo vm create the user could not log in
// — "ssh user1@IP" without evo's key, the generated password lost, and no
// word that the key and the state are in the directory evo ran in. The
// command now shows the ssh command with the key, the password once and the
// directory; --json is the tool's answer. Addresses are made up.
func TestVMCreate_ShowsSSHPasswordAndDirectory(t *testing.T) {
	russian(t)
	setupCLI(t)
	work, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cloud := withVMCreateCloud(t)
	cloud.fip = &provision.FloatingIP{ID: "00000000-0000-0000-0000-00000000b004", IPAddress: "203.0.113.7", State: "available"}
	args := []string{"vm", "create", "web", "--image", "astralinuxsmolensk1.8.3kes"}

	code, out, errOut := runCLI(t, "", append([]string{"--yes"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	evo := filepath.Join(work, ".evo")
	ssh := "ssh -i " + filepath.Join(evo, "id_ed25519") + " -o UserKnownHostsFile=" + filepath.Join(evo, "known_hosts") +
		" -o StrictHostKeyChecking=accept-new user1@203.0.113.7"
	pw := cloud.creates[0].ImageMetadata["linux_password"]
	if pw == "" {
		t.Fatalf("no password generated: %v", cloud.creates[0].ImageMetadata)
	}
	for _, want := range []string{
		"\nSSH: " + ssh + "\n",
		"\nПароль ВМ: " + pw + " (записан только здесь — сохраните: evo его не хранит)\n",
		"\nКлюч SSH и state — в " + evo + ": evo status и evo vm delete запускайте из " + work + ".\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in stdout:\n%s", want, out)
		}
	}
	// The password once: on its line, not in the table too.
	if n := strings.Count(out, pw); n != 1 {
		t.Errorf("the password is %d times in stdout:\n%s", n, out)
	}
	if strings.Contains(errOut, pw) {
		t.Errorf("the password on stderr:\n%s", errOut)
	}
	// The summary on stderr gives the same command.
	if !strings.Contains(errOut, ". SSH: "+ssh+"\n") {
		t.Errorf("the summary has not the ssh command:\n%s", errOut)
	}
	// The key the command names is the one the VM got.
	pub, err := os.ReadFile(filepath.Join(evo, "id_ed25519.pub"))
	if err != nil || strings.TrimSpace(string(pub)) != cloud.creates[0].ImageMetadata["public_key"] {
		t.Errorf("the VM's key is not .evo/id_ed25519.pub (%v): %q", err, cloud.creates[0].ImageMetadata["public_key"])
	}

	// --json: the tool's answer, with the same fields.
	code, out, errOut = runCLI(t, "", append([]string{"--json", "--yes"}, append(args[:2:2], "web-2", "--image", "astralinuxsmolensk1.8.3kes")...)...)
	if code != 0 {
		t.Fatalf("--json: exit %d:\n%s\n%s", code, out, errOut)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if res["ssh"] != ssh || res["password"] != cloud.creates[1].ImageMetadata["linux_password"] || res["state_dir"] != evo {
		t.Errorf("--json: ssh %v, password %v, state_dir %v", res["ssh"], res["password"], res["state_dir"])
	}
}
