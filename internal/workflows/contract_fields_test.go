// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Release plan 1.6, the ratchet of the specs in job contract: a field the
// spec gains after the baseline is read by evo or declared. The job runs the
// test of the ratchet, and the baseline names every request of the
// contract, or a new request would never be held by it.
func TestWorkflows_ContractHoldsTheFieldBaseline(t *testing.T) {
	j, ok := readWorkflows(t)["api-contract.yml"].Jobs["contract"]
	if !ok {
		t.Fatal("api-contract: no job contract")
	}
	runs := false
	for _, s := range j.Steps {
		runs = runs || strings.Contains(s.Run, "api-contract-fields-test.py")
	}
	if !runs {
		t.Error("api-contract: the ratchet of the fields is not tested (api-contract-fields-test.py)")
	}
	var contract struct {
		Services []struct {
			Name     string `json:"name"`
			Requests []struct {
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"requests"`
		} `json:"services"`
	}
	var baseline struct {
		Requests map[string][]string `json:"requests"`
	}
	for file, v := range map[string]any{"api-contract.json": &contract, "api-contract-fields.json": &baseline} {
		b, err := os.ReadFile(filepath.Join("..", "..", "scripts", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	for _, svc := range contract.Services {
		for _, r := range svc.Requests {
			if _, ok := baseline.Requests[svc.Name+" "+r.Method+" "+r.Path]; !ok {
				t.Errorf("api-contract-fields.json: no baseline of %s %s %s — python3 scripts/api-contract-check.py --update-fields", svc.Name, r.Method, r.Path)
			}
		}
	}
}
