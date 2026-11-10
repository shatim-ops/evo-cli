// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package loginworld

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
)

// probeScript is the manual probe of the browser login the owner runs
// (B-5, docs/internal/briefs/login-probe.md).
var probeScript = filepath.Join("..", "..", "scripts", "login-probe.sh")

// The probe script walks the world from discovery to the projects as a
// person would — the browser is the world's — and prints what a report
// may keep: no code, token or key the world handed out is in its output.
// Its defaults are the login table's issuer and Organization API.
func TestLoginWorld_ProbeScript(t *testing.T) {
	b, err := os.ReadFile(probeScript)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ISSUER="${ISSUER:-` + auth.OIDCIssuer + `}"`, `ORG="${ORG:-https://organization.api.cloud.ru}"`,
		`PORT=` + strconv.Itoa(auth.LoginRedirectPorts[0]), "client_id=" + auth.LoginClientID} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s: the probe drifted from the login table", probeScript, want)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("the probe is a bash script for macOS and Linux")
	}
	for _, tool := range []string{"bash", "curl", "jq", "openssl", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(auth.LoginRedirectPorts[0]))); err != nil {
		t.Skipf("port %d is busy", auth.LoginRedirectPorts[0])
	} else {
		ln.Close()
	}
	w := New(t, Variant{})
	// wall-clock: the script waits for the browser up to 5 minutes; a
	// world that answers at once needs seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", probeScript)
	// EXTRA: the parameters of the probe of a fresh sign-in (prompt=login,
	// max_age; owner's decision 10.11) go into the address of the login.
	cmd.Env = append(os.Environ(), "ISSUER="+w.Issuer, "ORG="+w.URL, "EXTRA=prompt=login&max_age=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		l := sc.Text()
		lines = append(lines, l)
		if strings.HasPrefix(l, w.Issuer+"/openid/auth?") {
			go func() { // the person signs in once the script listens:
				// a refused callback is a listener not up yet (a probe
				// connection would take its one request)
				for i := 0; i < 100; i++ {
					if _, err := w.Browser(l); err == nil {
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
			}()
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("probe: %v\n%s", err, strings.Join(lines, "\n"))
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{`"issuer_ok":true`, `"state_ok":true`, `"expires_in":3600`,
		`"repeat_same_code_http":400`, `"iss_ok":true`, `"nonce_ok":true`, `{"customers":200`, `{"projects":200`,
		"&prompt=login&max_age=0", `"auth_age_s":`, `"prompt_values":`, `"auth_time_claim":`} {
		if !strings.Contains(text, want) {
			t.Errorf("the probe's output lacks %s:\n%s", want, text)
		}
	}
	for _, s := range w.Secrets() {
		if strings.Contains(text, s) {
			t.Errorf("the probe printed a secret of kind %s", strings.SplitN(s, "-", 2)[0])
		}
	}
}

// EXTRA takes only prompt=login and max_age=N: anything else is refused
// before any request — the address of the login carries no parameter the
// probe does not mean.
func TestLoginWorld_ProbeScriptExtraRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe is a bash script for macOS and Linux")
	}
	for _, tool := range []string{"bash", "curl", "jq", "openssl", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	for _, extra := range []string{"prompt=none", "max_age=1&redirect_uri=http://evil", "scope=openid offline_access", "max_age=-1"} {
		cmd := exec.Command("bash", probeScript)
		// An issuer nothing answers at: a request would fail differently.
		cmd.Env = append(os.Environ(), "ISSUER=http://127.0.0.1:1/auth/system", "EXTRA="+extra)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "EXTRA: только") || strings.Contains(string(out), "== 1.") {
			t.Errorf("EXTRA=%q: %v\n%s", extra, err, out)
		}
	}
}
