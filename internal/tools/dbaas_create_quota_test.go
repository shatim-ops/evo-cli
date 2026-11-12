// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// cloud-e2e 208 and 203 (12.11): pg_create is taken (creating, an
// operation id), seconds later the cluster is FAILED and the operation of
// its create is done with error {code: 13, message: "acquire_quota
// failed"}; the card of the cluster names no cause. pg_operation answered
// it as a success: done, the error as the service words it, resource_status
// FAILED — no machine cause, nothing that says a repeat of the create meets
// the same quota and that evo cannot delete the FAILED cluster (DB-21).
// The answer names the cause the way a quota refusal of Compute does
// (details.cause "quota", #1488) and the way out. Kafka and Redis read
// their operations through the same code; the form of their refusal for a
// quota is a hypothesis until a run shows it.
func TestDBaaSOperation_QuotaFailureOfACreateNamesTheCause(t *testing.T) {
	rt := &contractRT{kind: "200", body: `{"id":"` + contractID + `","done":true,` +
		`"resource_id":"` + contractID + `","error":{"code":13,"message":"acquire_quota failed"},` +
		`"created_at":"2026-11-12T10:00:00Z","updated_at":"2026-11-12T10:00:05Z","status":"FAILED"}`}
	reg := contractReg(t, rt)
	tl, _ := reg.Lookup("pg_operation")
	text, _, _ := contractCall(tl, map[string]any{"operation_id": contractID}, contractCallLimit)
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		t.Fatalf("not JSON: %s", text)
	}
	details, _ := m["details"].(map[string]any)
	if details["cause"] != "quota" {
		t.Errorf("a create the platform failed for its quota: want details.cause \"quota\": %s", text)
	}
	hint, _ := m["hint"].(string)
	if !strings.Contains(hint, "quota") || !strings.Contains(hint, "console") {
		t.Errorf("want the way out: a repeat meets the same quota, the FAILED cluster is deleted in the console (DB-21): %s", text)
	}
}
