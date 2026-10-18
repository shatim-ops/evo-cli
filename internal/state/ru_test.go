// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The errors a colleague meets in the Russian CLI — a broken state file, two
// deploys in one directory — are in Russian (QA r4 Q5); the English texts
// (EVO_LANG=en, MCP) stay as they were.

func russian(t *testing.T) {
	t.Helper()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
}

func cliError(t *testing.T, err error) *clierr.CLIError {
	t.Helper()
	var ce *clierr.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a CLIError", err)
	}
	return ce
}

func writeState(t *testing.T, content string) string {
	t.Helper()
	path := DefaultPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRussian_StateCorrupted(t *testing.T) {
	russian(t)
	path := writeState(t, "{")
	ce := cliError(t, func() error { _, err := Read(path); return err }())
	if ce.Code != clierr.CodeStateCorrupted || ce.Message != "в файле состояния ошибка JSON" ||
		!strings.HasPrefix(ce.Hint, "Исправьте JSON в "+path+".") || !strings.Contains(ce.Hint, "консоли Cloud.ru") {
		t.Errorf("got %s %q / %q", ce.Code, ce.Message, ce.Hint)
	}

	path = writeState(t, `{"version": 99}`)
	ce = cliError(t, func() error { _, err := Read(path); return err }())
	if ce.Message != "файл состояния "+path+" версии 99, а этот evo понимает версии до 1" ||
		ce.Hint != "Обновите evo: этот файл записала более новая версия." {
		t.Errorf("got %q / %q", ce.Message, ce.Hint)
	}
}

func TestRussian_StateLocked(t *testing.T) {
	russian(t)
	path := DefaultPath(t.TempDir())
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ce := cliError(t, func() error { _, err := Lock(path); return err }())
	lockPath := filepath.Join(filepath.Dir(path), "state.lock")
	if ce.Code != clierr.CodeStateLocked || ce.Message != "в этом каталоге уже идёт другой evo deploy или destroy" ||
		ce.Hint != "Дождитесь его завершения: блокировка ("+lockPath+") снимается, когда тот процесс закончит работу." {
		t.Errorf("got %s %q / %q", ce.Code, ce.Message, ce.Hint)
	}
}

// The English texts are part of what scripts and agents saw before: kept.
func TestEnglish_StateErrorsUnchanged(t *testing.T) {
	path := writeState(t, "{")
	ce := cliError(t, func() error { _, err := Read(path); return err }())
	if ce.Message != "state file contains invalid JSON" ||
		ce.Hint != "Fix the JSON in "+path+". Before deleting it, note the VMs and floating IPs it lists "+
			"(they keep running and are billed) or check them in the Cloud.ru console." {
		t.Errorf("got %q / %q", ce.Message, ce.Hint)
	}

	path = writeState(t, `{"version": 99}`)
	ce = cliError(t, func() error { _, err := Read(path); return err }())
	if ce.Message != "state file "+path+" has version 99; this evo understands up to 1" ||
		ce.Hint != "Upgrade evo; the newer version wrote this state." {
		t.Errorf("got %q / %q", ce.Message, ce.Hint)
	}

	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ce = cliError(t, func() error { _, err := Lock(path); return err }())
	lockPath := filepath.Join(filepath.Dir(path), "state.lock")
	if ce.Message != "another evo deploy or destroy is running in this directory" ||
		ce.Hint != "Wait for it to finish; the lock ("+lockPath+") is released when that process exits." {
		t.Errorf("got %q / %q", ce.Message, ce.Hint)
	}
}
