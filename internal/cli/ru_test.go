// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Тесты русского вывода: по умолчанию CLI говорит по-русски, TestMain же
// включает английский для остальных тестов пакета.

func russian(t *testing.T) {
	t.Helper()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
}

func TestRussian_RootHelp(t *testing.T) {
	russian(t)
	setupCLI(t)
	code, stdout, _ := runCLI(t, "", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"Коды выхода:\n  0    успех",
		"Использование:\n  evo [команда]",
		"Развёртывание compose-проекта:",
		"  deploy      Развернуть docker-compose проект на ВМ Cloud.ru\n",
		"  vm          Виртуальные машины\n",
		"  help        Справка по любой команде\n",
		"  completion  Скрипт автодополнения для оболочки\n",
		"Флаги:",
		"  -h, --help                Справка по evo\n",
		"Справка по команде: \"evo [команда] --help\".",
		"EVO_LANG=en",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root help lacks %q:\n%s", want, stdout)
		}
	}
	for _, en := range []string{"Usage:", "Available Commands:", "Flags:", "CLI and MCP server", "help for"} {
		if strings.Contains(stdout, en) {
			t.Errorf("root help still has %q:\n%s", en, stdout)
		}
	}
}

func TestRussian_TreeHelp(t *testing.T) {
	russian(t)
	setupCLI(t)
	code, stdout, _ := runCLI(t, "", "vm", "--help")
	if code != 0 || !strings.Contains(stdout, "  list        Список ВМ проекта") ||
		!strings.Contains(stdout, "Использование:\n  evo vm [флаги]\n  evo vm [команда]") {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
	// A leaf of vm or disk is Russian throughout: summary, long help, flags.
	code, stdout, _ = runCLI(t, "", "vm", "delete", "--help")
	if code != 0 || !strings.HasPrefix(stdout, toolShortRU["destroy"]+".\n\n"+toolLongRU["destroy"]+"\n\nВызывает инструмент destroy") ||
		strings.Contains(stdout, "Permanently delete a VM") || strings.Contains(stdout, "confirmed") ||
		!strings.Contains(stdout, "Спрашивает подтверждение") ||
		!strings.Contains(stdout, "--release-ip   Освободить и floating IP ВМ. Выключить: --release-ip=false (по умолчанию true)\n") {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
	// The other trees are Russian throughout too: no English description
	// of the tool, no English flag.
	code, stdout, _ = runCLI(t, "", "vpc", "delete", "--help")
	if code != 0 || !strings.HasPrefix(stdout, toolShortRU["vpc_delete"]+".\n\n"+toolLongRU["vpc_delete"]+"\n\nВызывает инструмент vpc_delete") ||
		strings.Contains(stdout, "на английском") || strings.Contains(stdout, "Delete a VPC") {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
	code, stdout, _ = runCLI(t, "", "k8s", "kubeconfig", "--help")
	if code != 0 || strings.Contains(stdout, "Embed a static bearer token") ||
		!strings.Contains(stdout, "Встроить статический токен, чтобы kubectl работал без плагина cloudlogin. Выключить: --embed-token=false (по умолчанию true)\n") {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
}

// Every tree is Russian throughout: every command has a long help in
// toolLongRU, every flag a text in flagShortRU. The texts speak of the CLI:
// no confirmed=true (the CLI asks or takes --yes), no tool names (evo image
// list, not image_list) except as 'evo tools call <tool>' for a tool without
// a command. No entry is left without a flag.
func TestTrees_RussianFlags(t *testing.T) {
	reg := newRegistry()
	toolNames := map[string]bool{}
	for _, tl := range reg.Tools() {
		if strings.Contains(tl.Name, "_") {
			toolNames[tl.Name] = true
		}
	}
	mcpOnly := func(key, s string) {
		t.Helper()
		if strings.Contains(s, "confirmed") {
			t.Errorf("%s mentions confirmed: %q", key, s)
		}
		s = toolsCallRE.ReplaceAllString(s, "evo tools call")
		for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if toolNames[w] {
				t.Errorf("%s names the tool %s instead of its command: %q", key, w, s)
			}
		}
	}
	usedLong, usedFlag := map[string]bool{}, map[string]bool{}
	for _, g := range treeGroups {
		for _, c := range g.Cmds {
			tl, _ := reg.Lookup(c.Tool)
			if s := toolLongRU[c.Tool]; s == "" {
				t.Errorf("%s %s: no Russian long help for %s in toolLongRU", g.Name, c.Verb, c.Tool)
			} else {
				mcpOnly(c.Tool, s)
			}
			usedLong[c.Tool] = true
			for _, p := range c.flagArgs(tl) {
				key := c.Tool + "." + p.Name
				usedFlag[key] = true
				if s := flagShortRU[key]; s == "" {
					t.Errorf("%s %s --%s: no Russian help for %s in flagShortRU", g.Name, c.Verb, c.flagName(p.Name), key)
				} else {
					mcpOnly(key, s)
				}
			}
		}
	}
	for k := range toolLongRU {
		if !usedLong[k] {
			t.Errorf("toolLongRU: %s is no tree command", k)
		}
	}
	for k := range flagShortRU {
		if !usedFlag[k] {
			t.Errorf("flagShortRU: %s is no flag of a tree command", k)
		}
	}
}

// Every tree command has a Russian summary: a new tool in a tree must get one.
func TestTrees_RussianShorts(t *testing.T) {
	for _, g := range treeGroups {
		if treeGroupShortRU[g.Name] == "" {
			t.Errorf("tree %s: no Russian summary in treeGroupShortRU", g.Name)
		}
		for _, c := range g.Cmds {
			ru := toolShortRU[c.Tool]
			switch {
			case ru == "":
				t.Errorf("%s %s: no Russian summary for tool %s in toolShortRU", g.Name, c.Verb, c.Tool)
			case strings.HasSuffix(ru, "."):
				t.Errorf("%s: a summary does not end with a period: %q", c.Tool, ru)
			}
		}
	}
}

func TestRussian_WhoamiAndHint(t *testing.T) {
	russian(t)
	setupCLI(t)

	// Нет ключа: сообщение и подсказка по-русски, код 2.
	code, _, stderr := runCLI(t, "", "whoami")
	want := "Ошибка: не задан ключ доступа Cloud.ru\nВыполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET"
	if code != clerrors.ExitAuth || !strings.HasPrefix(stderr, want) {
		t.Errorf("exit %d, stderr:\n%s", code, stderr)
	}

	saveProfile(t, "default", "p-1")
	newProcess()
	code, stdout, stderr := runCLI(t, "", "whoami")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"Профиль:     default\n",
		"Key ID:      " + config.MaskKeyID(fakeKeyID) + " (источник: profile)\n",
		"Проект:      p-1 (источник: profile)\n",
		"Проекты:     ключ видит 1 проект\n",
		"Токен:       действует до ",
		"только что выдан IAM",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("whoami lacks %q:\n%s", want, stdout)
		}
	}
}

func TestRussian_Doctor(t *testing.T) {
	russian(t)
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	code, stdout, stderr := runCLI(t, "", "doctor")
	t.Logf("\n%s%s", stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Статус", "Проверка", "Подробности", "ок ", "профиль default; key_id из profile; project_id из profile",
		"доступен (", " мс)", "токен выдан, действует ещё "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("doctor lacks %q", want)
		}
	}
	// The API root answers 404: «HTTP 404» read as an error to a person,
	// so the code is only in --verbose and --json (v1.0.1).
	if strings.Contains(stdout, "HTTP ") {
		t.Errorf("the status code is shown without --verbose:\n%s", stdout)
	}
	newProcess()
	_, stdout, _ = runCLI(t, "", "--verbose", "doctor")
	if !strings.Contains(stdout, "доступен (HTTP ") {
		t.Errorf("--verbose lacks the status code:\n%s", stdout)
	}
	// JSON keeps the stable statuses.
	newProcess()
	_, stdout, _ = runCLI(t, "", "--json", "doctor")
	if !strings.Contains(stdout, `"status": "ok"`) || !strings.Contains(stdout, `"http_status": `) {
		t.Errorf("JSON statuses changed:\n%s", stdout)
	}
}

// The symbolic code is in "reason", whatever the language of the message.
func TestJSONError_Reason(t *testing.T) {
	russian(t)
	setupCLI(t)
	for _, c := range []struct {
		args   []string
		code   int
		reason string
	}{
		{[]string{"--json", "whoami"}, clerrors.ExitAuth, clerrors.CodeAuthRequired},
		{[]string{"--json", "nosuchcmd"}, clerrors.ExitGeneral, clerrors.CodeUsage},
		{[]string{"--json", "verify"}, clerrors.ExitNotFound, clerrors.CodeVMNotFound},
	} {
		code, _, stderr := runCLI(t, "", c.args...)
		var body struct {
			Error struct {
				Code    int    `json:"code"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
				Hint    string `json:"hint"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(lastLine(stderr)), &body); err != nil {
			t.Fatalf("%v: stderr is not JSON: %q", c.args, stderr)
		}
		if code != c.code || body.Error.Code != c.code || body.Error.Reason != c.reason || body.Error.Message == "" {
			t.Errorf("%v: exit %d, %+v", c.args, code, body.Error)
		}
	}

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false), errPlain("boom"))
	if !strings.Contains(buf.String(), `"reason":"ERROR"`) {
		t.Errorf("plain error: %s", buf.String())
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestRussian_UsageErrors(t *testing.T) {
	russian(t)
	for in, want := range map[string]string{
		`unknown command "dploy" for "evo"` + "\n\nDid you mean this?\n\tdeploy\n": `неизвестная команда "dploy" для "evo"; может быть, deploy?`,
		"unknown flag: --nosuch":            "неизвестный флаг: --nosuch",
		"unknown shorthand flag: 'q' in -q": "неизвестный флаг: -q в -q",
		"flag needs an argument: --tail":    "флагу --tail нужно значение",
		`invalid argument "x" for "-n, --tail" flag: strconv.ParseInt: parsing "x": invalid syntax`: `недопустимое значение "x" флага "-n, --tail": нужно число`,
		"accepts 1 arg(s), received 0": "нужно аргументов: 1, передано: 0",
		"something else":               "something else",
	} {
		if got := usageMessage(in); got != want {
			t.Errorf("usageMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

// evo mcp speaks English from the moment it serves: the engine's messages in
// tool results are read by agents. The help of the command stays Russian.
func TestMCP_ForcesEnglish(t *testing.T) {
	russian(t)
	setupCLI(t)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_zones"}}` + "\n"
	code, stdout, _ := runCLI(t, call, "mcp")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if i18n.Lang() != i18n.EN {
		t.Errorf("language after 'evo mcp' = %s, want en", i18n.Lang())
	}
	if !strings.Contains(stdout, "Cloud.ru credentials not set") || strings.Contains(stdout, "не задан") {
		t.Errorf("MCP answer is not English:\n%s", stdout)
	}
}
