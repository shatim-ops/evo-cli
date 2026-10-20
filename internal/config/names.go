// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Names of the tool and of what it keeps on disk. The tool used to be
// called cloudru-vm; the legacy names are still read until
// v2.0, so a project or a profile made by the old binary keeps working.
const (
	// AppName is the name of the binary, of the MCP server and of the
	// directories below.
	AppName = "evo"
	// LegacyAppName is the old name: the alias binary and legacy paths.
	LegacyAppName = "cloudru-vm" // legacy name

	// ProjectFileName is the per-project settings file in the working
	// directory; LegacyProjectFileName is read when it is absent.
	ProjectFileName       = "." + AppName + ".yaml"
	LegacyProjectFileName = "." + LegacyAppName + ".yaml"

	// DataDirName is the per-project directory with the deploy state, the
	// SSH key and saved plans (and, in the home directory, stack jobs and
	// the price override); LegacyDataDirName is used when only it exists.
	DataDirName       = "." + AppName
	LegacyDataDirName = "." + LegacyAppName
)

// dataMarkers are entries of a data directory that must not be lost: the
// deploy state lists billed VMs, stack jobs list created resources.
var dataMarkers = []string{"state.json", "stack-jobs"}

var (
	warnMu   sync.Mutex
	warnFunc = func(msg string) {
		fmt.Fprintf(os.Stderr, "%s%s\n", i18n.T("Warning: ", "Предупреждение: "), msg)
	}
	warned = map[string]bool{}
)

// SetWarnFunc sets where warnings about legacy names go (stderr by
// default). The CLI routes them through its printer, so --json gets
// {"warning": ...} lines.
func SetWarnFunc(f func(msg string)) {
	warnMu.Lock()
	defer warnMu.Unlock()
	warnFunc = f
}

// warnOnce reports msg once per process: the MCP server resolves paths on
// every tool call.
func warnOnce(msg string) {
	warnMu.Lock()
	if warned[msg] {
		warnMu.Unlock()
		return
	}
	warned[msg] = true
	f := warnFunc
	warnMu.Unlock()
	f(msg)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func hasData(dir string) bool {
	for _, m := range dataMarkers {
		if exists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

// DataDir returns the data directory under root: root/.evo, or, for a
// project deployed by the old binary, the legacy root/.cloudru-vm.
//
// The legacy directory is used in place, not moved: it holds state.json
// with the billed VMs, the SSH key they accept and the state lock. A move
// could race a running deploy or a legacy binary (another install, an old
// MCP image) that still writes there, and a half-done move would leave
// state where no one reads it. The user renames it when nothing else uses
// the project; the warning says how.
//
// When both exist, the one with data wins (the legacy one only if .evo has
// none): an empty .evo made by hand must not hide the VMs listed in the
// legacy state.
func DataDir(root string) string {
	dir := filepath.Join(root, DataDirName)
	legacy := filepath.Join(root, LegacyDataDirName)
	if !exists(legacy) {
		return dir
	}
	if !exists(dir) {
		warnOnce(i18n.Tf("using the legacy directory %s; rename it to %s when no older %s works in this directory (support ends in v2.0)",
			"используется старый каталог %s; переименуйте его в %s, когда в этом каталоге не будет работать старый %s (поддержка — до v2.0)",
			legacy, DataDirName, LegacyAppName))
		return legacy
	}
	if hasData(legacy) && !hasData(dir) {
		warnOnce(i18n.Tf("both %s and %s exist; using the legacy %s, which has the state: move its contents to %s and remove it",
			"есть и %s, и %s; используется старый %s — в нём state: перенесите содержимое в %s и удалите старый каталог",
			dir, legacy, legacy, DataDirName))
		return legacy
	}
	if hasData(legacy) {
		warnOnce(i18n.Tf("both %s and %s have state; using %s, the legacy %s is ignored — check it for VMs still running",
			"state есть и в %s, и в %s; используется %s, старый %s не учитывается — проверьте, нет ли в нём ещё работающих ВМ",
			dir, legacy, dir, legacy))
	}
	return dir
}

// ProjectFile returns the project settings file in dir: .evo.yaml, or the
// legacy .cloudru-vm.yaml when only it exists.
func ProjectFile(dir string) string {
	path := filepath.Join(dir, ProjectFileName)
	legacy := filepath.Join(dir, LegacyProjectFileName)
	// The message names the full path: the same relative name in another
	// directory is another file.
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	if exists(path) || !exists(legacy) {
		if exists(legacy) {
			warnOnce(i18n.Tf("%s is ignored: %s is read instead; remove the legacy file",
				"%s не учитывается, читается %s; удалите старый файл", abs(legacy), abs(path)))
		}
		return path
	}
	warnOnce(i18n.Tf("reading the legacy %s; rename it to %s (support ends in v2.0)",
		"читается старый %s; переименуйте его в %s (поддержка — до v2.0)", abs(legacy), ProjectFileName))
	return legacy
}
