// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Ответ GET /availability-zones 17.10.2026: у ru.AZ-1 enabled=false, ВМ
// в ней — 422 wrong_az. Поля без enabled — зона открыта. ID выдуманы.
func TestAvailabilityZone_Closed(t *testing.T) {
	var zones []*AvailabilityZone
	body := `[{"id":"z1","name":"ru.AZ-1","enabled":false,"default":false},
	          {"id":"z2","name":"ru.AZ-2","enabled":true,"default":true},
	          {"id":"z3","name":"ru.AZ-3"}]`
	if err := json.Unmarshal([]byte(body), &zones); err != nil {
		t.Fatal(err)
	}
	if !zones[0].Closed() || zones[1].Closed() || zones[2].Closed() || !zones[1].Default {
		t.Errorf("closed/default: %v %v %v %v", zones[0].Closed(), zones[1].Closed(), zones[2].Closed(), zones[1].Default)
	}
	var nilZone *AvailabilityZone
	if nilZone.Closed() {
		t.Error("nil zone closed")
	}

	err := ClosedZoneError(zones, "z1")
	var ce *clierrors.CLIError
	if !asCLI(err, &ce) || ce.Code != clierrors.CodeConfigInvalid || !strings.Contains(ce.Message, "ru.AZ-1") ||
		!strings.Contains(ce.Hint, "ru.AZ-2, ru.AZ-3") || !strings.Contains(ce.Hint, "evo list-zones") {
		t.Errorf("closed zone error: %v", err)
	}
	for _, id := range []string{"z2", "z3", "unknown"} {
		if err := ClosedZoneError(zones, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if err := ClosedZoneError(nil, "z1"); err != nil {
		t.Errorf("empty list: %v", err)
	}

	if open := OpenZones(zones); len(open) != 2 || open[0].Name != "ru.AZ-2" || open[1].Name != "ru.AZ-3" {
		t.Errorf("open zones: %+v", open)
	}
	if all := OpenZones(zones[:1]); len(all) != 1 {
		t.Errorf("all closed: keep them, got %d", len(all))
	}
}

func asCLI(err error, target **clierrors.CLIError) bool {
	ce, ok := err.(*clierrors.CLIError)
	if ok {
		*target = ce
	}
	return ok
}

// FIP в error_deleting без привязки (NET-7): не свободен — его нельзя ни
// привязать, ни удалить. Свободен только available; без состояния — по
// привязке, как раньше; любое другое состояние — занят (ревью Grok на #83).
func TestFloatingIP_AvailableStates(t *testing.T) {
	for state, want := range map[string]bool{
		"available": true, "": true, "in_use": false, "error_deleting": false,
		"error": false, "deleting": false, "creating": false, "error_creating": false,
		"attaching": false, "detaching": false, "updating": false, "unknown_new": false,
	} {
		if got := (FloatingIP{State: state}).Available(); got != want {
			t.Errorf("%q: %v, want %v", state, got, want)
		}
	}
}

// Имя по шаблону compute (^[a-zA-Z][a-zA-Z0-9.\-_]*$, 1–64) — до создания
// (релизный QA, C-api 4).
func TestValidName(t *testing.T) {
	for _, ok := range []string{"web", "evo-app", "a.b_c-1", strings.Repeat("a", 64)} {
		if err := ValidName("VM", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1web", "-web", "веб", "my app", "a/b", strings.Repeat("a", 65)} {
		var ce *clierrors.CLIError
		if err := ValidName("VM", bad); !asCLI(err, &ce) || ce.Code != clierrors.CodeUsage {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
