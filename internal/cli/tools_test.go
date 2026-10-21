// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// execCLI runs the CLI with its output captured and returns stdout, stderr
// and the exit code. There are no credentials and no terminal: nothing
// reaches the cloud, and consent cannot be given at a prompt.
func execCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })

	root := newRootCmd()
	root.SetArgs(args)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	_, err := root.ExecuteContextC(context.Background())
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return out.String(), errOut.String(), clerrors.ExitCode(err)
}

func TestToolsList_JSONHasEveryTool(t *testing.T) {
	out, _, code := execCLI(t, "tools", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var list []toolInfo
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(list) != 154 {
		t.Errorf("%d tools, want 154 (146 + vm_list + image_list + 3 k8s addon tools + marketplace_list + whoami, doctor)", len(list))
	}
	for _, tl := range list {
		if tl.Group == "" || tl.Description == "" {
			t.Errorf("%s: empty group or description", tl.Name)
		}
	}
}

// The list goes by group, then by name: in registration order a group was
// scattered over 38 runs.
func TestToolsList_SortedByGroupThenName(t *testing.T) {
	out, _, code := execCLI(t, "tools", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var list []toolInfo
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for i := 1; i < len(list); i++ {
		a, b := list[i-1], list[i]
		if a.Group > b.Group || (a.Group == b.Group && a.Name >= b.Name) {
			t.Fatalf("%s/%s before %s/%s", a.Group, a.Name, b.Group, b.Name)
		}
	}
}

// The first sentence does not end at an abbreviation's dot: k8s_get was
// cut to "… (full details incl.".
func TestSummary_SkipsAbbreviations(t *testing.T) {
	for in, want := range map[string]string{
		"Get a cluster (full details incl. status). Read-only.": "Get a cluster (full details incl. status).",
		"Disks, NICs etc. are kept. Then more.":                 "Disks, NICs etc. are kept.",
		"Pass a flavor, e.g. low-1-2. Optional.":                "Pass a flavor, e.g. low-1-2.",
		"That is, i.e. this. Next.":                             "That is, i.e. this.",
		"One. Two.":                                             "One.",
		"Ends with pincl. Next.":                                "Ends with pincl.",
	} {
		if got := summary(in, 200); got != want {
			t.Errorf("summary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolsList_Group(t *testing.T) {
	out, _, code := execCLI(t, "tools", "list", "--group", "disk")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "disk_delete") || strings.Contains(out, "pg_list") {
		t.Errorf("group filter: %s", out)
	}
	if !strings.HasPrefix(out, "NAME") {
		t.Errorf("table header missing: %q", out)
	}
	if _, _, code := execCLI(t, "tools", "list", "--group", "nope"); code != clerrors.ExitGeneral {
		t.Errorf("unknown group: exit %d, want 1", code)
	}
}

func TestToolsDescribe(t *testing.T) {
	out, _, code := execCLI(t, "tools", "describe", "disk_delete")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "disk_id") || !strings.Contains(out, "required") {
		t.Errorf("arguments missing: %s", out)
	}
	if strings.Contains(out, "\nconfirmed") {
		t.Errorf("confirmed must not be offered as an argument: %s", out)
	}

	out, _, _ = execCLI(t, "tools", "describe", "disk_delete", "--json")
	var info toolInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil || info.InputSchema == nil || !info.Mutating {
		t.Errorf("describe --json: %v %+v", err, info)
	}

	if _, _, code := execCLI(t, "tools", "describe", "nope"); code != clerrors.ExitGeneral {
		t.Errorf("unknown tool: exit %d, want 1", code)
	}
}

// A gated tool without a terminal and without --yes stops at the preview
// with exit code 3; --dry-run shows the preview and exits 0; --yes passes
// the gate and goes on to the cloud (here: no credentials, exit 2).
func TestToolsCall_Confirmation(t *testing.T) {
	out, errOut, code := execCLI(t, "tools", "call", "disk_delete", "--arg", "disk_id=X")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("no tty: exit %d, want 3 (%s)", code, errOut)
	}
	if out != "" || !strings.Contains(errOut, "disk_delete permanently destroys disk X") {
		t.Errorf("preview should go to stderr: out=%q err=%q", out, errOut)
	}

	out, _, code = execCLI(t, "tools", "call", "disk_delete", "--arg", "disk_id=X", "--dry-run")
	if code != 0 || !strings.Contains(out, "Dry run") {
		t.Errorf("dry run: exit %d, out %q", code, out)
	}

	out, _, code = execCLI(t, "tools", "call", "disk_delete", "--arg", "disk_id=X", "--json")
	if code != clerrors.ExitConfirmationRequired || !strings.Contains(out, `"confirmation_required"`) {
		t.Errorf("--json: exit %d, out %q", code, out)
	}

	out, _, code = execCLI(t, "tools", "call", "disk_delete", "--arg", "disk_id=X", "--yes")
	if code != clerrors.ExitAuth || !strings.Contains(out, "AUTH_REQUIRED") {
		t.Errorf("--yes: exit %d, want 2 (no credentials), out %q", code, out)
	}
}

// s3_bucket_create asks for confirmation (its own gate since the MCP QA).
func TestToolsCall_MutatingWithoutGateAsks(t *testing.T) {
	_, errOut, code := execCLI(t, "tools", "call", "s3_bucket_create", "--arg", "bucket=bk1")
	if code != clerrors.ExitConfirmationRequired || !strings.Contains(errOut, `bucket "bk1"`) {
		t.Errorf("exit %d, err %q", code, errOut)
	}
}

func TestToolsCall_ToolErrorExitCodes(t *testing.T) {
	// The tool needs an engine and there are no credentials: it answers
	// with an AUTH_REQUIRED error, which keeps its exit code 2.
	out, _, code := execCLI(t, "tools", "call", "vm_get", "--arg", "name=x")
	if code != clerrors.ExitAuth {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(out, `"error"`) {
		t.Errorf("tool JSON should be printed as is: %q", out)
	}
	for text, want := range map[string]int{
		`{"error":"disk abc not found"}`:                clerrors.ExitNotFound,
		`{"error":"API_ERROR: HTTP 500"}`:               clerrors.ExitAPI,
		`{"error":"API_UNREACHABLE: connection reset"}`: clerrors.ExitGeneral,
		`{"error":"VM_NOT_FOUND: resource not found"}`:  clerrors.ExitNotFound,
		`{"error":"AUTH_FAILED: bad key"}`:              clerrors.ExitAuth,
		`{"error":"'disk_id' is required"}`:             clerrors.ExitAPI,
		// The tool refused its arguments before calling the cloud: bad input,
		// not an API error.
		`{"error":"USAGE: 'disk_id' is required"}`: clerrors.ExitGeneral,
		// Missing S3 keys are configuration, like a missing Cloud.ru key.
		`{"error":"CONFIG_INVALID: S3 credentials not configured"}`: clerrors.ExitAuth,
		`not json`: clerrors.ExitAPI,
	} {
		if got := clerrors.ExitCode(toolError("x", text, callHints("x"))); got != want {
			t.Errorf("%s: exit %d, want %d", text, got, want)
		}
	}
}

func TestToolsCall_UsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"tools", "call", "nope"},
		{"tools", "call", "disk_delete", "--arg", "id=X"},
		{"tools", "call", "disk_delete"},
		{"tools", "call", "disk_delete", "--arg", "disk_id=X", "--arg", "confirmed=true"},
		{"tools", "call", "disk_delete", "--args-json", `{"disk_id":"X","confirmed":true}`},
		{"tools", "call", "disk_resize", "--arg", "disk_id=X", "--arg", "size=big"},
		{"tools", "call", "list_zones", "--wait"},
	} {
		if _, _, code := execCLI(t, args...); code != clerrors.ExitGeneral {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
}

func TestBuildToolArgs_SourcesAndCoercion(t *testing.T) {
	reg := tools.New(nil)
	tl, _ := reg.Lookup("open_ports")
	file := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(file, []byte(`{"security_group":"from-file","cidr":"10.0.0.0/8"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := buildToolArgs(tl, file, `{"security_group":"from-json"}`, []string{"ports=80, 443", "protocol=udp"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"security_group": "from-json",
		"cidr":           "10.0.0.0/8",
		"ports":          []any{float64(80), float64(443)},
		"protocol":       "udp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestCoerceArg(t *testing.T) {
	cases := []struct {
		p    param
		in   string
		want any
	}{
		{param{Type: "string"}, "a,b", "a,b"},
		{param{Type: "integer"}, "42", float64(42)},
		{param{Type: "number"}, "1.5", 1.5},
		{param{Type: "boolean"}, "true", true},
		{param{Type: "array", ItemType: "string"}, "a, b", []any{"a", "b"}},
		{param{Type: "array"}, `[1,"x"]`, []any{float64(1), "x"}},
		{param{Type: "object"}, `{"k":"v"}`, map[string]any{"k": "v"}},
	}
	for _, c := range cases {
		got, err := coerceArg(c.p, c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("coerceArg(%s, %q) = %#v, %v; want %#v", c.p.Type, c.in, got, err, c.want)
		}
	}
	for _, c := range []struct{ typ, in string }{{"integer", "1.5"}, {"boolean", "maybe"}, {"object", "x"}} {
		if _, err := coerceArg(param{Type: c.typ}, c.in); err == nil {
			t.Errorf("coerceArg(%s, %q): want an error", c.typ, c.in)
		}
	}
}

func TestApplyWait(t *testing.T) {
	reg := tools.New(nil)
	op, _ := reg.Lookup("pg_operation")
	args := map[string]any{}
	if err := applyWait(op, args); err != nil || args["wait_seconds"] != float64(tools.OpWaitMaxSeconds) {
		t.Errorf("pg_operation: %v %v", err, args)
	}
	args = map[string]any{"wait_seconds": float64(10)}
	_ = applyWait(op, args)
	if args["wait_seconds"] != float64(10) {
		t.Errorf("explicit wait_seconds overridden: %v", args)
	}
}

// A tool error without a hint prints no "hint" key in --json, not "hint":""
// (QA r4 Q10); the other keys stay.
func TestToolError_JSONWithoutHint(t *testing.T) {
	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false),
		toolError("dns_zones", `{"status":"error","error":"zone not found"}`, callHints("dns_zones")))
	want := `{"error":{"code":4,"reason":"NOT_FOUND","message":"dns_zones: zone not found"}}` + "\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
