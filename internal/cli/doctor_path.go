// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// otherEvoTimeout bounds the run of another evo for its version: doctor
// must answer even when that file hangs.
const otherEvoTimeout = 3 * time.Second

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

// evoVersionOf runs path version --json and reads its version; an evo too
// old for --json answers its first line.
func evoVersionOf(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), otherEvoTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if i := bytes.IndexByte(out, '{'); i >= 0 {
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(bytes.TrimSpace(out[i:]), &v) == nil && v.Version != "" {
			return i18n.Tf("version %s", "версия %s", v.Version)
		}
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n"); line != "" {
		return strings.TrimSpace(line)
	}
	if err != nil {
		return i18n.Tf("version not read: %v", "версия не прочитана: %v", err)
	}
	return i18n.T("version not read", "версия не прочитана")
}
