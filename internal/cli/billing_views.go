// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// The billing commands (balance, consumption, finops) print a report for a
// human, not the generic FIELD/VALUE table with raw floats and the English
// keys of the answer (dogfood №5): whole rubles with a digit separator, the
// period as dates and days, Russian words for the answer's state. The
// totals carry both sums, including and excluding VAT; the rows of the
// services and of the growth are including VAT, as the price preview and
// the invoice (owner's decision 21.10). The runway and the budget are on
// the sums excluding VAT, as the tool computes them, and say so. --json
// prints the tool's answer as is, at full precision.

// rub is a sum in whole rubles with a digit separator and ₽: "14 998 ₽"
// ("14,998 ₽" in English); a sum under a ruble is "< 1 ₽", not a zero. The
// digits come from the float itself, not an int64: a sum beyond int64
// printed its overflow, a negative sum (Cursor review of 1.2.2, CUR122-6).
// A value that is no number (NaN, ±Inf from a numeric string) is "n/a".
func rub(v float64) string {
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		return i18n.T("n/a", "н/д")
	case v > 0 && v < 1:
		return "< 1 ₽"
	case v < 0 && v > -1:
		return "> -1 ₽"
	}
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	sep := i18n.T(",", " ")
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(d)
	}
	return sign + b.String() + " ₽"
}

// withAndWithoutVAT is a total as both sums: "14 998 ₽ с НДС · 12 498 ₽ без
// НДС".
func withAndWithoutVAT(withVAT, withoutVAT float64) string {
	return i18n.Tf("%s with VAT · %s without VAT", "%s с НДС · %s без НДС", rub(withVAT), rub(withoutVAT))
}

// billingNow is the clock of the period's year; a test sets it.
var billingNow = time.Now

const billingDay = "2006-01-02"

// billingPeriod is a period as "22.09–21.10 (30 дн.)" (billingDates and
// its days).
func billingPeriod(start, end string, now time.Time) string {
	dates, days := billingDates(start, end, now)
	if days == 0 {
		return dates
	}
	return dates + i18n.Tf(" (%d days)", " (%d дн.)", days)
}

// billingDates is a period as "22.09–21.10" and its days: the dates without
// the year when the period is in the current one, with it otherwise; one
// date for a day. A date that is not YYYY-MM-DD is shown as it is, with 0
// days.
func billingDates(start, end string, now time.Time) (string, int) {
	s, err1 := time.Parse(billingDay, start)
	e, err2 := time.Parse(billingDay, end)
	if err1 != nil || err2 != nil {
		return start + "–" + end, 0
	}
	layout := "02.01.2006"
	if s.Year() == now.Year() && e.Year() == now.Year() {
		layout = "02.01"
	}
	days := int(e.Sub(s).Hours()/24) + 1
	if days == 1 {
		return s.Format(layout), days
	}
	return s.Format(layout) + "–" + e.Format(layout), days
}

// billingDate is a date of the answer as DD.MM.YYYY.
func billingDate(d string) string {
	if t, err := time.Parse(billingDay, d); err == nil {
		return t.Format("02.01.2006")
	}
	return d
}

type billingPeriodField struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// billingScopeOf is the scope of the answer in words: "договор <id>",
// "проект <id>" or "проект <id> в договоре <id>".
func billingScopeOf(scope, project, agreement string) string {
	if scope == "project" {
		s := i18n.Tf("project %s", "проект %s", project)
		if agreement != "" {
			s += i18n.Tf(" in agreement %s", " в договоре %s", agreement)
		}
		return s
	}
	return i18n.Tf("agreement %s", "договор %s", agreement)
}

// shown is the cap of a list: " (показано 12 из 94)" when the answer cut
// it, "" otherwise.
func shown(n, cut int) string {
	if cut <= 0 {
		return ""
	}
	return i18n.Tf(" (shown %d of %d)", " (показано %d из %d)", n, n+cut)
}

type billingService struct {
	Service     string  `json:"service"`
	CostWithNDS float64 `json:"cost_with_nds"`
}

// printServices prints the services of by_service with their sums
// including VAT.
func printServices(p *output.Printer, w io.Writer, svcs []billingService, cut int) {
	if len(svcs) == 0 {
		fmt.Fprintln(w, i18n.T("By service: no costs.", "По сервисам: расхода нет."))
		return
	}
	fmt.Fprintln(w, i18n.T("By service, sums with VAT", "По сервисам, суммы с НДС")+shown(len(svcs), cut)+":")
	rows := make([][]string, len(svcs))
	for i, s := range svcs {
		rows[i] = []string{s.Service, rub(s.CostWithNDS)}
	}
	p.Table([]string{i18n.T("Service", "Сервис"), i18n.T("With VAT", "С НДС")}, rows)
}

// num is a number of the answer: a JSON number or a numeric string.
func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func renderBillingFinops(p *output.Printer, w io.Writer, text string) error {
	var a struct {
		Period     *billingPeriodField `json:"period"`
		PrevPeriod billingPeriodField  `json:"prev_period"`
		Balance    *float64            `json:"balance"`
		Active     *bool               `json:"active"`
		BalanceErr string              `json:"balance_error"`
		Total      float64             `json:"total_cost"`
		TotalVAT   float64             `json:"total_cost_with_nds"`
		Daily      float64             `json:"daily_burn"`
		DailyVAT   float64             `json:"daily_burn_with_nds"`
		Services   []billingService    `json:"by_service"`
		ServCut    int                 `json:"by_service_truncated"`
		Anomalies  []struct {
			Service  string   `json:"service"`
			DeltaPct *float64 `json:"delta_pct"`
			PrevVAT  float64  `json:"prev_with_nds"`
			NowVAT   float64  `json:"now_with_nds"`
		} `json:"anomalies"`
		AnomCut    int      `json:"anomalies_truncated"`
		LineItems  int      `json:"line_items"`
		Scope      string   `json:"scope"`
		Project    string   `json:"project_id"`
		Agreement  string   `json:"agreement_id"`
		Runway     *float64 `json:"runway_days"`
		ZeroDate   string   `json:"projected_zero_date"`
		RunwayNote string   `json:"runway_note"`
		Budget     *struct {
			Monthly   float64 `json:"monthly"`
			Projected float64 `json:"projected_monthly"`
			Over      bool    `json:"over"`
			Pct       float64 `json:"pct"`
		} `json:"budget"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Period == nil {
		fmt.Fprintln(w, text)
		return nil
	}
	now := billingNow()
	fmt.Fprintln(w, output.SanitizeCell(i18n.T("FinOps report: ", "Отчёт FinOps: ")+
		billingScopeOf(a.Scope, a.Project, a.Agreement)+" · "+billingPeriod(a.Period.Start, a.Period.End, now)))

	var balance, runway string
	switch {
	case a.Balance != nil:
		balance = rub(*a.Balance)
		if a.Active != nil {
			balance += i18n.T(", agreement ", ", договор ") + activeWord(*a.Active)
		}
	case a.Agreement == "":
		balance = i18n.T("unknown: only an agreement has a balance, set CLOUDRU_AGREEMENT_ID",
			"неизвестен: баланс есть только у договора, задайте CLOUDRU_AGREEMENT_ID")
	default:
		balance = i18n.T("unavailable: ", "недоступен: ") + a.BalanceErr
	}
	switch {
	case a.Runway != nil:
		runway = i18n.Tf("%d days, until %s (by the costs without VAT)", "%d дн., до %s (по расходу без НДС)",
			int(*a.Runway), billingDate(a.ZeroDate))
	case a.RunwayNote != "":
		runway = i18n.T("not computed: the balance is the agreement's, the costs are the project's",
			"не рассчитан: баланс — договора, расход — проекта")
	case a.Balance == nil:
		runway = i18n.T("not computed: the balance is unknown", "не рассчитан: баланс неизвестен")
	default:
		runway = i18n.T("not computed: no costs", "не рассчитан: расхода нет")
	}
	lines := []cardLine{
		{i18n.T("Costs of the period", "Расход за период"), withAndWithoutVAT(a.TotalVAT, a.Total)},
		{i18n.T("Per day", "В день"), withAndWithoutVAT(a.DailyVAT, a.Daily)},
		{i18n.T("Balance", "Баланс"), balance},
		{i18n.T("Runway", "Запас"), runway},
	}
	if b := a.Budget; b != nil {
		v := i18n.Tf("%s; projected %s without VAT (%.0f%%)", "%s; прогноз %s без НДС (%.0f%%)", rub(b.Monthly), rub(b.Projected), b.Pct)
		if b.Over {
			v += i18n.T(" — over", " — превышен")
		}
		lines = append(lines, cardLine{i18n.T("Monthly budget", "Бюджет на месяц"), v})
	}
	lines = append(lines, cardLine{i18n.T("Consumption rows", "Строк потребления"), strconv.Itoa(a.LineItems)})
	printCard(w, lines)

	fmt.Fprintln(w)
	printServices(p, w, a.Services, a.ServCut)

	fmt.Fprintln(w)
	prev, _ := billingDates(a.PrevPeriod.Start, a.PrevPeriod.End, now)
	growth := i18n.T("Growth against ", "Рост к ") + prev
	if len(a.Anomalies) == 0 {
		fmt.Fprintln(w, growth+i18n.T(": none.", ": нет."))
		return nil
	}
	fmt.Fprintln(w, growth+i18n.T(", sums with VAT", ", суммы с НДС")+shown(len(a.Anomalies), a.AnomCut)+":")
	rows := make([][]string, len(a.Anomalies))
	for i, an := range a.Anomalies {
		delta := i18n.T("new", "новое")
		if an.DeltaPct != nil {
			delta = fmt.Sprintf("+%.0f%%", *an.DeltaPct)
		}
		rows[i] = []string{an.Service, rub(an.PrevVAT), rub(an.NowVAT), delta}
	}
	p.Table([]string{i18n.T("Service", "Сервис"), i18n.T("Was", "Было"), i18n.T("Now", "Стало"), i18n.T("Growth", "Рост")}, rows)
	return nil
}

func activeWord(active bool) string {
	if active {
		return i18n.T("active", "активен")
	}
	return i18n.T("not active", "не активен")
}

func renderBillingConsumption(p *output.Printer, w io.Writer, text string) error {
	var a struct {
		Period    *billingPeriodField `json:"period"`
		Total     float64             `json:"total"`
		TotalVAT  float64             `json:"total_with_nds"`
		Records   int                 `json:"records"`
		Services  []billingService    `json:"by_service"`
		ServCut   int                 `json:"by_service_truncated"`
		Scope     string              `json:"scope"`
		Project   string              `json:"project_id"`
		Agreement string              `json:"agreement_id"`
		Note      string              `json:"note"`
		Keys      []string            `json:"sample_item_keys"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Period == nil {
		fmt.Fprintln(w, text)
		return nil
	}
	fmt.Fprintln(w, output.SanitizeCell(i18n.T("Consumption: ", "Потребление: ")+
		billingScopeOf(a.Scope, a.Project, a.Agreement)+" · "+billingPeriod(a.Period.Start, a.Period.End, billingNow())))
	printCard(w, []cardLine{
		{i18n.T("Total", "Итого"), withAndWithoutVAT(a.TotalVAT, a.Total)},
		{i18n.T("Consumption rows", "Строк потребления"), strconv.Itoa(a.Records)},
	})
	fmt.Fprintln(w)
	printServices(p, w, a.Services, a.ServCut)
	if a.Note != "" {
		note := a.Note
		if len(a.Keys) > 0 {
			note += i18n.T("; fields of a row: ", "; поля строки: ") + strings.Join(a.Keys, ", ")
		}
		fmt.Fprintln(w, output.SanitizeCell(i18n.T("Note: ", "Примечание: ")+note))
	}
	return nil
}

// renderBillingBalance prints the balance of the agreement and whether it
// is active; the other fields of the agreement object are in --json. An
// answer without a balance is printed as it is.
func renderBillingBalance(_ *output.Printer, w io.Writer, text string) error {
	var a map[string]any
	_ = json.Unmarshal([]byte(text), &a)
	balance, ok := num(a["balance"])
	if !ok {
		fmt.Fprintln(w, text)
		return nil
	}
	lines := []cardLine{{i18n.T("Balance", "Баланс"), rub(balance)}}
	if active, ok := a["active"].(bool); ok {
		lines = append(lines, cardLine{i18n.T("Agreement", "Договор"), activeWord(active)})
	}
	printCard(w, lines)
	return nil
}
