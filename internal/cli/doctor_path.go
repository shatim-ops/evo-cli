// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"debug/buildinfo"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// otherEvoChecks names every evo in PATH that is not this one, with its
// version (#718, T7): the shell runs and completes from the first evo in
// PATH, the person may have upgraded another — the version and the Tab
// they see then differ.
func otherEvoChecks() []tools.DoctorCheck {
	self := ""
	if exe, err := os.Executable(); err == nil {
		self = sameFile(exe)
	}
	var checks []tools.DoctorCheck
	seen := map[string]bool{self: true}
	for _, path := range evoInPath() {
		key := sameFile(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		checks = append(checks, tools.DoctorCheck{Name: "other evo", Status: tools.CheckWarn,
			Detail: i18n.Tf("%s: %s — another evo in PATH; the shell runs and completes from the first one in PATH: remove or update the other",
				"%s: %s — ещё один evo в PATH; оболочка запускает и дополняет первый в PATH: удалите или обновите другой",
				path, evoVersionOf(path))})
	}
	return checks
}

// evoInPath lists the files PATH resolves as evo, in its order: on Windows
// evo with each extension of PATHEXT, as the shell finds it.
func evoInPath() []string {
	names := []string{"evo"}
	if runtime.GOOS == "windows" {
		names = nil
		ext := os.Getenv("PATHEXT")
		if ext == "" {
			ext = ".COM;.EXE;.BAT;.CMD"
		}
		for _, e := range strings.Split(ext, ";") {
			if e != "" {
				names = append(names, "evo"+strings.ToLower(e))
			}
		}
	}
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		for _, n := range names {
			p := filepath.Join(dir, n)
			st, err := os.Stat(p)
			if err != nil || st.IsDir() || (runtime.GOOS != "windows" && st.Mode()&0o111 == 0) {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// sameFile is the path with its links resolved, to tell one file under two
// names (a link in /usr/local/bin) from two files.
func sameFile(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

// ldflagsVersion is the version a release stamps (-X main.version=…,
// .goreleaser.yaml) as the build settings record it.
var ldflagsVersion = regexp.MustCompile(`-X[= ]main\.version=([^\s'"]+)`)

// evoVersionOf reads the version of the evo at path from its build
// information, without running it: the file may be anyone's, and a run
// would hand it the environment — the keys of evo among it (§4). A file
// that is no Go binary, or has no version, is named without one.
func evoVersionOf(path string) string {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return i18n.T("version not read: not a Go binary", "версия не прочитана: не исполняемый файл Go")
	}
	for _, s := range bi.Settings {
		if s.Key == "-ldflags" {
			if m := ldflagsVersion.FindStringSubmatch(s.Value); m != nil {
				return i18n.Tf("version %s", "версия %s", m[1])
			}
		}
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return i18n.Tf("version %s", "версия %s", v)
	}
	return i18n.T("version not read", "версия не прочитана")
}
