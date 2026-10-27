// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	stderrors "errors"
	"io"
	"os"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func TestHTTPAuthToken(t *testing.T) {
	cases := []struct {
		addr, mode, env string
		wantToken       string
		wantCode        string // "" = no error
	}{
		// A token shorter than minBearerTokenLen guards a server open to
		// the network: refused there (D45); on loopback it starts, with
		// the warning (bearerTokenWarning).
		{":8080", "bearer", "tok", "", clerrors.CodeConfigInvalid},
		{"0.0.0.0:8080", "bearer", "tok", "", clerrors.CodeConfigInvalid},
		{"[::]:8080", "bearer", "tok", "", clerrors.CodeConfigInvalid},
		{"10.0.0.5:8080", "bearer", strings.Repeat("x", 31), "", clerrors.CodeConfigInvalid},
		{"127.0.0.1:8080", "bearer", "tok", "tok", ""},
		{"localhost:8080", "bearer", "tok", "tok", ""},
		{"[::1]:8080", "bearer", "tok", "tok", ""},
		{":8080", "bearer", strings.Repeat("x", 32), strings.Repeat("x", 32), ""},
		{"0.0.0.0:8080", "bearer", strings.Repeat("x", 64), strings.Repeat("x", 64), ""},
		{":8080", "bearer", "", "", clerrors.CodeConfigInvalid},
		{"127.0.0.1:8080", "none", "", "", ""},
		{"localhost:8080", "none", "", "", ""},
		{"[::1]:8080", "none", "", "", ""},
		{"127.0.0.1:8080", "none", "tok", "", ""}, // none ignores the token
		{":8080", "none", "", "", clerrors.CodeUsage},
		{"0.0.0.0:8080", "none", "", "", clerrors.CodeUsage},
		{"10.0.0.5:8080", "none", "", "", clerrors.CodeUsage},
		{"8080", "bearer", "tok", "", clerrors.CodeUsage},
		{":8080", "oauth", "tok", "", clerrors.CodeUsage},
	}
	for _, c := range cases {
		got, err := httpAuthToken(c.addr, c.mode, c.env)
		if c.wantCode == "" {
			if err != nil || got != c.wantToken {
				t.Errorf("httpAuthToken(%q, %q): %q, %v", c.addr, c.mode, got, err)
			}
			continue
		}
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != c.wantCode {
			t.Errorf("httpAuthToken(%q, %q): err %v, want code %s", c.addr, c.mode, err, c.wantCode)
		}
	}
}

// The server must refuse to start, not listen, on bad combinations.
func TestRun_MCPHTTPRefusesToStart(t *testing.T) {
	t.Setenv("MCP_BEARER_TOKEN", "")
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"mcp", "--http", ":8080", "--auth", "none"}, clerrors.ExitGeneral},
		{[]string{"mcp", "--http", "127.0.0.1:0"}, clerrors.ExitAuth}, // bearer by default, no token
		{[]string{"mcp", "--auth", "none"}, clerrors.ExitGeneral},     // --auth without --http
	}
	for _, c := range cases {
		if got := run(c.args); got != c.want {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.want)
		}
	}
}

// --allow-project refuses every other project, the unset one included
// (QA P4); without the flag nothing is refused (D12: compatible default).
func TestCheckAllowedProject(t *testing.T) {
	if err := checkAllowedProject(nil, "p-any"); err != nil {
		t.Errorf("no allowlist: %v", err)
	}
	allowed := []string{"p-a", "p-b"}
	if err := checkAllowedProject(allowed, "p-b"); err != nil {
		t.Errorf("allowed project: %v", err)
	}
	for _, p := range []string{"p-c", ""} {
		err := checkAllowedProject(allowed, p)
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid {
			t.Errorf("project %q: %v, want CONFIG_INVALID", p, err)
		}
	}
}

// A project_id argument goes through the factory as the ProjectID override:
// with --allow-project, deploy and provision in another project are refused
// before reaching the cloud (ревью Qwen на #55: «аргумент не проверяется»).
func TestMCPFactory_AllowProjectCoversToolArguments(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-a")
	reg := tools.New(mcpEngineFactory([]string{"p-a"}))
	for _, name := range []string{"provision", "deploy", "billing_consumption", "billing_finops"} {
		tl, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		args := map[string]any{"project_id": "p-b", "confirmed": true}
		switch name {
		case "provision":
			args["name"] = "x"
		case "billing_consumption":
			// billing_*: the consumption of another project was read with
			// --allow-project p-a (architecture review 20.10).
			args["start"] = "2026-10-01"
			args["end"] = "2026-10-02"
		}
		text, isErr := tl.Handler(context.Background(), args)
		if !isErr || !strings.Contains(text, "p-b") || !strings.Contains(text, "not allowed") {
			t.Errorf("%s with project_id p-b = %s (error %v), want a refusal naming p-b", name, text, isErr)
		}
	}
}

// Финальный аудит, этап 1: stdout — канал JSON-RPC; строки хода движка
// (освобождённый floating IP у destroy vm_id=, FIP после загрузки у
// provision) шли туда. Движок фабрики MCP печатает в stderr.
func TestMCPFactory_EngineDoesNotPrintToStdout(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-a")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w // before the engine: a printer keeps the stdout it was made with
	eng, err := mcpEngineFactory(nil)(nil)
	if err != nil {
		os.Stdout = old
		t.Fatal(err)
	}
	eng.Printer.Step(1, 1, "release")
	eng.Printer.StepDone("Floating IP 1.2.3.4 released")
	eng.Printer.Warning("w")
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Errorf("engine printed to stdout: %q", out)
	}
}

// SEC-9: предупреждение о коротком токене — до 32 символов (было 16);
// сервер всё равно запускается.
func TestBearerTokenWarning(t *testing.T) {
	for _, c := range []struct {
		token string
		warn  bool
	}{
		{"", false},
		{"short", true},
		{strings.Repeat("x", 16), true},
		{strings.Repeat("x", 31), true},
		{strings.Repeat("x", 32), false},
		{strings.Repeat("x", 64), false},
	} {
		w := bearerTokenWarning(c.token)
		if (w != "") != c.warn {
			t.Errorf("len %d: warning %q, want warn=%v", len(c.token), w, c.warn)
		}
		if c.warn && (!strings.Contains(w, "32") || !strings.Contains(w, "MCP_BEARER_TOKEN")) {
			t.Errorf("len %d: %q", len(c.token), w)
		}
	}
}
