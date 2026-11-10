// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-7 (Compute, 10.11): the platform asks for the id of each request the
// probe sent. api_rid of cloud-live.sh prints the request, trace and
// correlation ids of the last answer — never a cookie, a token or another
// header — with the dashes as dots, so that redact.sed, which masks UUIDs
// as ids of resources, leaves them to be read.
func TestScripts_APIRidNamesOnlyRequestIDs(t *testing.T) {
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
	script := `source <(sed -n '/^api_rid()/,/^}/p' .github/scripts/cloud-live.sh) && api_rid | sed -E -f .github/scripts/redact.sed`
	cmd := exec.Command("bash", "-c", script)
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
