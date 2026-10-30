// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// impactValue is a field of impact: its class, and, for a field evo does
// not send, why (a choice) or the work item that decides it (a gap).
var impactValue = regexp.MustCompile(`^(цена|работа|безопасность)(; (не шлёт|пропуск): .{10,})?$`)

// CLAUDE.md §6, axis 4 (release plan 1.6, the ratchet of the specs, part
// two; audits/blind-spots-1.5.md §9.3, item 4): every create of the
// contract map names the fields of its request that bear on the price, the
// work or the security of what it makes. A field evo sends is in the
// preview (bodyUnnamed holds that); a field it does not send says why, or
// names the work item that decides it. A create without a request in the
// contract is exempt with a reason. impact-unknown of api-contract-check.py
// holds the fields against the spec.
func TestWorkflows_CreatesDeclareImpact(t *testing.T) {
	var cmap map[string]struct {
		Creates *struct {
			None string `json:"none"`
		} `json:"creates"`
	}
	var contract struct {
		Exempt   map[string]string `json:"impact_exempt"`
		Services []struct {
			Name     string `json:"name"`
			Requests []struct {
				Method  string            `json:"method"`
				Path    string            `json:"path"`
				Body    []string          `json:"body"`
				Creates []string          `json:"creates"`
				Impact  map[string]string `json:"impact"`
			} `json:"requests"`
		} `json:"services"`
	}
	for file, v := range map[string]any{"../../docs/internal/contract-map.json": &cmap, "../../scripts/api-contract.json": &contract} {
		b, err := os.ReadFile(filepath.FromSlash(file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	covered := map[string]bool{}
	gaps := 0
	for _, svc := range contract.Services {
		for _, r := range svc.Requests {
			where := svc.Name + " " + r.Method + " " + r.Path
			if (r.Creates == nil) != (r.Impact == nil) {
				t.Errorf("%s: creates and impact go together", where)
			}
			for _, c := range r.Creates {
				covered[c] = true
				if e, ok := cmap[c]; !ok || e.Creates == nil {
					t.Errorf("%s: creates names %s, which the contract map has no create of", where, c)
				}
			}
			sent := map[string]bool{}
			for _, f := range r.Body {
				sent[strings.TrimSuffix(strings.SplitN(f, ".", 2)[0], "[]")] = true
			}
			for f, v := range r.Impact {
				m := impactValue.FindStringSubmatch(v)
				switch {
				case m == nil:
					t.Errorf("%s: impact %s = %q — «цена|работа|безопасность», and for a field evo does not send «; не шлёт: why» or «; пропуск: work item»", where, f, v)
				case sent[f] && m[3] != "":
					t.Errorf("%s: impact %s — evo sends it now: drop «%s»", where, f, m[3])
				case !sent[f] && m[3] == "":
					t.Errorf("%s: impact %s — evo does not send it: say why («не шлёт: …») or name the work item («пропуск: …»)", where, f)
				}
				if m != nil && m[3] == "пропуск" {
					gaps++
				}
			}
		}
	}
	for c, why := range contract.Exempt {
		if covered[c] {
			t.Errorf("impact_exempt names %s, which a request declares now: remove the line", c)
		}
		if len(why) < 10 {
			t.Errorf("impact_exempt %s: a reason, not %q", c, why)
		}
	}
	for name, e := range cmap {
		if e.Creates == nil || e.Creates.None != "" {
			continue
		}
		if !covered[name] && contract.Exempt[name] == "" {
			t.Errorf("%s creates a resource, and no request of api-contract.json declares its impact (creates, impact) — nor impact_exempt", name)
		}
	}
	t.Logf("fields of impact evo does not send and no one decided (пропуск): %d", gaps)
}

// Release plan 1.6, «Цена PostgreSQL до согласия»: the tariff evo prices by
// is checked against the public page once a week and by hand before a
// release candidate — in job contract, not in a PR, whose code does not
// change the tariff.
func TestWorkflows_ContractChecksTheTariff(t *testing.T) {
	j := readWorkflows(t)["api-contract.yml"].Jobs["contract"]
	for _, s := range j.Steps {
		if !strings.Contains(s.Run, "-run '^TestLive_TariffPageMatchesSnapshot$'") {
			continue
		}
		if s.Env["EVO_LIVE_TARIFFS"] != "1" {
			t.Errorf("api-contract, %s: without EVO_LIVE_TARIFFS=1 the check skips", s.Name)
		}
		if !strings.Contains(s.If, "schedule") || !strings.Contains(s.If, "workflow_dispatch") || strings.Contains(s.If, "pull_request") {
			t.Errorf("api-contract, %s: runs on %q, want the schedule and workflow_dispatch only", s.Name, s.If)
		}
		return
	}
	t.Error("api-contract: no step checks the tariff of PostgreSQL (TestLive_TariffPageMatchesSnapshot)")
}
