// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Cursor r2, R2-3: without HOME, XDG_CONFIG_HOME and CLOUDRU_CONFIG_DIR
// (systemd, launchd, a container) Dir fell back to ".config/evo" relative
// to the working directory, and configure wrote credentials.yaml there.
// Now there is no directory: configure is CONFIG_INVALID naming
// CLOUDRU_CONFIG_DIR, reading finds no profiles, the token is not cached,
// and nothing is written into the working directory.
func TestDir_NoHomeNeverRelative(t *testing.T) {
	for _, k := range []string{EnvConfigDir, "HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(k, "")
	}
	if _, err := os.UserConfigDir(); err == nil {
		t.Skip("this OS still has a config directory without these variables")
	}
	cwd := t.TempDir()
	t.Chdir(cwd)

	if d := Dir(); d != "" {
		t.Fatalf("Dir() = %q, want \"\"", d)
	}
	if _, err := os.UserCacheDir(); err != nil {
		if p := TokenCachePath("default"); p != "" {
			t.Errorf("TokenCachePath = %q, want \"\" (no cache)", p)
		}
	}
	if _, _, found, err := LoadProfile("default"); err != nil || found {
		t.Errorf("LoadProfile: found=%v err=%v, want no profile and no error", found, err)
	}
	err := SaveProfile("default", Profile{}, Credentials{KeyID: "k", Secret: "s"})
	if clerrors.ExitCode(err) != clerrors.ExitAuth {
		t.Fatalf("SaveProfile without a config directory: %v, want CONFIG_INVALID", err)
	}
	if msg := err.Error(); !strings.Contains(msg, EnvConfigDir) {
		t.Errorf("error does not name %s: %s", EnvConfigDir, msg)
	}
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 0 {
		t.Errorf("files written into the working directory: %v", entries)
	}
}

// Cursor r2, R2-4: a profile lock older than 30 s was taken over even
// though its holder was alive (stopped, on a slow disk), and that holder's
// unlock then removed the new holder's file. The lock is the OS's now: a
// held lock stays held however old the file is.
func TestProfileLock_LiveHolderKeepsLock(t *testing.T) {
	isolate(t)
	prev := lockWait
	lockWait = 100 * time.Millisecond
	t.Cleanup(func() { lockWait = prev })

	unlockA, err := lockProfiles()
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(Dir(), lockName)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatal(err)
	}
	unlockB, err := lockProfiles()
	if err == nil {
		unlockB()
		t.Fatal("a second holder took the profile lock while the first holds it")
	}
	unlockA()
	unlockC, err := lockProfiles()
	if err != nil {
		t.Fatalf("lock not free after unlock: %v", err)
	}
	unlockC()
}
