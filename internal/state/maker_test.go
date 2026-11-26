// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// Release plan 1.8, L3: «another key» only when it is sure — by the account
// when both know it (a new key of the same account sees the same), else by
// the Key ID; unknown on either side is not «another».
func TestMaker_Other(t *testing.T) {
	for _, c := range []struct {
		name     string
		rec, cur *Maker
		other    bool
	}{
		{"a record from before 1.8", nil, &Maker{KeyID: "a"}, false},
		{"no key now", &Maker{KeyID: "a"}, nil, false},
		{"the same account, a new key", &Maker{ServiceAccountID: "sa", KeyID: "a"}, &Maker{ServiceAccountID: "sa", KeyID: "b"}, false},
		{"another account", &Maker{ServiceAccountID: "sa1", KeyID: "a"}, &Maker{ServiceAccountID: "sa2", KeyID: "a"}, true},
		{"no account, the same key", &Maker{KeyID: "a"}, &Maker{ServiceAccountID: "sa", KeyID: "a"}, false},
		{"the record knows the account, the key does not: by the Key ID", &Maker{ServiceAccountID: "sa", KeyID: "a"}, &Maker{KeyID: "b"}, true},
		{"no account, another key", &Maker{KeyID: "a"}, &Maker{KeyID: "b"}, true},
		{"nothing known", &Maker{}, &Maker{KeyID: "b"}, false},
	} {
		if got := c.rec.Other(c.cur); got != c.other {
			t.Errorf("%s: Other %v, want %v", c.name, got, c.other)
		}
	}
}

func TestMaker_RuleOwnerSurvivesOnlyInLocalState(t *testing.T) {
	const source, other = "abcd-source-wxyz", "abcd-second-wxyz"
	maker, collision := NewMaker("", source), NewMaker("", other)
	if maker.KeyID != collision.KeyID {
		t.Fatal("test requires the same display mask")
	}
	sf := &File{Deployments: []DeploymentRecord{{ID: "new", CreatedBy: maker}, {ID: "old", CreatedBy: &Maker{KeyID: maker.KeyID}}}}
	path := DefaultPath(t.TempDir())
	if err := Write(path, sf); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(maker.ruleKeyDigest)) || bytes.Contains(data, []byte(source)) {
		t.Fatal("local state must contain only the digest besides the masked key ID", err)
	}
	state, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal(state)
	if err != nil || bytes.Contains(public, []byte(maker.ruleKeyDigest)) || bytes.Contains(public, []byte(source)) {
		t.Fatal("public serialization exposed local evidence", err)
	}
	stored := state.FindDeployment("new").CreatedBy
	if same, known := stored.MatchRuleOwner(NewMaker("", source)); !known || !same {
		t.Fatal("original key was not recognized")
	}
	if same, known := stored.MatchRuleOwner(collision); !known || same {
		t.Fatal("another key with the same mask was accepted")
	}
	if same, known := state.FindDeployment("old").CreatedBy.MatchRuleOwner(maker); known || same {
		t.Fatal("legacy mask became proof")
	}
	state.RemoveDeployment("new")
	if err := Write(path, state); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || bytes.Contains(data, []byte("rule_owner_keys")) {
		t.Fatal("retired owner's digest retained", err)
	}
}
