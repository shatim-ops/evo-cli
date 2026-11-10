// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// Grok audit R3-10 (06.11, fixtures; release plan 1.8, #1095): 20 deletes
// and 20 lists of profiles at once left a whole config — a PASS without a
// control group: the run could not show it would have seen a lost write.
// Here the control comes first: a writer that reads the profiles and
// writes them back without the lock, two at once, loses one of the two —
// so the count below sees a lost update. Then SaveProfile, the same race
// with twenty writers, loses none.
func TestSaveProfile_RaceLosesNoProfile(t *testing.T) {
	t.Setenv(EnvConfigDir, filepath.Join(t.TempDir(), "evo"))

	// Control: read-modify-write without the lock; both read before either
	// writes. The writes go one after the other: two renames onto the same
	// file at once fail on Windows (Access is denied, CI #1099), and the lost
	// update needs only the reads to come first.
	s1, err1 := readSettingsFile()
	s2, err2 := readSettingsFile()
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	for _, w := range []struct {
		s    settingsFile
		name string
	}{{s1, "c1"}, {s2, "c2"}} {
		if w.s.Profiles == nil {
			w.s.Profiles = map[string]Profile{}
		}
		w.s.Profiles[w.name] = Profile{Region: w.name}
		if err := writeYAML(SettingsPath(), w.s); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := readSettingsFile(); err != nil || len(s.Profiles) != 1 {
		t.Fatalf("control: a write without the lock lost nothing (%d profiles, %v): the count below would not see a loss", len(s.Profiles), err)
	}
	if err := writeYAML(SettingsPath(), settingsFile{}); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := SaveProfile(fmt.Sprintf("p%02d", i), Profile{Region: "r"}, Credentials{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != n {
		t.Errorf("%d writers at once left %d profiles: a write was lost", n, len(ps))
	}
}
