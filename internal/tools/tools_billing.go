// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/billing"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// maxBreakdown caps how many service line-items a billing tool returns. A tenant
// can have 30+ services; an unbounded array overflows the agent's trajectory
// (OpenClaw trajectory-array-size-limit → the turn becomes non-deliverable and
// the agent "couldn't generate a response"). Top-N + a *_truncated count keeps
// the signal (biggest spenders) without the overflow.
const maxBreakdown = 12

// billingClient builds an authenticated billing/consumption client. The
// agreement id comes from the env (CLOUDRU_AGREEMENT_ID), exported by the MCP
// wrapper from .cloudru/credentials.json — same mechanism as project id.
// Consumption is filtered by the project of the argument project_id, or, with
// no agreement, of the configuration: a key of a project administrator
// reads the consumption of its project without the agreement id, which it
// cannot see (api-ref_start; register BIL-5). Neither — CONFIG_INVALID
// before the token (release audit, L-06).
func (s *Registry) billingClient(ctx context.Context, args map[string]any) (*billing.Client, error) {
	agreement := os.Getenv("CLOUDRU_AGREEMENT_ID")
	project := stringArg(args, "project_id")
	eng, err := s.newEngine(nil)
	if err != nil {
		return nil, err
	}
	if agreement == "" && project == "" {
		project = eng.Config.ProjectID
	}
	if agreement == "" && project == "" {
		return nil, billing.ErrNoAgreement()
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return billing.New(tok.Value, agreement, "", "").WithProject(project), nil
}

// billingScope says which filter the consumption was read by: "agreement"
// (the whole agreement) or "project" (the project, within the agreement if
// one is set too), with the ids.
func billingScope(out map[string]any, cli *billing.Client) {
	out["scope"] = "agreement"
	if cli.ProjectID() != "" {
		out["scope"] = "project"
		out["project_id"] = cli.ProjectID()
	}
	if cli.AgreementID() != "" {
		out["agreement_id"] = cli.AgreementID()
	}
}

func (s *Registry) handleBillingBalance(ctx context.Context, args map[string]any) (string, bool) {
	// The balance belongs to the agreement: no API gives it by project.
	// Before the engine and the token, as before (L-06).
	if os.Getenv("CLOUDRU_AGREEMENT_ID") == "" {
		return errResult(billing.ErrBalanceNoAgreement()), true
	}
	cli, err := s.billingClient(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	data, err := cli.Balance(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(data), false
}

// billNum pulls a numeric value from an item, trying several field names,
// a number or a string. Consumption rows are read by the spec «Контроль
// затрат» v2: amount — «Стоимость потребления без НДС», amount_nds — «с
// НДС», cost — «Тариф потребления облачного сервиса без НДС», the price of a
// unit, not of the row: never a fallback for amount (a row of zero amount
// may omit the field, and its tariff would be summed instead).
func billNum(item map[string]any, keys ...string) float64 {
	for _, k := range keys {
		v, ok := item[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return n
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

func billStr(item map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := item[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (s *Registry) handleBillingConsumption(ctx context.Context, args map[string]any) (string, bool) {
	start := stringArg(args, "start")
	if start == "" {
		return usageT("'start' is required (YYYY-MM-DD)", "нужен 'start' (ГГГГ-ММ-ДД)"), true
	}
	end := stringArg(args, "end")
	if end == "" {
		end = start
	}
	// Dates YYYY-MM-DD, end not before start: "2026-13-45", "yesterday" and
	// a timestamp went to the API and the answer was a total of 0 (QA-5).
	if _, _, err := periodArgs(start, end); err != nil {
		return errResult(err), true
	}
	cli, err := s.billingClient(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.Consumption(ctx, start+"T00:00:00Z", end+"T23:59:59Z")
	if err != nil {
		return errResult(err), true
	}

	var total, totalNDS float64
	bySvc := map[string]float64{}
	for _, it := range items {
		amt := billNum(it, "amount")
		amtNDS := billNum(it, "amount_nds")
		total += amt
		totalNDS += amtNDS
		key := billStr(it, "servname", "servicename", "service")
		if rn := billStr(it, "resource_name", "resourcename"); rn != "" {
			key = key + " " + rn
		}
		if key == "" {
			key = "Unknown"
		}
		bySvc[key] += amt
	}

	// Sort service breakdown by cost descending.
	type svcCost struct {
		Service string  `json:"service"`
		Cost    float64 `json:"cost"`
	}
	breakdown := make([]svcCost, 0, len(bySvc))
	for k, v := range bySvc {
		breakdown = append(breakdown, svcCost{Service: k, Cost: v})
	}
	sort.Slice(breakdown, func(i, j int) bool { return breakdown[i].Cost > breakdown[j].Cost })

	// Cap the breakdown so a 30+-service array can't overflow the agent's
	// trajectory (OpenClaw trajectory-array-size-limit → non_deliverable turn).
	svcTrunc := 0
	if len(breakdown) > maxBreakdown {
		svcTrunc = len(breakdown) - maxBreakdown
		breakdown = breakdown[:maxBreakdown]
	}
	out := map[string]any{
		"period":         map[string]string{"start": start, "end": end},
		"currency":       "RUB",
		"total":          total,
		"total_with_nds": totalNDS,
		"records":        len(items),
		"by_service":     breakdown,
		"vat":            consumptionVAT,
	}
	billingScope(out, cli)
	if svcTrunc > 0 {
		out["by_service_truncated"] = svcTrunc
	}
	// When totals are zero but items exist, surface a sample item's keys so the
	// operator can see which field names the tenant actually uses (no guessing).
	if total == 0 && len(items) > 0 {
		keys := make([]string, 0, len(items[0]))
		for k := range items[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out["note"] = "total is 0 — verify field names"
		out["sample_item_keys"] = keys
	}
	return toJSON(out), false
}

// consumptionVAT says which sums of the answer include VAT.
var consumptionVAT = map[string]string{
	"total":          "excluding VAT: sum of amount",
	"total_with_nds": "including VAT: sum of amount_nds",
	"by_service":     "excluding VAT: amount",
}

// aggregateConsumption sums the cost of the rows, `amount` (excluding VAT),
// grouped by service+resource. It summed `cost` first, as the ported
// finops_report.py did; the spec calls `cost` the tariff of the service, the
// price of a unit, and `amount` the cost of the row.
func aggregateConsumption(items []map[string]any) (float64, map[string]float64) {
	total := 0.0
	bySvc := map[string]float64{}
	for _, it := range items {
		c := billNum(it, "amount")
		total += c
		key := billStr(it, "servname", "servicename", "service")
		if rn := billStr(it, "resource_name", "resourcename"); rn != "" {
			key = key + " " + rn
		}
		if key == "" {
			key = "Unknown"
		}
		bySvc[key] += c
	}
	return total, bySvc
}

// handleBillingFinops ports finops_report.py: INFORM (cost allocation by service,
// anomalies vs the previous same-length period) + QUANTIFY (daily burn-rate,
// balance runway in days, projected zero date, optional budget check). Read-only.
// maxFinopsDays bounds the window of billing_finops: a leap year.
const maxFinopsDays = 366

func (s *Registry) handleBillingFinops(ctx context.Context, args map[string]any) (string, bool) {
	const layout = "2006-01-02"
	now := time.Now().UTC()
	today := now.Format(layout)
	// days -1 became 30, a reversed period counted as one day (QA-5, QA-14).
	if err := minIntArg(args, "days", 1, i18n.T("days is the length of the window ending at end; the default is 30.", "days — длина периода, который заканчивается в end; по умолчанию 30.")); err != nil {
		return errResult(err), true
	}
	days := pgInt(args, "days", 30)
	// At most a year: the report also reads the period before, and days of
	// 10^9 went into the date arithmetic as they were (release audit,
	// REL-15).
	if days > maxFinopsDays {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("days must be at most %d, got %d", "days должен быть не больше %d, передано %d", maxFinopsDays, days),
			i18n.Tf("The report covers at most a year (%d days); for a longer one pass start and end for each year.",
				"Отчёт строится не больше чем за год (%d дней); для более долгого периода передайте start и end по годам.", maxFinopsDays))), true
	}
	end := stringArg(args, "end")
	if end == "" {
		end = today
	}
	endT, err := dateArg("end", end)
	if err != nil {
		return errResult(err), true
	}
	start := stringArg(args, "start")
	if start == "" {
		start = endT.AddDate(0, 0, -(days - 1)).Format(layout)
	}
	startT, endT, err := periodArgs(start, end)
	if err != nil {
		return errResult(err), true
	}
	periodDays := int(endT.Sub(startT).Hours()/24) + 1

	cli, err := s.billingClient(ctx, args)
	if err != nil {
		return errResult(err), true
	}

	// Current period consumption.
	cons, err := cli.Consumption(ctx, start+"T00:00:00Z", end+"T23:59:59Z")
	if err != nil {
		return errResult(err), true
	}
	total, bySvc := aggregateConsumption(cons)

	// Balance: billing.api.cloud.ru/v1/agreement is in none of Cloud.ru's
	// published specs (api-contract §4.1). If it fails, the cost part of the
	// summary still stands: the balance is "unknown", not an error. Without
	// an agreement (consumption by project) there is no balance to ask for.
	bal, balErr := cli.Balance(ctx)
	balance := billNum(bal, "balance")
	active, _ := bal["active"].(bool)

	daily := total / float64(periodDays)
	// The balance is the agreement's: against the burn of one project
	// (project_id with an agreement) the runway would be too long.
	var runway float64
	if daily > 0 && balErr == nil && cli.ProjectID() == "" {
		runway = balance / daily
	}

	// Previous same-length period → anomalies.
	prevEndT := startT.AddDate(0, 0, -1)
	prevStartT := prevEndT.AddDate(0, 0, -(periodDays - 1))
	prevStart := prevStartT.Format(layout)
	prevEnd := prevEndT.Format(layout)
	prevCons, err := cli.Consumption(ctx, prevStart+"T00:00:00Z", prevEnd+"T23:59:59Z")
	if err != nil {
		return errResult(err), true
	}
	_, bySvcPrev := aggregateConsumption(prevCons)

	type anomaly struct {
		Service  string   `json:"service"`
		Prev     float64  `json:"prev"`
		Now      float64  `json:"now"`
		DeltaPct *float64 `json:"delta_pct"`
	}
	var anomalies []anomaly
	for svc, c := range bySvc {
		p := bySvcPrev[svc]
		if p > 0 && c > p*1.5 && (c-p) > 1 {
			d := (c - p) / p * 100
			anomalies = append(anomalies, anomaly{Service: svc, Prev: p, Now: c, DeltaPct: &d})
		} else if p == 0 && c > 1 {
			anomalies = append(anomalies, anomaly{Service: svc, Prev: 0, Now: c, DeltaPct: nil})
		}
	}
	sort.Slice(anomalies, func(i, j int) bool { return anomalies[i].Now > anomalies[j].Now })

	// Service breakdown sorted desc.
	type svcCost struct {
		Service string  `json:"service"`
		Cost    float64 `json:"cost"`
	}
	breakdown := make([]svcCost, 0, len(bySvc))
	for k, v := range bySvc {
		breakdown = append(breakdown, svcCost{Service: k, Cost: v})
	}
	sort.Slice(breakdown, func(i, j int) bool { return breakdown[i].Cost > breakdown[j].Cost })

	// Cap arrays so a 30+-service breakdown / long anomaly list can't overflow
	// the agent trajectory (trajectory-array-size-limit → non-deliverable turn).
	svcTrunc, anomTrunc := 0, 0
	if len(breakdown) > maxBreakdown {
		svcTrunc = len(breakdown) - maxBreakdown
		breakdown = breakdown[:maxBreakdown]
	}
	if len(anomalies) > maxBreakdown {
		anomTrunc = len(anomalies) - maxBreakdown
		anomalies = anomalies[:maxBreakdown]
	}

	out := map[string]any{
		"period":      map[string]any{"start": start, "end": end, "days": periodDays},
		"prev_period": map[string]any{"start": prevStart, "end": prevEnd},
		"currency":    "RUB",
		"balance":     balance,
		"active":      active,
		"total_cost":  total,
		"daily_burn":  daily,
		"by_service":  breakdown,
		"anomalies":   anomalies,
		"line_items":  len(cons),
	}
	billingScope(out, cli)
	out["vat"] = map[string]string{"total_cost": "excluding VAT: sum of amount", "by_service": "excluding VAT: amount"}
	if balErr != nil {
		out["balance"], out["active"] = nil, nil
		if cli.AgreementID() == "" {
			out["balance_error"] = "balance unavailable: it needs the billing agreement (CLOUDRU_AGREEMENT_ID), the API has no balance by project"
		} else {
			out["balance_error"] = "balance unavailable (undocumented billing endpoint): " + balErr.Error()
		}
	}
	if svcTrunc > 0 {
		out["by_service_truncated"] = svcTrunc
	}
	if anomTrunc > 0 {
		out["anomalies_truncated"] = anomTrunc
	}
	if runway > 0 {
		out["runway_days"] = runway
		out["projected_zero_date"] = now.AddDate(0, 0, int(runway)).Format(layout)
	} else {
		out["runway_days"] = nil
		out["projected_zero_date"] = nil
		if balErr == nil && cli.ProjectID() != "" {
			out["runway_note"] = "runway not computed: the balance is the agreement's, the costs are the project's"
		}
	}
	if budget := billNum(args, "budget"); budget > 0 {
		proj := daily * 30
		out["budget"] = map[string]any{
			"monthly":           budget,
			"projected_monthly": proj,
			"over":              proj > budget,
			"pct":               proj / budget * 100,
		}
	}
	return toJSON(out), false
}
