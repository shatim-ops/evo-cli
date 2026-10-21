// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// evo vm create показывает этапы в stderr (догфуд 1.2.0: «y» — и минуты
// тишины); stdout — ответ инструмента, байт в байт как раньше. stderr здесь
// не терминал: строки, без \r (Windows, перенаправление).

// vmCreateArgs: the fake creates no floating IP (withVMCreateCloud).
var vmCreateArgs = []string{"vm", "create", "web", "--image", "opensearchdashboards", "--meta", "opensearch_host=db.internal",
	"--no-public-ip", "--port", "80"}

// toolAnswer is provision's answer to the same arguments, called as MCP
// calls it (no Caller).
func toolAnswer(t *testing.T) string {
	t.Helper()
	tl, _ := newRegistry().Lookup("provision")
	text, isErr := tl.Handler(context.Background(), map[string]any{"name": "web", "image": "opensearchdashboards",
		"image_metadata": map[string]any{"opensearch_host": "db.internal"}, "no_public_ip": true, "ports": []any{"80"}, "confirmed": true})
	if isErr {
		t.Fatalf("tool: %s", text)
	}
	return text
}

func TestVMCreate_JSONStdoutIsToolAnswerProgressNowhere(t *testing.T) {
	russian(t)
	setupCLI(t)
	withVMCreateCloud(t)
	code, out, errOut := runCLI(t, "", append([]string{"--json", "--yes"}, vmCreateArgs...)...)
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	if want := toolAnswer(t) + "\n"; out != want {
		t.Errorf("stdout is not the tool's answer:\n got %q\nwant %q", out, want)
	}
	// --json: no progress at all, as deploy --json; stderr holds JSON lines
	// only.
	for _, line := range strings.Split(strings.TrimSpace(errOut), "\n") {
		if line != "" && !json.Valid([]byte(line)) {
			t.Errorf("stderr line is not JSON: %q", line)
		}
	}
	if strings.Contains(errOut, "Создаю ВМ") {
		t.Errorf("progress with --json:\n%s", errOut)
	}
}

func TestVMCreate_TextStagesOnStderrTableOnStdout(t *testing.T) {
	russian(t)
	setupCLI(t)
	withVMCreateCloud(t)
	code, out, errOut := runCLI(t, "", append([]string{"--yes"}, vmCreateArgs...)...)
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	// stdout: the table of the answer, as the command printed it before.
	var c treeCmd
	for _, g := range treeGroupsOf(newRegistry()) {
		for _, tc := range g.Cmds {
			if tc.Tool == "provision" {
				c = tc
			}
		}
	}
	var want, discard bytes.Buffer
	if err := c.show(output.NewWithWriters(&want, &discard, output.ModePlain, false), &want, toolAnswer(t)); err != nil {
		t.Fatal(err)
	}
	if out != want.String() {
		t.Errorf("stdout is not the answer's table:\n got %q\nwant %q", out, want.String())
	}
	at := 0
	for _, w := range []string{"==> Создаю ВМ web…\n", "    Запрос принят: ВМ web, id " + vcVM + "\n", "    ВМ web активна (",
		"    Открыты порты 80 в группе безопасности\n", "ВМ web готова: зона ", ", без внешнего IP, "} {
		i := strings.Index(errOut[at:], w)
		if i < 0 {
			t.Fatalf("no %q after position %d in stderr:\n%s", w, at, errOut)
		}
		at += i + len(w)
	}
	if strings.ContainsAny(errOut, "\r\x1b") {
		t.Errorf("control characters on a stderr that is not a terminal: %q", errOut)
	}
	if strings.Contains(out, "Создаю") || strings.Contains(out, "==>") {
		t.Errorf("progress on stdout:\n%s", out)
	}
}
