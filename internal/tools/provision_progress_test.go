// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// evo vm create shows its stages (Caller.Progress) while provision works;
// the answer stays the one MCP gets. Догфуд 1.2.0: «y» — и минуты тишины.
// Все ID и адреса выдуманы.

// progressCloud answers the polls of the new VM with states in turn (the
// last one then forever); onPoll runs at every poll (a Ctrl-C mid-wait).
type progressCloud struct {
	cancelCloud
	mu       sync.Mutex
	states   []string
	polls    int
	onPoll   func(n int)
	rulesAdd int
}

func (c *progressCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	return &provision.VM{ID: "vm-made"}, nil
}
func (c *progressCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.mu.Lock()
	c.polls++
	n := c.polls
	st := c.states[min(n, len(c.states))-1]
	hook := c.onPoll
	c.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return &provision.VM{ID: "vm-made", Status: st}, nil
}
func (*progressCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "in_use"}, nil
}
func (c *progressCloud) AddSecurityGroupRule(_ context.Context, _, _ string, rule provision.SGRule) (*provision.SGRule, error) {
	c.mu.Lock()
	c.rulesAdd++
	c.mu.Unlock()
	rule.ID = "rule-" + rule.PortRange
	return &rule, nil
}

// provisionRun is one confirmed provision on cloud: watched by a person at
// the CLI (a Progress printer in mode) or not (MCP). It returns the answer
// and what the person saw.
func provisionRun(t *testing.T, ctx context.Context, cloud *progressCloud, watched bool, mode output.Mode, timeout time.Duration,
	args map[string]any) (string, bool, string, string) {
	t.Helper()
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: timeout,
				VMProgressInterval: time.Hour, FIPPollInterval: time.Millisecond, FIPRetryInterval: time.Millisecond},
		}, nil
	})
	var seen bytes.Buffer
	if watched {
		ctx = WithCaller(ctx, &Caller{Progress: output.NewWithWriter(&seen, mode, false)})
	}
	call := map[string]any{"name": "web", "confirmed": true}
	for k, v := range args {
		call[k] = v
	}
	text, isErr := r.handlers["provision"](ctx, call)
	return text, isErr, seen.String(), dir
}

// Без терминала: этапы по порядку, отдельными строками, без \r; итог —
// имя, зона, IP и ssh. По-русски — те же этапы словами deploy.
func TestProvisionProgress_StagesInOrder(t *testing.T) {
	for lang, want := range map[string][]string{
		i18n.EN: {"==> Creating VM web…\n", "    Floating IP 203.0.113.9 reserved\n", "    Request accepted: VM web, id vm-made\n",
			"    waiting for the VM… 0s (creating)\n", "    VM web active (", "    Public IP: 203.0.113.9\n",
			"    Opened ports 80,443 in the security group\n", "VM web is ready: zone z, IP 203.0.113.9, ", ". SSH: ssh user1@203.0.113.9\n"},
		i18n.RU: {"==> Создаю ВМ web…\n", "    Зарезервирован floating IP 203.0.113.9\n", "    Запрос принят: ВМ web, id vm-made\n",
			"    ожидание ВМ… 0с (creating)\n", "    ВМ web активна (", "    Внешний IP: 203.0.113.9\n",
			"    Открыты порты 80,443 в группе безопасности\n", "ВМ web готова: зона z, IP 203.0.113.9, ", ". SSH: ssh user1@203.0.113.9\n"},
	} {
		t.Run(lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Cleanup(func() { i18n.SetLang(i18n.EN) })
			cloud := &progressCloud{states: []string{"creating", "creating", "active"}}
			text, isErr, seen, _ := provisionRun(t, context.Background(), cloud, true, output.ModePlain, time.Minute,
				map[string]any{"ports": []any{"80", "443"}})
			if isErr {
				t.Fatalf("answer: %s", text)
			}
			at := 0
			for _, w := range want {
				i := strings.Index(seen[at:], w)
				if i < 0 {
					t.Fatalf("no %q after position %d in:\n%s", w, at, seen)
				}
				at += i + len(w)
			}
			if strings.ContainsAny(seen, "\r\x1b") {
				t.Errorf("control characters without a terminal: %q", seen)
			}
			// The state changes once: one waiting line, not one per poll.
			if n := strings.Count(seen, "(creating)"); n != 1 {
				t.Errorf("%d waiting lines, want 1:\n%s", n, seen)
			}
		})
	}
}

// liveLineClosed: the last draw of the live line is erased (\r, spaces, \r)
// before anything else and before the end: nothing is left on it.
func liveLineClosed(t *testing.T, seen string) string {
	t.Helper()
	i := strings.LastIndex(seen, "\r    waiting for the VM… ")
	if i < 0 {
		t.Fatalf("no live line:\n%q", seen)
	}
	rest := seen[i+1:]
	erase := regexp.MustCompile(`\r {20,}\r`).FindStringIndex(rest)
	if erase == nil {
		t.Fatalf("the live line is not erased:\n%q", seen)
	}
	after := rest[erase[1]:]
	if strings.Contains(after, "\r") || (after != "" && !strings.HasSuffix(after, "\n")) {
		t.Errorf("output after the erase is not whole lines:\n%q", after)
	}
	return after
}

// The live line is closed on every way out of the wait, and after a stop
// with the VM in the cloud the person is told what to do with it. The
// answer is byte for byte the one MCP gets (no Progress), and the VM is in
// the state either way — a client that cancels mid-wait included.
func TestProvisionProgress_LiveLineClosedOnEveryExit(t *testing.T) {
	for name, tc := range map[string]struct {
		states  []string
		timeout time.Duration
		cancel  bool
		isErr   bool
		next    string // the next step said, "" for none
	}{
		"success":        {states: []string{"creating", "active"}, timeout: time.Minute},
		"error":          {states: []string{"creating", "error"}, timeout: time.Minute, isErr: true, next: "VM vm-made is in state error and is billed; check it: evo vm get vm-made; delete it: evo vm delete vm-made\n"},
		"error_creating": {states: []string{"creating", "error_creating"}, timeout: time.Minute, isErr: true, next: "VM vm-made is in state error_creating and is billed; check it: evo vm get vm-made; delete it: evo vm delete vm-made\n"},
		"timeout": {states: []string{"creating"}, timeout: 40 * time.Millisecond, isErr: true,
			next: "VM vm-made is still being created and is billed; check it: evo vm get vm-made; delete it: evo vm delete vm-made; to wait longer next time: CLOUDRU_TIMEOUT (seconds)\n"},
		"ctrl-c": {states: []string{"creating"}, timeout: time.Minute, cancel: true, isErr: true,
			next: "VM vm-made is still being created and is billed; check it: evo vm get vm-made; delete it: evo vm delete vm-made\n"},
	} {
		t.Run(name, func(t *testing.T) {
			run := func(watched bool) (string, bool, string, string) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cloud := &progressCloud{states: tc.states}
				if tc.cancel {
					cloud.onPoll = func(n int) {
						if n == 3 {
							cancel() // Ctrl-C, or an MCP client gone, mid-wait
						}
					}
				}
				return provisionRun(t, ctx, cloud, watched, output.ModeText, tc.timeout, nil)
			}
			text, isErr, seen, dir := run(true)
			if isErr != tc.isErr {
				t.Fatalf("isErr = %v: %s", isErr, text)
			}
			after := liveLineClosed(t, seen)
			if tc.next != "" && !strings.Contains(after, tc.next) {
				t.Errorf("no next step %q after the live line:\n%q", tc.next, after)
			}
			if tc.next == "" && strings.Contains(seen, "evo vm delete") {
				t.Errorf("a next step on success:\n%q", seen)
			}
			if rec := stateHasVM(t, dir, "vm-made"); tc.isErr && rec.Status == "running" {
				t.Errorf("record = %+v", rec)
			}
			mcp, mcpErr, quiet, mcpDir := run(false)
			if quiet != "" {
				t.Errorf("MCP printed progress: %q", quiet)
			}
			if mcp != text || mcpErr != isErr {
				t.Errorf("the answer differs from MCP's:\nCLI %s\nMCP %s", text, mcp)
			}
			stateHasVM(t, mcpDir, "vm-made")
		})
	}
}

// --json: the progress printer is quiet (no line, no erase), as deploy's.
func TestProvisionProgress_JSONQuiet(t *testing.T) {
	cloud := &progressCloud{states: []string{"creating", "active"}}
	text, isErr, seen, _ := provisionRun(t, context.Background(), cloud, true, output.ModeJSON, time.Minute, nil)
	if isErr || seen != "" {
		t.Fatalf("answer %s, progress %q", text, seen)
	}
}

// Повтор после Ctrl-C получает 409: по-русски (это CLI — MCP говорит
// по-английски, D6) подсказка ведёт к evo vm get, ответ MCP прежний.
func TestProvision_NameTaken409HintSpeaksCLI(t *testing.T) {
	taken := func() *nameTakenCloud {
		return &nameTakenCloud{cancelCloud{createErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
			&provision.StatusError{Status: 409, Body: `{"code":"vm_with_this_data_already_exists"}`})}}
	}
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	r, _ := fastRegistry(t, taken(), provisionCfg)
	m, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if h, _ := m["hint"].(string); !strings.Contains(h, "evo vm get web") || !strings.Contains(h, "evo vm delete web") {
		t.Errorf("hint = %q, want evo vm get / evo vm delete: %s", h, text)
	}
	i18n.SetLang(i18n.EN)
	r, _ = fastRegistry(t, taken(), provisionCfg)
	m, _, _ = callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if h, _ := m["hint"].(string); !strings.Contains(h, "vm_get web") || strings.Contains(h, "evo ") {
		t.Errorf("MCP hint = %q, want the tool names as before", h)
	}
}

// *_operation wait_seconds: one live line «ожидание операции… (<status>)»,
// closed when the operation ends; MCP sees nothing.
func TestOpWait_ProgressLine(t *testing.T) {
	defer func(d time.Duration) { opWaitFirstDelay = d }(opWaitFirstDelay)
	opWaitFirstDelay = time.Millisecond
	fetch := func() func(context.Context) (map[string]any, error) {
		n := 0
		return func(context.Context) (map[string]any, error) {
			n++
			if n < 3 {
				return map[string]any{"status": "RUNNING"}, nil
			}
			return map[string]any{"status": "DONE"}, nil
		}
	}
	for _, mode := range []output.Mode{output.ModeText, output.ModePlain} {
		var seen bytes.Buffer
		ctx := WithCaller(context.Background(), &Caller{Progress: output.NewWithWriter(&seen, mode, false)})
		m, err := waitForOpJSON(ctx, 10, fetch())
		if err != nil || m["status"] != "DONE" {
			t.Fatalf("mode %d: %v %v", mode, m, err)
		}
		got := seen.String()
		switch mode {
		case output.ModeText:
			if !strings.HasPrefix(got, "\r    waiting for the operation… 0s (RUNNING)") || !regexp.MustCompile(`\r {20,}\r$`).MatchString(got) {
				t.Errorf("terminal: %q", got)
			}
		default:
			if got != "    waiting for the operation… 0s (RUNNING)\n" {
				t.Errorf("no terminal: %q", got)
			}
		}
	}
	m, err := waitForOpJSON(context.Background(), 10, fetch())
	if err != nil || m["status"] != "DONE" {
		t.Fatalf("MCP: %v %v", m, err)
	}
}
