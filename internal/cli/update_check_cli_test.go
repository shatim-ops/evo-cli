// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func writeUpdateCache(t *testing.T, latest string) {
	t.Helper()
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]any{
		"fetched_at":  now,
		"next_try_at": now.Add(24 * time.Hour),
		"latest":      latest,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.CacheDir(), "update-check.json")
	if err := config.WriteFileAtomic(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorLocal_UpdateCheckWarnAndUpToDate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CI", "")
	t.Setenv("EVO_NO_UPDATE_CHECK", "")
	t.Setenv("EVO_FORCE_UPDATE_CHECK", "")

	origVer := version
	version = "1.9.2"
	t.Cleanup(func() { version = origVer })

	// Cached newer version -> warn, zero network hit (noNetwork is active in cli tests).
	writeUpdateCache(t, "1.9.3")
	checks := doctorLocal(nil)
	var found *tools.DoctorCheck
	for i := range checks {
		if checks[i].Name == "update" {
			found = &checks[i]
			break
		}
	}
	if found == nil || found.Status != tools.CheckWarn || !strings.Contains(found.Detail, "1.9.3") {
		t.Fatalf("expected update warn check for 1.9.3, got %+v", found)
	}

	// Cached equal version -> ok.
	writeUpdateCache(t, "1.9.2")
	checks = doctorLocal(nil)
	found = nil
	for i := range checks {
		if checks[i].Name == "update" {
			found = &checks[i]
			break
		}
	}
	if found == nil || found.Status != tools.CheckOK {
		t.Fatalf("expected update ok check for 1.9.2, got %+v", found)
	}

	// Disabled -> check omitted.
	t.Setenv("EVO_NO_UPDATE_CHECK", "1")
	checks = doctorLocal(nil)
	for _, ch := range checks {
		if ch.Name == "update" {
			t.Fatalf("expected no update check when disabled, got %+v", ch)
		}
	}
}

func TestCLI_MaybePrintUpdateNotice_SilenceAndThrottle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CI", "")
	t.Setenv("EVO_NO_UPDATE_CHECK", "")
	t.Setenv("EVO_FORCE_UPDATE_CHECK", "")

	origVer := version
	origTTY := isStderrTTY
	origJSON := flagJSON
	version = "1.9.2"
	t.Cleanup(func() {
		version = origVer
		isStderrTTY = origTTY
		flagJSON = origJSON
	})

	writeUpdateCache(t, "1.9.3")

	root := &cobra.Command{Use: "evo"}
	vm := &cobra.Command{Use: "vm"}
	vmList := &cobra.Command{Use: "list"}
	vm.AddCommand(vmList)
	doctor := &cobra.Command{Use: "doctor"}
	agent := &cobra.Command{Use: "agent"}
	agentSetup := &cobra.Command{Use: "setup"}
	agent.AddCommand(agentSetup)
	hidden := &cobra.Command{Use: "crew", Hidden: true}
	root.AddCommand(vm, doctor, agent, hidden)

	// 1. Non-TTY -> zero bytes.
	var buf bytes.Buffer
	vmList.SetErr(&buf)
	isStderrTTY = func() bool { return false }
	flagJSON = false
	maybePrintUpdateNotice(vmList)
	if buf.Len() != 0 {
		t.Fatalf("non-TTY must emit nothing, got %q", buf.String())
	}

	// 2. --json even on TTY -> zero bytes.
	isStderrTTY = func() bool { return true }
	flagJSON = true
	maybePrintUpdateNotice(vmList)
	if buf.Len() != 0 {
		t.Fatalf("--json must emit nothing, got %q", buf.String())
	}
	flagJSON = false

	// 3. Excluded commands -> zero bytes.
	for _, c := range []*cobra.Command{doctor, agentSetup, hidden} {
		buf.Reset()
		c.SetErr(&buf)
		maybePrintUpdateNotice(c)
		if buf.Len() != 0 {
			t.Fatalf("excluded command %q emitted %q", c.CommandPath(), buf.String())
		}
	}

	// 4. Interactive TTY on vm list -> prints once, then throttled for 24h.
	buf.Reset()
	vmList.SetErr(&buf)
	maybePrintUpdateNotice(vmList)
	if !strings.Contains(buf.String(), "1.9.3") {
		t.Fatalf("expected update notice in stderr, got %q", buf.String())
	}
	buf.Reset()
	maybePrintUpdateNotice(vmList)
	if buf.Len() != 0 {
		t.Fatalf("second run within 24h must be silent, got %q", buf.String())
	}

	// Ensure no stale temp files left in cache dir.
	entries, err := os.ReadDir(config.CacheDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "update-check.json" {
		t.Fatalf("unexpected cache dir entries: %+v", entries)
	}
}
