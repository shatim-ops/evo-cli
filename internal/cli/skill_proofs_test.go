// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// What the skills say of pg_create (skills/evo-postgres, G130-27; С9
// proof): evo names no price for PostgreSQL — its preview gives the
// tariff's page (tariff_url) and no sum of rubles.
func TestPgCreate_PreviewGivesTheTariffNotAPrice(t *testing.T) {
	withJSONWorld(t)
	tl, _ := newRegistry().Lookup("pg_create")
	newProcess()
	flagJSON, flagDryRun, flagYes = false, false, false
	text, _ := tl.Handler(context.Background(), copyArgs(parityArgs["pg_create"]))
	var answer struct {
		Preview map[string]any `json:"preview"`
	}
	if json.Unmarshal([]byte(text), &answer) != nil || answer.Preview == nil {
		t.Fatalf("pg_create without confirmed: no preview: %s", text)
	}
	if u, _ := answer.Preview["tariff_url"].(string); !strings.HasPrefix(u, "https://cloud.ru/") {
		t.Errorf("the preview has no tariff_url of cloud.ru: %v", answer.Preview["tariff_url"])
	}
	for _, k := range []string{"cost", "est_monthly_rub", "monthly_rub", "hourly_rub"} {
		if strings.Contains(text, `"`+k+`"`) {
			t.Errorf("the preview names a price (%s): %s", k, clipLine(text))
		}
	}
}

// What the skills say of HTTPS (skills/evo-deploy, evo-vm, #514; С9
// proof): the load balancer of evo takes no certificate — lb_create has no
// argument of a certificate or of TLS, so HTTPS ends on the VM.
func TestLBCreate_TakesNoCertificate(t *testing.T) {
	tl, ok := newRegistry().Lookup("lb_create")
	if !ok {
		t.Fatal("no tool lb_create")
	}
	props, _ := tl.InputSchema["properties"].(map[string]any)
	if len(props) == 0 {
		t.Fatal("lb_create has no arguments: the schema moved, update the test")
	}
	for arg := range props {
		if a := strings.ToLower(arg); strings.Contains(a, "cert") || strings.Contains(a, "tls") || strings.Contains(a, "https") {
			t.Errorf("lb_create takes %s: the skills say HTTPS ends on the VM — change them with the code", arg)
		}
	}
}
