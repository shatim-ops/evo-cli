// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// scriptFuncs is the text of the named functions of cloud-live.sh, each
// whole, for a test to source.
func scriptFuncs(t *testing.T, names ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	var out strings.Builder
	for _, n := range names {
		i := strings.Index(s, "\n"+n+"() {")
		if i < 0 {
			t.Fatalf("cloud-live.sh: no %s", n)
		}
		fn := s[i+1:]
		fn = fn[:strings.Index(fn, "\n}\n")+3]
		out.WriteString(fn)
	}
	return out.String()
}

// Runs 184 and 185 (10.11): image_get failed in the probe cmp7, vm_meta
// sent its stderr to /dev/null, and the line said only «image_get did not
// answer fields» — two runs with the owner's «yes» gave no reason. When
// image_get fails, the line of the probe names the reason and message evo
// answered; when it answers, the metadata are its fields.
func TestScripts_VMMetaNamesTheErrorOfImageGet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash script: CI runs it on Linux")
	}
	for _, tool := range []string{"bash", "jq", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root := filepath.Join("..", "..")
	fake, err := filepath.Abs(filepath.Join(root, ".github", "scripts", "fake_evo.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, rc, answer string
		check            func(t *testing.T, code string, api map[string]any)
	}{
		{"error", "2", `{"error":{"code":2,"reason":"NOT_FOUND","message":"image_get: образ evo-test-image не найден"}}`,
			func(t *testing.T, code string, api map[string]any) {
				msg, _ := api["message"].(string)
				if code != "000" || !strings.Contains(msg, "NOT_FOUND") || !strings.Contains(msg, "не найден") {
					t.Errorf("image_get failed: code %q, message %q; want 000 and the reason and message of evo", code, msg)
				}
			}},
		{"fields", "0", `{"fields":[{"name":"hostname","filled_by":"evo","role":"vm_name"},{"name":"tz","filled_by":"default","default_value":"UTC"},{"name":"lic","filled_by":"user"}]}`,
			func(t *testing.T, code string, api map[string]any) {
				if code != "201" {
					t.Errorf("image_get answered: code %q; want the POST sent (201)", code)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			lib := filepath.Join(tmp, "lib.sh")
			// api stands in for the request: it keeps the body it was given.
			// mask prints its command, as in cloud-live.sh: with a no-op
			// here the test did not see that vm_meta, read through $(…),
			// put «::add-mask::…» before its JSON — vm_post then took the
			// metadata for missing and sent nothing (probes p36, cmp7).
			src := scriptFuncs(t, "vm_post", "vm_pw", "vm_meta") +
				"mask() { echo \"::add-mask::$1\"; }\napi() { printf '%s' \"$3\" >\"$tmp/body.json\"; code=201; echo '{}' >\"$tmp/api.json\"; }\n"
			if err := os.WriteFile(lib, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			script := `set -euo pipefail
. "$1"
. "$2" && fake_evo_install "$FAKE_DIR/bin"
printf '%s' "$ANSWER" >"$FAKE_DIR/image_get.0.json"
printf '%s' "$RC" >"$FAKE_DIR/image_get.0.rc"
PATH="$FAKE_DIR/bin:$PATH"
PROJECT=p code=
echo 'ssh-ed25519 AAAA evo-probe' >"$tmp/vmprobe_key.pub"
vm_post evo-test-vm small evo-test-image zone 20 SSD '[]'
printf '%s' "$code" >"$tmp/code"`
			cmd := exec.Command("bash", "-c", script, "bash", lib, fake)
			cmd.Env = append(os.Environ(), "tmp="+tmp, "FAKE_DIR="+tmp, "ANSWER="+c.answer, "RC="+c.rc)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("vm_post: %v\n%s", err, out)
			}
			code, _ := os.ReadFile(filepath.Join(tmp, "code"))
			var api map[string]any
			b, _ := os.ReadFile(filepath.Join(tmp, "api.json"))
			if err := json.Unmarshal(b, &api); err != nil {
				t.Fatalf("api.json: %v: %q", err, b)
			}
			c.check(t, string(code), api)
			if c.name == "fields" {
				var body []map[string]any
				bb, _ := os.ReadFile(filepath.Join(tmp, "body.json"))
				if err := json.Unmarshal(bb, &body); err != nil || len(body) != 1 {
					t.Fatalf("body: %v: %q", err, bb)
				}
				m, _ := body[0]["image_metadata"].(map[string]any)
				if m["hostname"] != "evo-test-vm" || m["tz"] != "UTC" || m["lic"] != nil {
					t.Errorf("image_metadata %v; want hostname of the VM, tz by default, no field of the person", m)
				}
			}
		})
	}
}

// Run 185 (10.11): «VM shut down from inside — state running after 5 min»,
// and the code of `ssh … sudo shutdown` was dropped by `|| true`: whether
// the command reached the VM is not in the log. A call of ssh in the
// scripts of cloud-e2e keeps its exit code.
func TestScripts_SSHExitCodeIsKept(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "scripts", "*.sh"))
	if err != nil || len(files) == 0 {
		t.Fatal("no scripts in .github/scripts")
	}
	swallowed := regexp.MustCompile(`\bssh(_vm)?\b[^\n#]*\|\|\s*(true\b|:)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range swallowed.FindAll(b, -1) {
			t.Errorf("%s: %q drops the exit code of ssh: keep it for the line of the check", filepath.Base(f), m)
		}
	}
}
