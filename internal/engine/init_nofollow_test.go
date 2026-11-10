// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// R-67 (а), the find of «Процесс»: evo init checked .evo.yaml by os.Stat,
// which follows a link; a dangling .evo.yaml -> anywhere passed «no file»
// and os.WriteFile wrote the link's target. A link in place of .evo.yaml is
// a file that is there: init writes nothing, before any request.
func TestInit_DanglingSymlinkIsAFileThatIsThere(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "made")
	path := filepath.Join(dir, ".evo.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	var out bytes.Buffer
	e := &Engine{Printer: output.NewWithWriter(&out, output.ModeJSON, false)}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("init went on past the link: %v", r)
			}
		}()
		_ = e.Init(context.Background(), InitOptions{Path: path, NonInteractive: true})
	}()
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("init wrote the link's target: %v", err)
	}
	if !strings.Contains(out.String(), `"exists"`) {
		t.Errorf("init did not say the file is there: %q", out.String())
	}
}
