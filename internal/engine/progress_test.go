// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// slowVM answers "creating" for d, then "active": a VM that takes a while.
func slowVM(d time.Duration) func(context.Context, string) (*provision.VM, error) {
	var once sync.Once
	var start time.Time
	return func(_ context.Context, id string) (*provision.VM, error) {
		once.Do(func() { start = time.Now() })
		// wall-clock: not a bound — the fake VM is slow for d.
		if time.Since(start) < d {
			return &provision.VM{ID: id, Status: "creating"}, nil
		}
		return &provision.VM{ID: id, Status: "active", PublicIP: "127.0.0.1"}, nil
	}
}

func deployWith(t *testing.T, mode output.Mode, wait time.Duration, auto bool) (string, error) {
	t.Helper()
	out, _, err := deployWithStderr(t, mode, wait, auto)
	return out, err
}

// deployWithStderr deploys a VM that is "creating" for wait; it returns
// stdout and stderr.
func deployWithStderr(t *testing.T, mode output.Mode, wait time.Duration, auto bool) (string, string, error) {
	t.Helper()
	cloud := &mockCloudAPI{getVMFn: slowVM(wait)}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var out, errOut bytes.Buffer
	eng.Printer = output.NewWithWriters(&out, &errOut, mode, false)
	eng.Timings.VMPollInterval = 2 * time.Millisecond
	eng.Timings.VMProgressInterval = 20 * time.Millisecond
	flavor := "small"
	if auto {
		flavor = "auto"
	}
	err := eng.Deploy(context.Background(), DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: flavor,
		Region: "az-1", NonInteractive: true, Yes: true})
	return out.String(), errOut.String(), err
}

// The flavor picked for the workload is said under step 3, where the VM is
// created, not under step 1 (ревью r5 U10); the steps go in order.
func TestDeploy_FlavorUnderProvisionStep(t *testing.T) {
	out, err := deployWith(t, output.ModePlain, 0, true)
	if err != nil {
		t.Fatalf("Deploy: %v\n%s", err, out)
	}
	at := func(s string) int {
		i := strings.Index(out, s)
		if i < 0 {
			t.Fatalf("no %q in:\n%s", s, out)
		}
		return i
	}
	steps := []int{at("[1/5]"), at("[2/5]"), at("[3/5]"), at("Recommended flavor: "), at("[4/5]"), at("[5/5]")}
	for i := 1; i < len(steps); i++ {
		if steps[i] < steps[i-1] {
			t.Errorf("out of order (%d):\n%s", i, out)
		}
	}
}

// Without a terminal: a "waiting for the VM… <elapsed> (<state>)" line on
// the first state and then every VMProgressInterval, with no carriage
// returns or escape codes.
func TestDeploy_VMWaitLinesWithoutTTY(t *testing.T) {
	out, err := deployWith(t, output.ModePlain, 150*time.Millisecond, false)
	if err != nil {
		t.Fatalf("Deploy: %v\n%s", err, out)
	}
	lines := regexp.MustCompile(`(?m)^    waiting for the VM… \d+s \(creating\)$`).FindAllString(out, -1)
	if len(lines) < 3 {
		t.Errorf("want the first line and periodic ones, got %d:\n%s", len(lines), out)
	}
	if strings.ContainsAny(out, "\r\x1b") {
		t.Errorf("control characters without a terminal: %q", out)
	}
	if !regexp.MustCompile(`(?m)^    VM \S+ (\(\S+\) )?active \(\d+s\)$`).MatchString(out) {
		t.Errorf("no 'VM … active (<elapsed>)':\n%s", out)
	}
}

// On a terminal: one line redrawn in place (carriage return, no newline),
// erased before the next line; no escape codes without color.
func TestDeploy_VMWaitLineOnTTY(t *testing.T) {
	out, err := deployWith(t, output.ModeText, 30*time.Millisecond, false)
	if err != nil {
		t.Fatalf("Deploy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "\r    waiting for the VM… 0s (creating)") {
		t.Errorf("no progress line:\n%q", out)
	}
	if strings.Contains(out, "waiting for the VM… 0s (creating)\n") {
		t.Errorf("the progress line ends with a newline:\n%q", out)
	}
	i := strings.LastIndex(out, "waiting for the VM")
	if j := strings.Index(out, "VM vm-"); j >= 0 && j < i {
		t.Errorf("progress after the VM is active:\n%q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape codes without color: %q", out)
	}
}

// --json: stdout is the result object and nothing else; stderr has no
// progress either (only JSON lines).
func TestDeploy_VMWaitJSONClean(t *testing.T) {
	out, errOut, err := deployWithStderr(t, output.ModeJSON, 60*time.Millisecond, true)
	if err != nil {
		t.Fatalf("Deploy: %v\n%s", err, out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil || v["status"] != "deployed" {
		t.Errorf("stdout is not just the result: %v\n%q", err, out)
	}
	if strings.Contains(out+errOut, "waiting for the VM") || strings.Contains(out+errOut, "\r") {
		t.Errorf("progress in --json:\nstdout %q\nstderr %q", out, errOut)
	}
	for _, line := range strings.Split(strings.TrimSpace(errOut), "\n") {
		if line != "" && !json.Valid([]byte(line)) {
			t.Errorf("stderr line is not JSON: %q", line)
		}
	}
}

func TestElapsedText(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang(i18n.EN)
	if got := ElapsedText(80*time.Second + 400*time.Millisecond); got != "1m20s" {
		t.Errorf("en: %q", got)
	}
	i18n.SetLang(i18n.RU)
	if got := ElapsedText(80 * time.Second); got != "1м20с" {
		t.Errorf("ru: %q", got)
	}
	if got := ElapsedText(time.Hour + 2*time.Minute); got != "1ч2м0с" {
		t.Errorf("ru, hours: %q", got)
	}
}
