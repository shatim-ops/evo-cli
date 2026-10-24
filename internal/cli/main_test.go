// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// TestMain runs the tests in English: they assert the English texts.
// Tests of the Russian output (ru_test.go, docs/commands.md) set the
// language themselves.
func TestMain(m *testing.M) {
	i18n.SetLang("en")
	os.Exit(m.Run())
}

func TestHandleError_WrappedCLIErrorKeepsHint(t *testing.T) {
	err := fmt.Errorf("list zones: %w", clerrors.New(clerrors.CodeAuthRequired,
		"Cloud.ru credentials not set", "Set CLOUDRU_KEY_ID and CLOUDRU_SECRET"))

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModePlain, false), err)

	got := buf.String()
	if !strings.Contains(got, "Error: list zones:") || !strings.Contains(got, "Cloud.ru credentials not set") {
		t.Errorf("message lost: %q", got)
	}
	if !strings.Contains(got, "Set CLOUDRU_KEY_ID and CLOUDRU_SECRET") {
		t.Errorf("hint lost through wrapping: %q", got)
	}
}

func TestHandleError_DirectCLIError(t *testing.T) {
	err := clerrors.New(clerrors.CodeVMNotFound, "vm not found", "check the id")

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModePlain, false), err)

	if want := "Error: vm not found\ncheck the id\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestHandleError_JSON(t *testing.T) {
	err := fmt.Errorf("destroy: %w", clerrors.New(clerrors.CodeConfirmationRequired, "no tty", "use --force"))

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false), err)

	var got struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &got); jerr != nil {
		t.Fatalf("not JSON: %v: %q", jerr, buf.String())
	}
	if got.Error.Code != clerrors.ExitConfirmationRequired || got.Error.Hint != "use --force" ||
		!strings.Contains(got.Error.Message, "no tty") {
		t.Errorf("unexpected payload: %+v", got.Error)
	}
}

func TestHandleError_InterruptedKeepsHint(t *testing.T) {
	// As run() sees a deploy interrupted after the VM was created.
	inner := clerrors.Wrap(clerrors.CodeVMProvisionFailed, "deployment interrupted",
		"VM vm-1 was created and is still running. Use 'evo destroy --yes' to delete it.", context.Canceled)
	err := fmt.Errorf("%w: %w", context.Canceled, inner)

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false), err)

	if !strings.Contains(buf.String(), `"code":130`) || !strings.Contains(buf.String(), "vm-1") ||
		!strings.Contains(buf.String(), "destroy --yes") {
		t.Errorf("interrupt must keep the VM hint: %q", buf.String())
	}
}

// QA-16: Ctrl-C cuts off a request, which fails as API_UNREACHABLE with a
// hint about the proxy and "interrupt signal received". The error is
// "interrupted" (130, INTERRUPTED) without the network blamed, in both
// languages and both formats.
func TestHandleError_InterruptedNoNetworkHint(t *testing.T) {
	defer i18n.SetLang(i18n.EN)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		cut := stderrors.New("interrupt signal received")
		inner := clerrors.Wrap(clerrors.CodeAPIUnreachable, i18n.T("failed to reach IAM endpoint", "IAM недоступен"),
			clerrors.NetworkHint(&net.OpError{Op: "proxyconnect", Net: "tcp", Err: cut}, "check the network"), cut)
		err := fmt.Errorf("%w: %w", context.Canceled, fmt.Errorf("vm_list: %w", inner))
		want := i18n.T("interrupted", "прервано")

		var buf bytes.Buffer
		handleError(output.NewWithWriter(&buf, output.ModeJSON, false), err)
		var got struct {
			Error struct {
				Code    int    `json:"code"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
				Hint    string `json:"hint"`
			} `json:"error"`
		}
		if e := json.Unmarshal(buf.Bytes(), &got); e != nil {
			t.Fatalf("%s: %v: %q", lang, e, buf.String())
		}
		if got.Error.Code != clerrors.ExitInterrupted || got.Error.Reason != clerrors.ReasonInterrupted ||
			got.Error.Message != want || got.Error.Hint != "" {
			t.Errorf("%s: %+v", lang, got.Error)
		}

		buf.Reset()
		handleError(output.NewWithWriter(&buf, output.ModeText, false), err)
		text := buf.String()
		if !strings.Contains(text, want) || strings.Contains(text, "IAM") || strings.Contains(strings.ToLower(text), "proxy") ||
			strings.Contains(text, "прокси") || strings.Contains(text, "interrupt signal") {
			t.Errorf("%s: text %q", lang, text)
		}
	}
}

func TestRun_UsageErrorsExitOne(t *testing.T) {
	for _, args := range [][]string{{"nosuchcmd"}, {"deploy", "--nosuchflag"}, {"mcp", "--toolsets", "vm,nosuch"}} {
		if got := run(args); got != clerrors.ExitGeneral {
			t.Errorf("run(%v) = %d, want %d", args, got, clerrors.ExitGeneral)
		}
	}
}

func TestRun_MissingCredentialsExitTwo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")

	if got := run([]string{"list-zones"}); got != clerrors.ExitAuth {
		t.Errorf("run(list-zones) = %d, want %d", got, clerrors.ExitAuth)
	}
}

// Глобальные --yes/--dry-run доходят до destroy: без терминала и без --yes —
// код 3, --dry-run — 0 и ВМ на месте, --yes проходит гейт (и упирается в
// отсутствие кредов, то есть в облако, а не в подтверждение).
func TestRun_DestroyConfirmationFlags(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })

	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", Status: "running"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"destroy"}, clerrors.ExitConfirmationRequired},
		{[]string{"destroy", "--json"}, clerrors.ExitConfirmationRequired},
		{[]string{"destroy", "--dry-run"}, clerrors.ExitOK},
		{[]string{"destroy", "--dry-run", "--yes"}, clerrors.ExitOK},
		{[]string{"destroy", "-y"}, clerrors.ExitAuth},
	} {
		if got := run(c.args); got != c.want {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.want)
		}
	}
	after, _ := state.Read(state.DefaultPath(dir))
	if a := after.ActiveDeployment(); a == nil || a.VMID != "vm-1" {
		t.Fatalf("deployment changed: %+v", a)
	}
}

// destroy --force still agrees in advance, like --yes, but is hidden from
// help and warns: deploy --force means something else.
func TestRun_DestroyForceIsDeprecatedYes(t *testing.T) {
	setupCLI(t)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", Status: "running"})
	if err := state.Write(state.DefaultPath("."), sf); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "", "destroy", "--force")
	if code != clerrors.ExitAuth { // past the gate, no credentials
		t.Errorf("destroy --force: exit %d, want %d\n%s", code, clerrors.ExitAuth, stderr)
	}
	if !strings.Contains(stderr, "destroy --force is deprecated: use --yes") {
		t.Errorf("no deprecation warning:\n%s", stderr)
	}
	newProcess()
	_, stdout, _ := runCLI(t, "", "destroy", "--help")
	if strings.Contains(stdout, "--force") {
		t.Errorf("--force still in destroy --help:\n%s", stdout)
	}
	newProcess()
	if _, stdout, _ := runCLI(t, "", "deploy", "--help"); !strings.Contains(stdout, "--force") {
		t.Errorf("deploy --force must stay:\n%s", stdout)
	}
}

// Commands that never reach a VM must not leave an SSH key in the project.
func TestRun_CommandsWithoutVMCreateNoSSHKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")

	for _, args := range [][]string{{"list-zones"}, {"list-images"}, {"status"}, {"verify"}, {"init", "--non-interactive"}} {
		_ = run(args)
		if _, err := os.Stat(filepath.Join(dir, ".evo", "id_ed25519")); err == nil {
			t.Fatalf("run(%v) created an SSH key", args)
		}
	}
}

// The global flags live for one run: --profile ci of one CLI run made the
// next test that builds an engine without run() read a profile it never
// saved (TestContract_EveryPreviewPricedOrListed on main under -shuffle).
func TestRun_GlobalFlagsDoNotOutliveTheRun(t *testing.T) {
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	runCLI(t, "", "--profile", "ci", "--json", "--yes", "--dry-run", "--verbose", "--no-color",
		"--project-id", "p-flag", "--region", "z-flag", "version")
	if flagProfile != "" || flagJSON || flagYes || flagDryRun || flagVerbose || flagNoColor {
		t.Errorf("after the run: profile %q json %v yes %v dry-run %v verbose %v no-color %v",
			flagProfile, flagJSON, flagYes, flagDryRun, flagVerbose, flagNoColor)
	}
	// The scope flags too: an engine built after the run took the project of
	// the run before (TestDeployDryRunKeepsV11Keys after
	// TestWhoamiDoctor_ProjectFlag, -shuffle 1792844402228420628 on main).
	if flagProjectID != "" || flagRegion != "" {
		t.Errorf("after the run: project-id %q region %q", flagProjectID, flagRegion)
	}
}
