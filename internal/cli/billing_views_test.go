// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// renderRU renders text with the view in Russian, as the CLI speaks.
func renderRU(t *testing.T, view renderFunc, text string) string {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	var buf bytes.Buffer
	if err := view(output.NewWithWriter(&buf, output.ModePlain, false), &buf, text); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The three billing commands have their text view (dogfood №5: raw floats
// and English fields reached a human).
func TestBillingViews_Named(t *testing.T) {
	for _, verb := range []string{"finops", "consumption", "balance"} {
		if _, c, _ := findLeaf(t, "billing", verb); c.render == nil {
			t.Errorf("billing %s has no text view", verb)
		}
	}
}

// Whole rubles with a digit separator and ₽; under a ruble — "< 1 ₽".
func TestRub(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	for v, want := range map[float64]string{
		0: "0 ₽", 0.3: "< 1 ₽", 0.99: "< 1 ₽", 1.49: "1 ₽", 999.5: "1 000 ₽",
		14998.2: "14 998 ₽", 1234567: "1 234 567 ₽", -2500.4: "-2 500 ₽", -0.2: "> -1 ₽",
	} {
		if got := rub(v); got != want {
			t.Errorf("rub(%v) = %q, want %q", v, got, want)
		}
	}
	i18n.SetLang(i18n.EN)
	if got := rub(14998.2); got != "14,998 ₽" {
		t.Errorf("en: %q", got)
	}
}

// The period: "22.09–21.10 (30 дн.)" in the current year, with the year
// otherwise.
func TestBillingPeriod(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct{ start, end, want string }{
		{"2026-09-22", "2026-10-21", "22.09–21.10 (30 дн.)"},
		{"2026-10-01", "2026-10-01", "01.10 (1 дн.)"},
		{"2025-12-22", "2026-01-20", "22.12.2025–20.01.2026 (30 дн.)"},
		{"2025-09-01", "2025-09-30", "01.09.2025–30.09.2025 (30 дн.)"},
		{"bad", "2026-10-01", "bad–2026-10-01"},
	} {
		if got := billingPeriod(c.start, c.end, now); got != c.want {
			t.Errorf("%s..%s = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}

const finopsAnswer = `{
 "period":{"start":"2026-09-22","end":"2026-10-21","days":30},
 "prev_period":{"start":"2026-08-23","end":"2026-09-21"},
 "currency":"RUB","balance":52340.71,"active":true,
 "total_cost":12498.33,"total_cost_with_nds":14998.0,
 "daily_burn":416.61,"daily_burn_with_nds":499.93,
 "by_service":[{"service":"Compute vm-1","cost":10000.4,"cost_with_nds":12000.48},
  {"service":"Disks","cost":0.3,"cost_with_nds":0.36}],
 "by_service_truncated":82,
 "anomalies":[{"service":"Compute vm-1","prev":4000,"now":10000.4,"delta_pct":150.01,"prev_with_nds":4800,"now_with_nds":12000.48},
  {"service":"Kafka k-1","prev":0,"now":20,"delta_pct":null,"prev_with_nds":0,"now_with_nds":24}],
 "line_items":485,"scope":"agreement","agreement_id":"agr-1",
 "runway_days":125.6,"projected_zero_date":"2027-02-23",
 "budget":{"monthly":10000,"projected_monthly":12498.3,"over":true,"pct":124.98}
}`

func TestRenderBillingFinops(t *testing.T) {
	billingNow = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { billingNow = time.Now })
	got := renderRU(t, renderBillingFinops, finopsAnswer)
	for _, want := range []string{
		"Отчёт FinOps: договор agr-1 · 22.09–21.10 (30 дн.)",
		"Расход за период:  14 998 ₽ с НДС · 12 498 ₽ без НДС",
		"В день:            500 ₽ с НДС · 417 ₽ без НДС",
		"Баланс:            52 341 ₽, договор активен",
		"Запас:             125 дн., до 23.02.2027 (по расходу без НДС)",
		"Бюджет на месяц:   10 000 ₽; прогноз 12 498 ₽ без НДС (125%) — превышен",
		"Строк потребления: 485",
		"По сервисам, суммы с НДС (показано 2 из 84):",
		"Compute vm-1  12 000 ₽",
		"Disks         < 1 ₽",
		"Рост к 23.08–21.09, суммы с НДС:",
		"Compute vm-1  4 800 ₽  12 000 ₽  +150%",
		"Kafka k-1     0 ₽      24 ₽      новое",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	for _, raw := range []string{"12498.33", "_truncated", "total_cost", "null"} {
		if strings.Contains(got, raw) {
			t.Errorf("raw %q in:\n%s", raw, got)
		}
	}
}

// By project: the balance is unknown and the runway is not computed — the
// view says why, in Russian words of the answer's state, not its English
// text.
func TestRenderBillingFinops_ProjectScope(t *testing.T) {
	got := renderRU(t, renderBillingFinops, `{
 "period":{"start":"2026-10-01","end":"2026-10-02","days":2},
 "prev_period":{"start":"2026-09-29","end":"2026-09-30"},
 "balance":null,"active":null,"balance_error":"balance unavailable: it needs the billing agreement (CLOUDRU_AGREEMENT_ID), the API has no balance by project",
 "total_cost":150,"total_cost_with_nds":180,"daily_burn":75,"daily_burn_with_nds":90,
 "by_service":[],"anomalies":null,"line_items":3,"scope":"project","project_id":"p-1",
 "runway_days":null,"projected_zero_date":null}`)
	for _, want := range []string{
		"Отчёт FinOps: проект p-1 ·",
		"Баланс:            неизвестен: баланс есть только у договора, задайте CLOUDRU_AGREEMENT_ID",
		"Запас:             не рассчитан",
		"По сервисам: расхода нет.",
		"Рост к 29.09–30.09: нет.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

func TestRenderBillingConsumption(t *testing.T) {
	billingNow = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { billingNow = time.Now })
	got := renderRU(t, renderBillingConsumption, `{
 "period":{"start":"2026-10-01","end":"2026-10-21"},"currency":"RUB",
 "total":12498.33,"total_with_nds":14998,"records":485,
 "by_service":[{"service":"Compute vm-1","cost":10000.4,"cost_with_nds":12000.48}],
 "scope":"project","project_id":"p-1","agreement_id":"agr-1"}`)
	for _, want := range []string{
		"Потребление: проект p-1 в договоре agr-1 · 01.10–21.10 (21 дн.)",
		"Итого:             14 998 ₽ с НДС · 12 498 ₽ без НДС",
		"Строк потребления: 485",
		"По сервисам, суммы с НДС:",
		"Compute vm-1  12 000 ₽",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	// A zero total over rows: the note and the keys of a row, as the answer
	// gives them.
	got = renderRU(t, renderBillingConsumption, `{"period":{"start":"2026-10-01","end":"2026-10-01"},
 "total":0,"total_with_nds":0,"records":2,"by_service":[],"scope":"agreement","agreement_id":"agr-1",
 "note":"total is 0 — verify field names","sample_item_keys":["a","b"]}`)
	if !strings.Contains(got, "Примечание: total is 0 — verify field names; поля строки: a, b") {
		t.Errorf("zero total:\n%s", got)
	}
}

func TestRenderBillingBalance(t *testing.T) {
	got := renderRU(t, renderBillingBalance, `{"balance":"-1520.5","active":false,"id":"agr-1"}`)
	for _, want := range []string{"Баланс:  -1 521 ₽", "Договор: не активен"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	// An answer without a balance: as it is, not a made-up zero.
	got = renderRU(t, renderBillingBalance, `{"other":1}`)
	if strings.Contains(got, "₽") || !strings.Contains(got, "other") {
		t.Errorf("no balance:\n%s", got)
	}
}

// A service name from the cloud does not reach the terminal with control
// characters.
func TestRenderBilling_Sanitized(t *testing.T) {
	got := renderRU(t, renderBillingConsumption, `{"period":{"start":"2026-10-01","end":"2026-10-01"},
 "total":1,"total_with_nds":1.2,"records":1,"scope":"project","project_id":"p\u001b[2J",
 "by_service":[{"service":"x\u001b[31m","cost":1,"cost_with_nds":1.2}]}`)
	if strings.Contains(got, "\x1b") {
		t.Errorf("escape in:\n%q", got)
	}
}
