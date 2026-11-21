// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"golang.org/x/sys/windows"
)

// A read-only view (doctor and MCP) performs no preparation, including
// when evidence from a previous terminal attempt remains in the directory.
func TestManualWindowsReadOnlyAndCleanup(t *testing.T) {
	_, rec, load := manualFixture(t)
	stage := filepath.Join(config.Dir(), "intent-review")
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if marked, err := s.Marked(); marked || err != nil {
		t.Fatal(marked, err)
	}
	if _, err = os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("read-only created a stage", err)
	}
	if err = s.Eligible(rec.Key, "project"); err != nil {
		t.Fatal(err)
	}
	pin, err := s.PrepareTerminal()
	if err != nil {
		t.Fatal(err)
	}
	if s.source.file != nil {
		t.Fatal("receipt still open at the prompt")
	}
	if mark, err := s.Marked(); mark || err != nil {
		t.Fatal(mark, err)
	}
	other, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(pin); err != nil {
		t.Fatal("another reader removed the link", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(pin); !os.IsNotExist(err) {
		t.Fatal("link survived cancellation", err)
	}
	if _, err = os.Stat(recordPath(rec.Dir, rec.ID)); err != nil {
		t.Fatal(err)
	}
}

// Cleanup must report a replacement and leave the unrelated file alone.
func TestManualWindowsCleanupFailure(t *testing.T) {
	_, _, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	pin, err := s.PrepareTerminal()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(pin); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pin, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err == nil {
		t.Fatal("claimed cleanup")
	}
	if b, err := os.ReadFile(pin); err != nil || string(b) != "other" {
		t.Fatal(string(b), err)
	}
}

func TestManualWindowsCrashKeepsOnlyPreparedVersion(t *testing.T) {
	if root := os.Getenv("EVO_TEST_CRASH_REVIEW"); root != "" {
		s, err := PreviewManual(root, os.Getenv("EVO_TEST_REVIEW_ID"), "default")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PrepareTerminal(); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("EVO_TEST_AFTER_PUBLISH") == "1" {
			if err = s.Apply(func() (*ManualSnapshot, error) { return PreviewManual(root, s.Record.ID, "default") }); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(17)
	}
	for _, after := range []string{"0", "1"} {
		t.Run(after, func(t *testing.T) {
			root, rec, load := manualFixture(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestManualWindowsCrashKeepsOnlyPreparedVersion$")
			cmd.Env = append(os.Environ(), "EVO_TEST_CRASH_REVIEW="+root, "EVO_TEST_REVIEW_ID="+rec.ID, "EVO_TEST_AFTER_PUBLISH="+after)
			out, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17 {
				t.Fatal(string(out), err)
			}
			path := recordPath(rec.Dir, rec.ID)
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			pins, _ := filepath.Glob(filepath.Join(config.Dir(), "intent-review", "*.receipt"))
			if len(pins) != 1 {
				t.Fatal(pins)
			}
			view, err := load()
			if err != nil {
				t.Fatal(err)
			}
			marked, err := view.Marked()
			view.Close()
			if err != nil || marked != (after == "1") {
				t.Fatal(marked, err)
			}
			if err = config.WriteFileAtomic(path, old, 0600); err != nil {
				t.Fatal(err)
			}
			newer, err := load()
			if err != nil {
				t.Fatal(err)
			}
			defer newer.Close()
			if yes, err := newer.Marked(); yes || err != nil {
				t.Fatal(yes, err)
			}
			anchor, err := openManualReceipt(pins[0])
			if err != nil {
				t.Fatal(err)
			}
			defer anchor.Close()
			if err = checkManualACL(anchor, true); err != nil {
				t.Fatal(err)
			}
			if same, err := newer.source.matchesFile(anchor); same || err != nil {
				t.Fatal(same, err)
			}
			if b, err := os.ReadFile(pins[0]); err != nil || !bytes.Equal(b, old) {
				t.Fatal(err)
			}
		})
	}
}

// Effective access is checked on the shared object. Creating a hard link
// cannot repair a permissive DACL without changing the original's security.
func TestManualWindowsRefusesForeignACL(t *testing.T) {
	for _, where := range []string{"receipt", "stage"} {
		t.Run(where, func(t *testing.T) {
			_, rec, load := manualFixture(t)
			path := recordPath(rec.Dir, rec.ID)
			if where == "stage" {
				path = filepath.Join(config.Dir(), "intent-review")
				if err := config.MkdirPrivate(path); err != nil {
					t.Fatal(err)
				}
			}
			user, err := windows.GetCurrentProcessToken().GetTokenUser()
			if err != nil {
				t.Fatal(err)
			}
			sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;WD)")
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			s, err := load()
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err = s.PrepareTerminal(); err == nil {
				t.Fatal("retained a receipt with untrusted access")
			}
			files, _ := filepath.Glob(filepath.Join(config.Dir(), "intent-review", "*.receipt"))
			if len(files) != 0 {
				t.Fatal(files)
			}
		})
	}
}

func TestManualWindowsSubstitutedDirectory(t *testing.T) {
	_, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.PrepareTerminal(); err != nil {
		t.Fatal(err)
	}
	moved := rec.Dir + "-old"
	if err = os.Rename(rec.Dir, moved); err != nil {
		t.Fatal(err)
	}
	if err = config.MkdirPrivate(rec.Dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(recordPath(moved, rec.ID), recordPath(rec.Dir, rec.ID)); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(load); err == nil {
		t.Fatal("accepted the same file in a substituted directory")
	}
	marks, _ := filepath.Glob(filepath.Join(s.audit, "decision-*"))
	if len(marks) != 0 {
		t.Fatal(marks)
	}
}
