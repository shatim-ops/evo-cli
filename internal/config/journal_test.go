// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

// The journal switch lives in config.yaml beside the profiles (D46): saving
// or deleting a profile keeps it, and turning it off keeps the profiles.
func TestJournalSwitchKeepsProfiles(t *testing.T) {
	isolate(t)
	if JournalEnabled() {
		t.Fatal("the journal is on without config.yaml")
	}
	if err := SaveProfile("a", Profile{Region: "z"}, Credentials{KeyID: "k", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := SetJournal(true); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile("b", Profile{Region: "z"}, Credentials{KeyID: "k", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProfile("b"); err != nil {
		t.Fatal(err)
	}
	if !JournalEnabled() {
		t.Fatal("saving or deleting a profile turned the journal off")
	}
	if err := SetJournal(false); err != nil {
		t.Fatal(err)
	}
	if JournalEnabled() {
		t.Fatal("the journal is still on")
	}
	if p, _, ok, err := LoadProfile("a"); err != nil || !ok || p.Region != "z" {
		t.Fatalf("profile a after the switch: %+v ok=%v err=%v", p, ok, err)
	}
}
