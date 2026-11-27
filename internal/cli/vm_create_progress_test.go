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
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// evo vm create показывает этапы в stderr (догфуд 1.2.0: «y» — и минуты
// тишины); stdout — ответ инструмента, байт в байт как раньше. stderr здесь
// не терминал: строки, без \r (Windows, перенаправление).

// vmCreateArgs: the fake creates no floating IP (withVMCreateCloud).
var vmCreateArgs = []string{"vm", "create", "web", "--image", "opensearchdashboards", "--meta", "opensearch_host=db.internal",
	"--no-public-ip", "--port", "80"}

// toolAnswer is provision's answer to the same arguments, called as MCP
// calls it (no Caller). Every create makes a password of its own: the
// answer gets the one of the command's create (the first), so that the
// two compare byte for byte.
func toolAnswer(t *testing.T, cloud *vmCreateCloud) string {
	t.Helper()
	tl, _ := newRegistry().Lookup("provision")
	text, isErr := tl.Handler(context.Background(), map[string]any{"name": "web", "image": "opensearchdashboards",
		"image_metadata": map[string]any{"opensearch_host": "db.internal"}, "no_public_ip": true, "ports": []any{"80"}, "confirmed": true})
	if isErr {
		t.Fatalf("tool: %s", text)
	}
	first, last := cloud.creates[0].ImageMetadata["linux_password"], cloud.creates[len(cloud.creates)-1].ImageMetadata["linux_password"]
	if first == "" || !strings.Contains(text, last) {
		t.Fatalf("no generated password in the tool's answer (%q, %q):\n%s", first, last, text)
	}
	return strings.Replace(text, last, first, 1)
}

func TestVMCreate_JSONStdoutIsToolAnswerProgressNowhere(t *testing.T) {
	russian(t)
	setupCLI(t)
	cloud := withVMCreateCloud(t)
	code, out, errOut := runCLI(t, "", append([]string{"--json", "--yes"}, vmCreateArgs...)...)
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	// --json is the tool's answer over MCP, English in Russian too (D6).
	i18n.SetLang(i18n.EN)
	want := toolAnswer(t, cloud) + "\n"
	i18n.SetLang(i18n.RU)
	if out != want {
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
	cloud := withVMCreateCloud(t)
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
	if err := c.show(output.NewWithWriters(&want, &discard, output.ModePlain, false), &want, toolAnswer(t, cloud)); err != nil {
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
	for _, line := range strings.Split(strings.TrimSpace(errOut), "\n") {
		if stage := strings.TrimSpace(line); stage != "" && !hasCyrillicText(stage) {
			t.Errorf("untranslated progress line: %q", stage)
		}
	}
	if strings.ContainsAny(errOut, "\r\x1b") {
		t.Errorf("control characters on a stderr that is not a terminal: %q", errOut)
	}
	if strings.Contains(out, "Создаю") || strings.Contains(out, "==>") {
		t.Errorf("progress on stdout:\n%s", out)
	}
}

// The stages an MCP client that asked for them gets (a progressToken:
// notifications/progress, tools.WithProgress) are the lines evo vm create
// prints on stderr, one for one (owner's decision 23.10, parity of the
// progress): the two surfaces show the same call the same way.
func TestVMCreate_MCPProgressIsTheStderrStages(t *testing.T) {
	russian(t)
	setupCLI(t)
	withVMCreateCloud(t)
	// The durations the stages print are read on clock: a stopped clock
	// prints the same in both runs, whatever second the wall clock turns
	// between them (Windows, «0с» and «1с»).
	stopped := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return stopped }
	t.Cleanup(func() { clock.Now = old })
	code, out, errOut := runCLI(t, "", append([]string{"--yes"}, vmCreateArgs...)...)
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	// The stages start at the first step; the line of the consent before
	// it is the command's own (MCP consents with confirmed).
	var cli []string
	for _, line := range strings.Split(errOut, "\n") {
		if line = strings.TrimSpace(line); line != "" && (len(cli) > 0 || strings.HasPrefix(line, "==> ")) {
			cli = append(cli, line)
		}
	}
	var mcp []string
	ctx := tools.WithProgress(context.Background(), func(line string) { mcp = append(mcp, line) })
	tl, _ := newRegistry().Lookup("provision")
	if text, isErr := tl.Handler(ctx, map[string]any{"name": "web", "image": "opensearchdashboards",
		"image_metadata": map[string]any{"opensearch_host": "db.internal"}, "no_public_ip": true, "ports": []any{"80"}, "confirmed": true}); isErr {
		t.Fatalf("tool: %s", text)
	}
	if len(mcp) == 0 || strings.Join(mcp, "\n") != strings.Join(cli, "\n") {
		t.Errorf("MCP progress is not the stderr stages:\nMCP:\n%s\nstderr:\n%s", strings.Join(mcp, "\n"), strings.Join(cli, "\n"))
	}
}

type unattachedIPCloud struct{ *vmCreateCloud }

func (c unattachedIPCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return c.fip, nil
}
func (c unattachedIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{Interfaces: []provision.VMInterface{{ID: vcSubnet, Primary: true}}}, nil
}
func (c unattachedIPCloud) AttachFloatingIP(context.Context, string, string, string, string) (*provision.FloatingIP, error) {
	return nil, &provision.StatusError{Status: 422, Body: "fixture denied"}
}

func TestVMCreate_UnattachedIPLanguage(t *testing.T) {
	for _, mode := range []string{"ru-text", "json", "stdio", "http"} {
		t.Run(mode, func(t *testing.T) {
			setupCLI(t)
			cloud := withVMCreateCloud(t)
			cloud.fip = &provision.FloatingIP{ID: "00000000-0000-0000-0000-00000000b004", IPAddress: "203.0.113.7", State: "available"}
			base := toolEngine
			toolEngine = func(opts map[string]any) (*engine.Engine, error) {
				e, err := base(opts)
				if err == nil {
					e.Cloud, e.Timings = unattachedIPCloud{cloud}, parityTimings
				}
				return e, err
			}

			var msg, hint string
			if mode == "stdio" || mode == "http" {
				servers := parityServers(t)
				args := map[string]any{"name": "web", "image": "Ubuntu-24.04", "confirmed": true}
				var reply map[string]any
				if mode == "stdio" {
					reply = stdioDetails(t, servers.stdio, "provision", args)
				} else {
					reply = httpDetails(t, servers.http, "provision", args)
				}
				if reply["status"] != "partial" || reply["reason"] != clerrors.CodeAPIError {
					t.Fatalf("expected partial creation: %v", reply)
				}
				msg, _ = reply["error"].(string)
				hint, _ = reply["hint"].(string)
				if summary, _ := reply["message"].(string); !strings.Contains(summary, "billed") || hasCyrillicText(summary) {
					t.Errorf("partial machine summary: %q", summary)
				}
			} else {
				ru(t)
				args := []string{"vm", "create", "web", "--image", "Ubuntu-24.04", "--yes"}
				if mode == "json" {
					args = append(args, "--json")
				}
				code, _, errOut := runCLI(t, "", args...)
				if code != clerrors.ExitAPI {
					t.Fatalf("expected partial creation, exit %d: %s", code, errOut)
				}
				if mode == "json" {
					e := lastJSONError(t, errOut)
					if e.Reason != clerrors.CodeAPIError {
						t.Fatalf("wrong reason: %+v", e)
					}
					msg, hint = e.Message, e.Hint
				} else {
					msg, hint = personText(errOut)
				}
			}
			if !strings.Contains(msg, vcVM) || !strings.Contains(msg, "fixture denied") || hint == "" {
				t.Fatalf("missing VM, service cause or next action: %q / %q", msg, hint)
			}
			if mode == "ru-text" {
				// The localized cause alone cannot translate the product's frame.
				beforeCause, _, _ := strings.Cut(msg, "(")
				if !hasCyrillicText(beforeCause) || !hasCyrillicText(hint) || strings.Contains(hint, "Attach a free") {
					t.Errorf("untranslated partial error or hint: %q / %q", msg, hint)
				}
			} else if hasCyrillicText(msg+hint) || !strings.Contains(msg, "not attached") {
				t.Errorf("machine error or hint is not English: %q / %q", msg, hint)
			}
		})
	}
}
