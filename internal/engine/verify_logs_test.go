// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

func engineWithDeployment(t *testing.T, rec state.DeploymentRecord) (*Engine, *mockDeployer, *bytes.Buffer) {
	t.Helper()
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	return eng, dep, &out
}

func probeReturning(healthy map[int]bool) func(context.Context, string, []int, time.Duration) []verify.ProbeResult {
	return func(_ context.Context, _ string, ports []int, _ time.Duration) []verify.ProbeResult {
		res := make([]verify.ProbeResult, len(ports))
		for i, p := range ports {
			res[i] = verify.ProbeResult{Port: p, Healthy: healthy[p], StatusCode: 200}
			if !healthy[p] {
				res[i] = verify.ProbeResult{Port: p, Error: "connection refused"}
			}
		}
		return res
	}
}

var verifyRecord = state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "198.51.100.10", SSHUser: "user1",
	Status: "running", ExposedPorts: []int{80, 8080}}

// verify used to exit 0 with "no" in the table; scripts could not tell.
func TestVerify_UnhealthyIsError(t *testing.T) {
	eng, _, out := engineWithDeployment(t, verifyRecord)
	eng.Probe = probeReturning(map[int]bool{80: true, 8080: false})

	err := eng.Verify(context.Background())
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeUnhealthy {
		t.Fatalf("err = %v, want UNHEALTHY", err)
	}
	if got := clerrors.ExitCode(err); got != clerrors.ExitGeneral {
		t.Errorf("exit code = %d, want %d", got, clerrors.ExitGeneral)
	}
	if !strings.Contains(cliErr.Message, "1 of 2") {
		t.Errorf("message = %q", cliErr.Message)
	}
	if !strings.Contains(out.String(), "connection refused") {
		t.Errorf("table missing:\n%s", out.String())
	}
}

func TestVerify_AllHealthyIsSuccess(t *testing.T) {
	eng, _, _ := engineWithDeployment(t, verifyRecord)
	eng.Probe = probeReturning(map[int]bool{80: true, 8080: true})
	if err := eng.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// With --json the results still go to stdout, and the exit code still says
// unhealthy.
func TestVerify_JSONUnhealthy(t *testing.T) {
	eng, _, _ := engineWithDeployment(t, verifyRecord)
	var stdout, stderr bytes.Buffer
	eng.Printer = output.NewWithWriters(&stdout, &stderr, output.ModeJSON, false)
	eng.Probe = probeReturning(map[int]bool{})

	err := eng.Verify(context.Background())
	if clerrors.ExitCode(err) != clerrors.ExitGeneral {
		t.Fatalf("err = %v", err)
	}
	var results []verify.ProbeResult
	if jerr := json.Unmarshal(stdout.Bytes(), &results); jerr != nil || len(results) != 2 {
		t.Errorf("stdout is not the results JSON: %v %q", jerr, stdout.String())
	}
}

// logs -f streams through ExecStream (no 120 s Exec limit) and the service
// name reaches the remote shell quoted.
func TestLogs_FollowStreamsWithQuotedService(t *testing.T) {
	eng, dep, out := engineWithDeployment(t, verifyRecord)
	want := "cd /opt/app && docker compose logs --tail 10 -f 'web; rm -rf /'"
	dep.execResults[want] = "web-1 | hello\n"

	err := eng.Logs(context.Background(), LogsOptions{Service: "web; rm -rf /", Tail: 10, Follow: true})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(dep.execCalls) != 1 || dep.execCalls[0] != want {
		t.Errorf("calls = %q, want [%q]", dep.execCalls, want)
	}
	if out.String() != "web-1 | hello\n" {
		t.Errorf("output = %q", out.String())
	}
}

// Ctrl-C ends a follow stream with the cancellation (exit 130), not with
// "failed to retrieve logs".
func TestLogs_FollowCancelled(t *testing.T) {
	eng, dep, _ := engineWithDeployment(t, verifyRecord)
	ctx, cancel := context.WithCancel(context.Background())
	dep.streamHook = func(ctx context.Context, _ string) error {
		cancel()
		<-ctx.Done()
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution cancelled", "", ctx.Err())
	}

	err := eng.Logs(ctx, LogsOptions{Follow: true})
	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
}

// logs loads the SSH key lazily: it is the first thing that needs it.
func TestLogs_LoadsSSHKeyLazily(t *testing.T) {
	eng, _, _ := engineWithDeployment(t, verifyRecord)
	eng.SSHKeyPEM, eng.SSHPubKey = nil, ""
	loaded := false
	eng.SSHKeyLoader = func() ([]byte, string, error) { loaded = true; return []byte("pem"), "pub", nil }

	if err := eng.Logs(context.Background(), LogsOptions{}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !loaded {
		t.Error("SSH key not loaded before connecting")
	}
}
