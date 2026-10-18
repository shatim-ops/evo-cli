// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"time"

	"github.com/shatim-ops/evo-cli/internal/audit"
)

// auditClient builds an authenticated Audit Logging client.
func (s *Registry) auditClient(ctx context.Context) (*audit.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return audit.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleAuditEvents(ctx context.Context, args map[string]any) (string, bool) {
	// Default window 1h; the API recommends short spans for chatty projects.
	since, err := parseSince(stringArg(args, "since"))
	if err != nil {
		return usageResult(err), true
	}
	cli, err := s.auditClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	end := time.Now()
	opts := audit.ListOptions{
		Start:     end.Add(-since),
		End:       end,
		PageSize:  pgInt(args, "page_size", 100),
		PageToken: stringArg(args, "page_token"),
	}
	page, err := cli.List(ctx, opts)
	if err != nil {
		return errResult(err), true
	}
	out := map[string]any{
		"project": cli.ProjectID(),
		"window":  since.String(),
		"count":   len(page.Events),
		"events":  page.Events,
	}
	if page.NextPageToken != "" {
		out["next_page_token"] = page.NextPageToken
	}
	return toJSON(out), false
}
