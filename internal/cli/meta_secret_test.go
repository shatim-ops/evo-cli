// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// --meta поле=- и --meta поле=@файл: пароль не попадает в argv и историю
// оболочки. Завершающий перевод строки отрезается, остальное — как есть.

func TestVMCreate_MetaFromStdinAndFile(t *testing.T) {
	setupCLI(t)
	cloud := withVMCreateCloud(t)
	pass := filepath.Join(t.TempDir(), "os.pass")
	if err := os.WriteFile(pass, []byte("@file-secret=x\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "stdin secret\n", "--yes", "vm", "create", "dash", "--image", "OpensearchDashboards", "--no-public-ip",
		"--meta", "opensearch_host=-", "--meta", "opensearch_password=@"+pass)
	if code != 0 || len(cloud.creates) != 1 {
		t.Fatalf("exit %d, creates %d:\n%s\n%s", code, len(cloud.creates), out, errOut)
	}
	meta := cloud.creates[0].ImageMetadata
	if meta["opensearch_host"] != "stdin secret" || meta["opensearch_password"] != "@file-secret=x" {
		t.Errorf("image_metadata = %q", meta)
	}
	if strings.Contains(out+errOut, "stdin secret") || strings.Contains(out+errOut, "file-secret") {
		t.Errorf("значение в выводе:\n%s\n%s", out, errOut)
	}
}

func TestVMCreate_MetaFromStdinAndFileRefusals(t *testing.T) {
	russian(t)
	for name, tc := range map[string]struct {
		args []string
		want []string
	}{
		"stdin дважды": {[]string{"--meta", "opensearch_host=-", "--meta", "opensearch_password=-"},
			[]string{"оба читают stdin", "stdin читает только одно поле", "=@<файл>"}},
		"нет файла": {[]string{"--meta", "opensearch_host=h", "--meta", "opensearch_password=@./nosuch.pass"},
			[]string{"opensearch_password=@./nosuch.pass: файл не прочитан", "начинается с @, — имя файла"}},
		"нет «=»": {[]string{"--meta", "opensearch_host"},
			[]string{"admin_password=@./admin.pass", "admin_password=-"}},
	} {
		t.Run(name, func(t *testing.T) {
			setupCLI(t)
			cloud := withVMCreateCloud(t)
			args := append([]string{"--yes", "vm", "create", "dash", "--image", "OpensearchDashboards"}, tc.args...)
			code, _, errOut := runCLI(t, "x\n", args...)
			if code != clerrors.ExitGeneral {
				t.Fatalf("exit %d:\n%s", code, errOut)
			}
			for _, w := range tc.want {
				if !strings.Contains(errOut, w) {
					t.Errorf("нет %q в:\n%s", w, errOut)
				}
			}
			if strings.Contains(errOut, "admin_password=secret") {
				t.Errorf("пароль в примере:\n%s", errOut)
			}
			noMutations(t, cloud)
		})
	}
}

func TestTrimNewline(t *testing.T) {
	for in, want := range map[string]string{"a\n": "a", "a\r\n": "a", "a\n\n": "a\n", "a": "a", "a\r": "a\r", "": ""} {
		if got := trimNewline(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// --meta поле=- в терминале: значение читается одной строкой без эха и с
// подсказкой, а не молчаливым ожиданием Ctrl-D с паролем на экране (ревью
// на #77).
func TestPairsArg_StdinTerminalReadsWithoutEcho(t *testing.T) {
	oldTTY, oldPass, oldSave := stdinIsTerminal, readPassword, saveTerminal
	t.Cleanup(func() { stdinIsTerminal, readPassword, saveTerminal = oldTTY, oldPass, oldSave })
	stdinIsTerminal = func() bool { return true }
	saveTerminal = func() func() { return func() {} }
	reads := 0
	readPassword = func() ([]byte, error) { reads++; return []byte("s3cret-from-tty"), nil }

	var prompt bytes.Buffer
	m, err := pairsArg(context.Background(), os.Stdin, &prompt, "meta", []string{"admin_password=-", "hostname=web-1"})
	if err != nil {
		t.Fatal(err)
	}
	if m["admin_password"] != "s3cret-from-tty" || m["hostname"] != "web-1" || reads != 1 {
		t.Errorf("values = %v, reads = %d", m, reads)
	}
	if !strings.Contains(prompt.String(), "admin_password") || strings.Contains(prompt.String(), "s3cret") {
		t.Errorf("prompt = %q: must name the field and not echo the value", prompt.String())
	}

	// Не терминал (pipe) — как раньше, весь stdin.
	stdinIsTerminal = func() bool { return false }
	m, err = pairsArg(context.Background(), strings.NewReader("piped\n"), &prompt, "meta", []string{"admin_password=-"})
	if err != nil || m["admin_password"] != "piped" {
		t.Errorf("pipe: %v %v", m, err)
	}
}
