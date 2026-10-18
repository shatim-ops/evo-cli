// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"testing"
)

// Группа зоны по умолчанию: признак default из ответа API, иначе по имени —
// SSH-access_<зона> (так Cloud.ru называет её), Default_<зона>, default.
func TestIsDefaultGroup(t *testing.T) {
	var g SecurityGroup
	if err := json.Unmarshal([]byte(`{"id":"sg-1","name":"web","default":true,"availability_zone":{"id":"z2"}}`), &g); err != nil {
		t.Fatal(err)
	}
	if !g.Default || g.AvailabilityZoneID != "z2" || !IsDefaultGroup(&g, "ru.AZ-2") {
		t.Errorf("API flag: %+v", g)
	}
	for name, want := range map[string]bool{
		"SSH-access_ru.AZ-2": true, "ssh-access_ru.az-2": true, "Default_ru.AZ-2": true, "default": true,
		"SSH-access_ru.AZ-1": false, "DefaultWeb": false, "inspector-fileserver-sg": false, "ssh-access": false,
	} {
		if got := IsDefaultGroup(&SecurityGroup{Name: name}, "ru.AZ-2"); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
	if !IsDefaultGroup(&SecurityGroup{Name: "SSH-access_ru.AZ-3"}, "") || IsDefaultGroup(nil, "ru.AZ-2") {
		t.Error("without a zone name / nil")
	}
}

func TestPickSecurityGroup(t *testing.T) {
	sgs := []*SecurityGroup{
		{ID: "a", Name: "inspector-fileserver-sg", AvailabilityZoneID: "z2"},
		{ID: "b", Name: "SSH-access_ru.AZ-2", AvailabilityZoneID: "z2"},
		{ID: "c", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: "z1"},
	}
	if i := PickSecurityGroup(sgs, "z2", "ru.AZ-2"); i != 1 {
		t.Errorf("picked %d, want the zone's default group", i)
	}
	if i := PickSecurityGroup(sgs, "z3", "ru.AZ-3"); i != -1 {
		t.Errorf("no group in the zone: %d", i)
	}
	// Единственная группа проекта чужой зоны не берётся (ревью Grok на #83);
	// единственная без зоны — берётся.
	if i := PickSecurityGroup(sgs[:1], "z3", "ru.AZ-3"); i != -1 {
		t.Errorf("the only group of another zone taken: %d", i)
	}
	if i := PickSecurityGroup([]*SecurityGroup{{ID: "x", Name: "web"}}, "z3", "ru.AZ-3"); i != 0 {
		t.Errorf("the only group without a zone: %d", i)
	}
}
