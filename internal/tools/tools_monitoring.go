// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/monitoring"
)

// monitoringClient builds an authenticated Service Monitoring client.
func (s *Registry) monitoringClient(ctx context.Context) (*monitoring.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return monitoring.New(tok.Value, eng.Config.ProjectID, ""), nil
}

// parseSince turns a "since" arg into a duration: a Go duration (30m, 1h,
// 1h30m), whole days (1d), or a plain number of minutes; empty is 1h.
// Anything else is an error: since=2d silently became 1h, as did any
// value time.ParseDuration does not know (release audit, L-14).
func parseSince(arg string) (time.Duration, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return time.Hour, nil
	}
	if d, err := time.ParseDuration(arg); err == nil && d > 0 {
		return d, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(arg, "d")); err == nil && n > 0 && strings.HasSuffix(arg, "d") {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	// tolerate a plain number of minutes
	if n, err := strconv.Atoi(arg); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf(i18n.T("since %q is not a duration: use 30m, 1h, 6h, 1d or a number of minutes", "since %q — не длительность: укажите 30m, 1h, 6h, 1d или число минут"), arg)
}

// metricsMaxWindow is the longest window export_metrics answers: since=48h
// is a 400 "can't be longer than 24 hours" (release audit, L-14; the
// description promised 7 days).
const metricsMaxWindow = 24 * time.Hour

func (s *Registry) handleMetricsHistory(ctx context.Context, args map[string]any) (string, bool) {
	match := stringArg(args, "match")
	if match == "" {
		return usageT("'match' is required (a metric name / PromQL selector, e.g. cpu_usage)", "нужен 'match' — имя метрики или селектор PromQL, например cpu_usage"), true
	}
	since, err := parseSince(stringArg(args, "since"))
	if err != nil {
		return usageResult(err), true
	}
	if since > metricsMaxWindow {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("since %s: the monitoring API answers at most 24 hours back", "since %s: API мониторинга отвечает не дальше чем на 24 часа назад", since),
			i18n.T("Pass since=24h or less (e.g. 6h, 1d).", "Передайте since=24h или меньше (например 6h, 1d)."))), true
	}
	cli, err := s.monitoringClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	end := time.Now()
	start := end.Add(-since)
	series, err := cli.ExportMetrics(ctx, match, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"project": cli.ProjectID(),
		"match":   match,
		"window":  since.String(),
		"count":   len(series),
		"series":  series,
	}), false
}

func (s *Registry) handleMetricsScrape(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.monitoringClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	match := stringArg(args, "match")
	if match == "" {
		match = "{__name__=~\".+\"}"
	}
	text, err := cli.ScrapeMetrics(ctx, match)
	if err != nil {
		return errResult(err), true
	}
	// Prometheus text exposition — return as-is (may be large; caller greps).
	return toJSON(map[string]any{
		"project":    cli.ProjectID(),
		"match":      match,
		"format":     "prometheus-text",
		"exposition": text,
	}), false
}
