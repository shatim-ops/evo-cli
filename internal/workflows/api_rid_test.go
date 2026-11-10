// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// CMP-7 (Compute, 10.11): the platform asks for the id of each request the
// probe sent. api_rid of cloud-live.sh prints the request, trace and
// correlation ids of the last answer — never a cookie, a token or another
// header — with the dashes as dots, so that redact.sed, which masks UUIDs
// as ids of resources, leaves them to be read.
func TestScripts_APIRidNamesOnlyRequestIDs(t *testing.T) {
	// cloud-live.sh runs on the Linux runners of cloud-e2e only; on Windows
	// the variable tmp the function reads meets TMP of the system, whose
	// name the environment does not tell apart (main, windows-latest, 10.11).
	if runtime.GOOS == "windows" {
		t.Skip("bash script: CI runs it on Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	tmp := t.TempDir()
	h := "HTTP/2 422\r\nX-Request-Id: 3f2a1b4c-1111-2222-3333-444455556666\r\n" +
		"Set-Cookie: s=evo-secret-cookie\r\nAuthorization: Bearer evo-secret-token\r\n" +
		"traceparent: 00-abc-def-01\r\nX-Other: evo-other\r\n\r\n"
	if err := os.WriteFile(filepath.Join(tmp, "api.h"), []byte(h), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..")
	src, err := os.ReadFile(filepath.Join(root, ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// The function alone, in a file: bash 3.2 of macOS does not source a
	// process substitution.
	fn := string(src)
	i := strings.Index(fn, "\napi_rid() {")
	if i < 0 {
		t.Fatal("cloud-live.sh: no api_rid")
	}
	fn = fn[i+1:]
	fn = fn[:strings.Index(fn, "\n}\n")+3]
	lib := filepath.Join(tmp, "api_rid.sh")
	if err := os.WriteFile(lib, []byte(fn), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `. "$1" && api_rid`
	// redact.sed is GNU sed (\b): the runners of cloud-e2e are Linux.
	if runtime.GOOS == "linux" {
		script += ` | sed -E -f .github/scripts/redact.sed`
	}
	cmd := exec.Command("bash", "-c", script, "bash", lib)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "tmp="+tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("api_rid: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "x-request-id=3f2a1b4c.1111.2222.3333.444455556666") || !strings.Contains(got, "traceparent=00.abc.def.01") {
		t.Errorf("api_rid does not name the request id readably after redact: %q", got)
	}
	for _, leak := range []string{"secret", "evo-other", "cookie", "authorization"} {
		if strings.Contains(strings.ToLower(got), leak) {
			t.Errorf("api_rid prints %q: only request ids may go to the public log — %q", leak, got)
		}
	}
}
