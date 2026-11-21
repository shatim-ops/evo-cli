// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func manualFixture(t *testing.T) (string, Intent, func() (*ManualSnapshot, error)) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	rec, err := Open(Dir(root), Intent{Tool: "redis_create", Name: "data", Surface: "cli", StartedAt: time.Now(), Ended: true, Counts: true, Sent: true, Key: &state.Maker{ServiceAccountID: "account", KeyID: "1234****5678"}, ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	load := func() (*ManualSnapshot, error) { return PreviewManual(root, rec.ID, "default") }
	return root, rec, load
}

func TestManualExactVersion(t *testing.T) {
	root, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prepareManualTest(t, s)
	if err = s.Eligible(rec.Key, "project"); err != nil {
		t.Fatal(err)
	}
	path := s.source.Name()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.WriteFileAtomic(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(load); err == nil {
		t.Fatal("applied a stale preview to identical bytes in a new inode")
	}
	current, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	prepareManualTest(t, current)
	if err = current.Apply(load); err != nil {
		t.Fatal(err)
	}
	if err = current.Apply(load); err != nil {
		t.Fatal(err)
	}
	match, err := current.Marked()
	if !match || err != nil {
		t.Fatal(match, err)
	}
	dirs, err := filepath.Glob(filepath.Join(current.audit, "decision-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatal(dirs, err)
	}
	if p := Pending(Dir(root)); len(p) != 1 || p[0].ID != rec.ID {
		t.Fatal("old reader lost its receipt", p)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, b) {
		t.Fatal("original changed", err)
	}
	if err = config.WriteFileAtomic(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	prepareManualTest(t, fresh)
	if yes, err := fresh.Marked(); err != nil || yes {
		t.Fatal("replacement was hidden", yes, err)
	}
	if err = fresh.Apply(load); err != nil {
		t.Fatal(err)
	}
	dirs, _ = filepath.Glob(filepath.Join(current.audit, "decision-*"))
	if len(dirs) != 2 {
		t.Fatal(dirs)
	}
}

func TestManualFailureKeepsReceipt(t *testing.T) {
	_, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prepareManualTest(t, s)
	stages := 0
	err = s.Apply(func() (*ManualSnapshot, error) {
		stages++
		if stages == 2 {
			return nil, errors.New("changed before publication")
		}
		return load()
	})
	if err == nil {
		t.Fatal("published despite failed recheck")
	}
	// An interrupted preparer leaves only a private staging directory.
	stage, err := os.MkdirTemp(s.audit, ".prepare-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Link(s.source.Name(), filepath.Join(stage, "receipt")); err != nil {
		t.Fatal(err)
	}
	if yes, err := s.Marked(); err != nil || yes {
		t.Fatal(yes, err)
	}
	if err = s.Apply(load); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(s.audit, "decision-*", "decision.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	if err = os.WriteFile(files[0], []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if yes, err := s.Marked(); err == nil || yes {
		t.Fatal("broken audit accepted")
	}
	if err = s.Apply(load); err == nil {
		t.Fatal("silently repaired broken evidence")
	}
	if _, err = os.Stat(recordPath(rec.Dir, rec.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestManualConcurrentProcesses(t *testing.T) {
	if root := os.Getenv("EVO_TEST_MANUAL_CHILD_ROOT"); root != "" {
		load := func() (*ManualSnapshot, error) {
			return PreviewManual(root, os.Getenv("EVO_TEST_MANUAL_CHILD_ID"), "default")
		}
		s, err := load()
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		prepareManualTest(t, s)
		if err = s.Apply(load); err != nil {
			t.Fatal(err)
		}
		return
	}
	root, rec, load := manualFixture(t)
	var cmds []*exec.Cmd
	for range 2 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestManualConcurrentProcesses$")
		cmd.Env = append(os.Environ(), "EVO_TEST_MANUAL_CHILD_ROOT="+root, "EVO_TEST_MANUAL_CHILD_ID="+rec.ID)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dirs, _ := filepath.Glob(filepath.Join(s.audit, "decision-*"))
	if len(dirs) != 1 {
		t.Fatal(dirs)
	}
}

func TestManualReplacementAfterPublication(t *testing.T) {
	_, _, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prepareManualTest(t, s)
	path := s.source.Name()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	err = s.Apply(func() (*ManualSnapshot, error) {
		checks++
		if checks == 3 {
			if e := config.WriteFileAtomic(path, data, 0600); e != nil {
				t.Fatalf("old writer could not replace the receipt after publication: %v", e)
			}
		}
		return load()
	})
	if err == nil || checks != 3 {
		t.Fatal("replacement accepted", checks, err)
	}
	fresh, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if v, e := fresh.Marked(); v || e != nil {
		t.Fatal(v, e)
	}
	marks, _ := filepath.Glob(filepath.Join(s.audit, "decision-*"))
	if len(marks) != 1 {
		t.Fatal("lost audit for former version", marks)
	}
}

func TestManualScopeAndUnreadableAnchor(t *testing.T) {
	root, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prepareManualTest(t, s)
	if err = s.Apply(load); err != nil {
		t.Fatal(err)
	}
	otherProfile, err := PreviewManual(root, rec.ID, "another")
	if err != nil {
		t.Fatal(err)
	}
	defer otherProfile.Close()
	if closed, e := otherProfile.Marked(); closed || e != nil {
		t.Fatal("profile crossed", closed, e)
	}
	other := t.TempDir()
	if err = os.MkdirAll(Dir(other), 0700); err != nil {
		t.Fatal(err)
	}
	otherPath := recordPath(Dir(other), rec.ID)
	if err = os.Link(s.source.Name(), otherPath); err != nil {
		t.Fatal(err)
	}
	otherDir, err := PreviewManual(other, rec.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	defer otherDir.Close()
	if closed, e := otherDir.Marked(); closed || e != nil {
		t.Fatal("directory crossed", closed, e)
	}
	if err = os.MkdirAll(ConfigDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(s.source.Name(), recordPath(ConfigDir(), rec.ID)); err != nil {
		t.Fatal(err)
	}
	if x, e := load(); e == nil {
		x.Close()
		t.Fatal("duplicate id accepted")
	}
	anchors, _ := filepath.Glob(filepath.Join(s.audit, "decision-*", "receipt"))
	if len(anchors) != 1 {
		t.Fatal(anchors)
	}
	if err = os.Remove(anchors[0]); err != nil {
		t.Fatal(err)
	}
	if closed, e := s.Marked(); closed || e == nil {
		t.Fatal("lost anchor accepted", closed, e)
	}
}

func TestManualPublicationErrorKeepsOriginal(t *testing.T) {
	_, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prepareManualTest(t, s)
	// Make the intended staging parent a file. A write failure cannot dismiss the receipt.
	if err = os.WriteFile(filepath.Dir(s.audit), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(load); err == nil {
		t.Fatal("applied without a writable audit directory")
	}
	if p := Pending(rec.Dir); len(p) != 1 || p[0].ID != rec.ID {
		t.Fatal(p)
	}
}

func TestManualOwnerEvidence(t *testing.T) {
	_, _, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		record, current, key string
		allowed              bool
	}{
		{"", "", "1234****5678", false},
		{"account", "", "1234****5678", false},
		{"", "account", "1234****5678", false},
		{"account", "other", "1234****5678", false},
		{"account", "account", "0000****9999", true},
	} {
		s.Record.Key.ServiceAccountID = tc.record
		err := s.Eligible(&state.Maker{ServiceAccountID: tc.current, KeyID: tc.key}, "project")
		if (err == nil) != tc.allowed {
			t.Errorf("receipt %q/current %q/key %q: %v", tc.record, tc.current, tc.key, err)
		}
	}
}

func prepareManualTest(t *testing.T, s *ManualSnapshot) {
	t.Helper()
	if _, err := s.PrepareTerminal(); err != nil {
		t.Fatal(err)
	}
}
