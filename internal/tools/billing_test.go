// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/billing"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// fakeBilling answers /v2/consumption of organization.api.cloud.ru with one
// status and body and records the query of each request; any other request
// is a 500.
type fakeBilling struct {
	status int
	body   string
	mu     *sync.Mutex
	calls  *[]url.Values
}

func (f fakeBilling) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	if r.URL.Host != "organization.api.cloud.ru" || r.URL.Path != "/v2/consumption" {
		http.Error(rec, "unexpected "+r.URL.String(), 500)
		return rec.Result(), nil
	}
	f.mu.Lock()
	*f.calls = append(*f.calls, r.URL.Query())
	f.mu.Unlock()
	rec.WriteHeader(f.status)
	_, _ = io.WriteString(rec, f.body)
	return rec.Result(), nil
}

// billingRegistry is a registry whose engine has the project and a token,
// and whose HTTP goes to fakeBilling; it returns the recorded queries.
func billingRegistry(t *testing.T, project string, status int, body string) (*Registry, *[]url.Values) {
	t.Helper()
	calls := &[]url.Values{}
	old := http.DefaultTransport
	http.DefaultTransport = fakeBilling{status: status, body: body, mu: &sync.Mutex{}, calls: calls}
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: project, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	}), calls
}

// Строки по спецификации «Контроль затрат» v2: amount — без НДС, amount_nds
// — с НДС, cost — тариф. У третьей строки amount = 0 опущен.
const billingRows = `{"consumptions":[
 {"servname":"Compute","resource_name":"vm-1","amount":100,"amount_nds":120,"cost":2.5},
 {"servname":"Compute","resource_name":"vm-1","amount":"50","amount_nds":"60","cost":2.5},
 {"servname":"Disks","amount_nds":0,"cost":0.31}
]}`

// Без договора — запрос по проекту конфигурации (project_ids), без
// agreement_id; суммы — amount (без НДС) и amount_nds (с НДС), тариф cost
// не суммируется; scope говорит, по какому фильтру считали.
func TestBillingConsumption_ByProjectWithoutAgreement(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "")
	r, calls := billingRegistry(t, addonProject, 200, billingRows)
	m, text, isErr := callNamed(t, r, "billing_consumption", map[string]any{"start": "2026-10-01"})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if len(*calls) != 1 {
		t.Fatalf("requests: %v", *calls)
	}
	q := (*calls)[0]
	if q.Get("project_ids") != addonProject || q.Has("agreement_id") {
		t.Errorf("query %v: want project_ids=%s and no agreement_id", q, addonProject)
	}
	if q.Get("start_date") != "2026-10-01T00:00:00Z" || q.Get("end_date") != "2026-10-01T23:59:59Z" {
		t.Errorf("period %v", q)
	}
	if m["scope"] != "project" || m["project_id"] != addonProject || m["agreement_id"] != nil {
		t.Errorf("scope: %s", text)
	}
	if m["total"] != float64(150) || m["total_with_nds"] != float64(180) || m["records"] != float64(3) {
		t.Errorf("sums: %s", text)
	}
	vat, _ := m["vat"].(map[string]any)
	if s, _ := vat["total"].(string); !strings.Contains(s, "excluding VAT") {
		t.Errorf("vat: %s", text)
	}
	if s, _ := vat["total_with_nds"].(string); !strings.Contains(s, "including VAT") {
		t.Errorf("vat: %s", text)
	}
}

// Аргумент project_id важнее проекта конфигурации.
func TestBillingConsumption_ProjectArgument(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "")
	r, calls := billingRegistry(t, "", 200, `{"consumptions":[]}`)
	other := "11111111-2222-3333-4444-555555555555"
	m, text, isErr := callNamed(t, r, "billing_consumption", map[string]any{"start": "2026-10-01", "project_id": other})
	if isErr || len(*calls) != 1 || (*calls)[0].Get("project_ids") != other || m["project_id"] != other {
		t.Errorf("answer %s, queries %v", text, *calls)
	}
}

// С договором — как раньше: agreement_id, без project_ids.
func TestBillingConsumption_ByAgreement(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-1")
	r, calls := billingRegistry(t, addonProject, 200, billingRows)
	m, text, isErr := callNamed(t, r, "billing_consumption", map[string]any{"start": "2026-10-01", "end": "2026-10-02"})
	if isErr || len(*calls) != 1 {
		t.Fatalf("answer %s, queries %v", text, *calls)
	}
	q := (*calls)[0]
	if q.Get("agreement_id") != "agr-1" || q.Has("project_ids") {
		t.Errorf("query %v: want agreement_id and no project_ids", q)
	}
	if m["scope"] != "agreement" || m["agreement_id"] != "agr-1" || m["project_id"] != nil || m["total"] != float64(150) {
		t.Errorf("answer %s", text)
	}
}

// 403 по проекту — API_ERROR (код 5) с ролью администратора проекта и
// ссылкой на «Перед началом работы», не CONFIG_INVALID.
func TestBillingConsumption_ProjectForbidden(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "")
	r, _ := billingRegistry(t, addonProject, 403, `{"message":"permission denied"}`)
	m, text, isErr := callNamed(t, r, "billing_consumption", map[string]any{"start": "2026-10-01"})
	hint, _ := m["hint"].(string)
	if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(hint, "platform.project.admin") || !strings.Contains(hint, billing.DocAPIStart) {
		t.Errorf("answer %s", text)
	}
	if msg, _ := m["error"].(string); !strings.Contains(msg, addonProject) || !strings.Contains(msg, "403") {
		t.Errorf("error %q", msg)
	}
}

// 403 по договору — роли администратора организации и затрат.
func TestBillingConsumption_AgreementForbidden(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-1")
	r, _ := billingRegistry(t, addonProject, 403, `{"message":"permission denied"}`)
	m, text, isErr := callNamed(t, r, "billing_consumption", map[string]any{"start": "2026-10-01"})
	hint, _ := m["hint"].(string)
	if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(hint, "platform.customer.expense-admin") {
		t.Errorf("answer %s", text)
	}
}

// billing_finops без договора: расход по проекту, баланс неизвестен (баланса
// по проекту в API нет), к хосту баланса запросов нет.
func TestBillingFinops_ByProjectBalanceUnknown(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "")
	r, calls := billingRegistry(t, addonProject, 200, billingRows)
	m, text, isErr := callNamed(t, r, "billing_finops", map[string]any{"start": "2026-10-01", "end": "2026-10-02"})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if len(*calls) != 2 {
		t.Errorf("queries %v, want the period and the previous one", *calls)
	}
	for _, q := range *calls {
		if q.Get("project_ids") != addonProject || q.Has("agreement_id") {
			t.Errorf("query %v", q)
		}
	}
	if m["scope"] != "project" || m["balance"] != nil || m["runway_days"] != nil || m["total_cost"] != float64(150) {
		t.Errorf("answer %s", text)
	}
	if be, _ := m["balance_error"].(string); !strings.Contains(be, "CLOUDRU_AGREEMENT_ID") {
		t.Errorf("balance_error %q", be)
	}
}

// A project-bound registry (evo mcp --allow-project) reads the agreement
// only within the configuration's project: both filters, scope project.
// billing_finops reads the period and the one before the same way.
func TestBilling_ProjectBoundReadsItsProject(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-1")
	r, calls := billingRegistry(t, addonProject, 200, billingRows)
	r.SetProjectBound()
	for _, c := range []struct {
		name     string
		requests int
	}{{"billing_consumption", 1}, {"billing_finops", 2}} {
		before := len(*calls)
		m, text, isErr := callNamed(t, r, c.name, map[string]any{"start": "2026-10-01", "end": "2026-10-02"})
		if isErr || len(*calls)-before != c.requests {
			t.Fatalf("%s: answer %s, queries %v", c.name, text, (*calls)[before:])
		}
		for _, q := range (*calls)[before:] {
			if q.Get("agreement_id") != "agr-1" || q.Get("project_ids") != addonProject {
				t.Errorf("%s: query %v, want agreement_id and project_ids", c.name, q)
			}
		}
		if m["scope"] != "project" || m["project_id"] != addonProject || m["agreement_id"] != "agr-1" {
			t.Errorf("%s: answer %s", c.name, text)
		}
	}
}

// Bound, with no project configured: CONFIG_INVALID naming project_id,
// before any request — not the whole agreement.
func TestBilling_ProjectBoundWithoutProject(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-1")
	r, calls := billingRegistry(t, "", 200, billingRows)
	r.SetProjectBound()
	for _, name := range []string{"billing_consumption", "billing_finops"} {
		m, text, isErr := callNamed(t, r, name, map[string]any{"start": "2026-10-01"})
		msg, _ := m["error"].(string)
		if !isErr || m["reason"] != "CONFIG_INVALID" || !strings.Contains(msg, "project_id") {
			t.Errorf("%s: answer %s", name, text)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("queries %v, want none", *calls)
	}
}

// agreementFake answers the consumption with billingRows and the balance of
// the agreement, and records the host and path of every request.
type agreementFake struct {
	mu   sync.Mutex
	reqs []string
}

func (f *agreementFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r.URL.Host+r.URL.Path)
	f.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.URL.Host == "organization.api.cloud.ru" && r.URL.Path == "/v2/consumption":
		_, _ = io.WriteString(rec, billingRows)
	case r.URL.Host == "billing.api.cloud.ru":
		_, _ = io.WriteString(rec, `{"balance":52340.5,"active":true}`)
	default:
		http.Error(rec, "unexpected "+r.URL.String(), 500)
	}
	return rec.Result(), nil
}

// A project-bound registry (evo mcp --allow-project, H9) gives no balance of
// the agreement: it is the money of the whole organization, wider than the
// server's project (dogfood №5). Every billing tool, with project_id or
// without: no request for the balance; billing_balance is refused with
// CONFIG_INVALID naming --allow-project, before the token; billing_finops
// reports the project's costs with the balance unknown and says why. A new
// billing tool that reads the balance fails here.
func TestBilling_ProjectBoundNoAgreementBalance(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-1")
	r, _ := billingRegistry(t, addonProject, 200, billingRows)
	fake := &agreementFake{}
	http.DefaultTransport = fake

	// Not bound: the balance is read, as documented.
	if m, text, isErr := callNamed(t, r, "billing_balance", nil); isErr || m["balance"] != 52340.5 {
		t.Fatalf("not bound: %s", text)
	}

	r.SetProjectBound()
	fake.reqs = nil
	tools := 0
	for _, tl := range r.Tools() {
		if !strings.HasPrefix(tl.Name, "billing_") {
			continue
		}
		tools++
		for _, args := range []map[string]any{
			{"start": "2026-10-01", "end": "2026-10-02"},
			{"start": "2026-10-01", "end": "2026-10-02", "project_id": addonProject},
		} {
			props, _ := tl.InputSchema["properties"].(map[string]any)
			for k := range args {
				if _, ok := props[k]; !ok {
					delete(args, k)
				}
			}
			m, text, isErr := callNamed(t, r, tl.Name, args)
			if _, ok := m["balance"].(float64); ok {
				t.Errorf("%s %v: the agreement's balance in %s", tl.Name, args, text)
			}
			switch tl.Name {
			case "billing_balance":
				msg, _ := m["error"].(string)
				if !isErr || m["reason"] != "CONFIG_INVALID" || !strings.Contains(msg, "--allow-project") {
					t.Errorf("billing_balance: %s", text)
				}
			case "billing_finops":
				be, _ := m["balance_error"].(string)
				if isErr || m["total_cost"] != float64(150) || m["runway_days"] != nil || !strings.Contains(be, "--allow-project") {
					t.Errorf("billing_finops %v: %s", args, text)
				}
			}
		}
	}
	if tools < 3 {
		t.Fatalf("%d billing tools", tools)
	}
	for _, q := range fake.reqs {
		if strings.HasPrefix(q, "billing.api.cloud.ru") {
			t.Errorf("request for the agreement's balance: %s", q)
		}
	}
}
