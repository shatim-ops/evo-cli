// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func breakIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
		"USERPROFILE", "APPDATA",
	} {
		t.Setenv(k, "")
	}
}

// No HOME and no XDG_CONFIG_HOME: os.UserConfigDir fails. Dir must not
// fall back to a relative ".config/evo" in the process working directory
// (Docker, systemd, launchd). Credentials would be written there.
func TestBreak_Env_UnsetHomeUsesRelativeConfigDir(t *testing.T) {
	t.Setenv("CLOUDRU_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("APPDATA", "")
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
	if _, err := os.UserConfigDir(); err == nil {
		t.Skip("UserConfigDir succeeded; this OS still has a config directory")
	}
	dir := Dir()
	if !filepath.IsAbs(dir) {
		t.Fatalf("config dir %q is relative when HOME is unset; SaveProfile writes credentials.yaml into the working directory", dir)
	}
}

// lockProfiles treats a lock older than lockStale as abandoned and deletes
// it without checking the PID it wrote. A holder that is still inside the
// critical section loses the lock; its unlock then removes the new holder's
// file by path.
func TestBreak_Config_LiveProfileLockStolenWhenStale(t *testing.T) {
	breakIsolate(t)
	prev := lockStale
	lockStale = time.Millisecond
	t.Cleanup(func() { lockStale = prev })

	unlockA, err := lockProfiles()
	if err != nil {
		t.Fatal(err)
	}
	defer unlockA()
	lockPath := filepath.Join(Dir(), lockName)
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatal(err)
	}
	unlockB, err := lockProfiles()
	if unlockB != nil {
		defer unlockB()
	}
	if err == nil {
		t.Fatal("second lockProfiles stole the profile lock while the first holder has not unlocked; both will write credentials.yaml")
	}
}
