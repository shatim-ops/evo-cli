// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"testing"
)

// Поля подсети из ответа API: основная подсеть зоны (default),
// маршрутизируемая (routed_network), DNS-серверы, состояние и тип.
func TestSubnet_UnmarshalFlags(t *testing.T) {
	var s Subnet
	if err := json.Unmarshal([]byte(`{"id":"sn","name":"x","default":true,"routed_network":true,"dns_servers":[],"state":"created","type":"regular","availability_zone":{"id":"z1"}}`), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Default || !s.RoutedNetwork || s.State != "created" || s.Type != "regular" || s.AvailabilityZoneID != "z1" || !s.NoDNS() {
		t.Errorf("parsed %+v (NoDNS %v)", s, s.NoDNS())
	}
	var withDNS, absent Subnet
	_ = json.Unmarshal([]byte(`{"id":"sn","dns_servers":["10.0.0.2"]}`), &withDNS)
	_ = json.Unmarshal([]byte(`{"id":"sn"}`), &absent)
	if withDNS.NoDNS() || absent.NoDNS() {
		t.Error("NoDNS only for an empty dns_servers in the answer")
	}
}

// Порядок выбора: основная подсеть зоны, затем маршрутизируемая, затем
// порядок API; технические и неисправные подсети — в последнюю очередь;
// чужая зона — никогда.
func TestPickSubnet(t *testing.T) {
	subnets := []*Subnet{
		{ID: "a", Name: "a", AvailabilityZoneID: "z2"},
		{ID: "routed", Name: "r", AvailabilityZoneID: "z2", RoutedNetwork: true},
		{ID: "main", Name: "m", AvailabilityZoneID: "z2", Default: true, RoutedNetwork: true},
		{ID: "other", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1", Default: true},
		{ID: "tech", Name: "t", AvailabilityZoneID: "z3", Default: true, Type: "technical"},
		{ID: "plain3", Name: "p", AvailabilityZoneID: "z3"},
		{ID: "broken", Name: "b", AvailabilityZoneID: "z4", Default: true, State: "error"},
		{ID: "ok4", Name: "o", AvailabilityZoneID: "z4", State: "created"},
	}
	for _, c := range []struct{ zone, want string }{
		{"z2", "main"}, {"z1", "other"}, {"z3", "plain3"}, {"z4", "ok4"}, {"z9", ""},
	} {
		got := ""
		if i := PickSubnet(subnets, c.zone, ""); i >= 0 {
			got = subnets[i].ID
		}
		if got != c.want {
			t.Errorf("zone %s: picked %q, want %q", c.zone, got, c.want)
		}
	}
	if i := PickSubnet(subnets[:2], "z2", ""); i != 1 {
		t.Errorf("routed over the API order: %d", i)
	}
	if i := PickSubnet(subnets[:1], "z2", ""); i != 0 {
		t.Errorf("API order: %d", i)
	}
	// No zone in the answer: by the zone name in the subnet name.
	byName := []*Subnet{{ID: "1", Name: "Default_ru.AZ-1"}, {ID: "2", Name: "Default_ru.AZ-2"}}
	if i := PickSubnet(byName, "z2", "ru.AZ-2"); i != 1 {
		t.Errorf("by name: %d", i)
	}
	if i := PickSubnet(byName[:1], "z9", "ru.AZ-9"); i != 0 {
		t.Errorf("the only subnet of the project, zone unknown: %d", i)
	}
	// Единственная подсеть проекта чужой зоны не берётся: новый проект с
	// одной Default_ru.AZ-1 и .evo.yaml на ru.AZ-2 (ревью Grok на #83).
	if i := PickSubnet(subnets[3:4], "z2", "ru.AZ-2"); i != -1 {
		t.Errorf("the only subnet of another zone taken: %d", i)
	}
}
