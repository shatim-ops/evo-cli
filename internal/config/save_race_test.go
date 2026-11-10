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
	// writes.
	var read, wrote sync.WaitGroup
	read.Add(2)
	wrote.Add(2)
	for _, name := range []string{"c1", "c2"} {
		go func() {
			defer wrote.Done()
			s, err := readSettingsFile()
			read.Done()
			read.Wait()
			if err != nil {
				t.Error(err)
				return
			}
			if s.Profiles == nil {
				s.Profiles = map[string]Profile{}
			}
			s.Profiles[name] = Profile{Region: name}
			if err := writeYAML(SettingsPath(), s); err != nil {
				t.Error(err)
			}
		}()
	}
	wrote.Wait()
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
