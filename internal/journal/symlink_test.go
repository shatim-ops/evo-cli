// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package journal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Grok audit R4, F1 (06.11, fixtures; release plan 1.8, #1095): with
// journal.jsonl a symbolic link, evo diag on and a failing call grew the
// link's target from 0 to 196 bytes — the journal wrote through the link,
// where config.yaml, credentials.yaml and the token cache replace a link
// with a file of their own (0600). A process that can write in the config
// directory could so have evo append to any file of the user. The journal
// follows no link: the target is left as it was, and the records go to a
// file of evo's own, or nowhere.
func TestWrite_FollowsNoSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a symbolic link needs a privilege on Windows")
	}
	isolate(t)
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, Path()); err != nil {
		t.Fatal(err)
	}
	Begin(SurfaceCLI, "1.8.0-test")
	Write(Record{Tool: "vm_list"})
	if fi, err := os.Stat(target); err != nil || fi.Size() != 0 {
		t.Errorf("the journal wrote through its symbolic link: the target holds %d bytes", fi.Size())
	}
}
