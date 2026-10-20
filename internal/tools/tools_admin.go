// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// adminGET performs an authenticated GET against an absolute Cloud.ru API URL
// and returns the decoded JSON. Used for the read-only IAM / Organization
// admin endpoints (they share the same IAM bearer token).
func (s *Registry) adminGET(ctx context.Context, rawurl string) (map[string]any, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	return tokenGET(ctx, eng.Auth, rawurl)
}

// tokenGET is adminGET with the token of tp.
func tokenGET(ctx context.Context, tp auth.TokenProvider, rawurl string) (map[string]any, error) {
	tok, err := tp.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	cl := httpretry.NewClient(30 * time.Second)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if e := json.Unmarshal(data, &m); e != nil {
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: string(data[:min(len(data), 300)])}
	}
	if resp.StatusCode != 200 {
		msg, _ := m["message"].(string)
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: msg}
	}
	return m, nil
}

// adminHTTPError is an answer of the API other than 200 (or not JSON): the
// API was reached and refused, unlike a network error.
type adminHTTPError struct {
	Status int
	Msg    string
}

func (e *adminHTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

// projectID returns the configured project id.
func (s *Registry) projectID(ctx context.Context) string {
	eng, err := s.newEngine(nil)
	if err != nil {
		return ""
	}
	return eng.Config.ProjectID
}

// handleIamServiceAccounts lists service accounts for a project (or the current
// one). This is where a k8s cluster_sa_id can be discovered.
func (s *Registry) handleIamServiceAccounts(ctx context.Context, args map[string]any) (string, bool) {
	pid := firstNonEmptyStr(stringArg(args, "project_id"), s.projectID(ctx))
	// Every page (#98). The next page is asked for by pageToken, as
	// iam-gw-public.yaml names it; the answer is read in both spellings
	// (serviceAccounts/nextPageToken in the spec, service_accounts/
	// next_page_token live, IAM-2). Only one spelling of the query is
	// sent: a parameter the API does not know can fail the call (AUD-2).
	var raw []any
	err := pager.All("iam: service accounts", func(token string) (string, error) {
		u := "https://iam.api.cloud.ru/api/v2/serviceAccounts?target.projectId=" + neturl.QueryEscape(pid)
		if token != "" {
			u += "&pageToken=" + neturl.QueryEscape(token)
		}
		m, err := s.adminGET(ctx, u)
		if err != nil {
			return "", err
		}
		page, _ := m["service_accounts"].([]any)
		if page == nil {
			page, _ = m["serviceAccounts"].([]any)
		}
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		if next == "" {
			next, _ = m["nextPageToken"].(string)
		}
		return next, nil
	})
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		sa, _ := x.(map[string]any)
		if sa == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": sa["id"], "name": sa["name"], "email": sa["email"],
			"type": sa["type"], "enabled": sa["enabled"],
		})
	}
	return toJSON(map[string]any{"service_accounts": out}), false
}

// OrgProjectsURL is the project list of the Organization API (a variable
// for the tests of the CLI, whose 'evo project list' calls org_projects).
var OrgProjectsURL = "https://organization.api.cloud.ru/v3/projects"

// handleOrgProjects lists projects the caller can see. A key the
// Organization API does not list projects for (a project-level service
// account: 400 "user not found", 14.10.2026) is not a failure — the key
// works in its project. As 'evo project list' does (QA r3 C3), the answer
// is then projects: null, the active project and a note, not an error
// (it was NOT_FOUND, exit 4, from "user not found" in the message). Only
// 400/403/404 are that case: a 401, a 429 or a 5xx is a failure of the
// call (orgHTTPError), not a property of the key.
func (s *Registry) handleOrgProjects(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return failed(ctx, err, nil), true
	}
	// Only a refusal of the first page says the key cannot list projects;
	// one of a later page is a failure of the call.
	raw, laterPage, err := orgProjectPages(ctx, eng.Auth)
	var he *adminHTTPError
	if errors.As(err, &he) && (laterPage || he.Status != http.StatusBadRequest && he.Status != http.StatusForbidden && he.Status != http.StatusNotFound) {
		return failed(ctx, orgHTTPError(he), nil), true
	}
	active := eng.Config.ProjectID
	if errors.As(err, &he) {
		hint := "Not an error: the key works in the active project (whoami). Pass project_id where a tool takes one."
		if active == "" {
			// No active project: "the key works in the active project" was
			// wrong exactly where it was shown (release audit, R-13).
			hint = orgNoProjectHint
		}
		return toJSON(map[string]any{
			"projects":    nil,
			"active":      active,
			"note":        ScopeText(0, false),
			"http_status": he.Status,
			"hint":        hint,
		}), false
	}
	if err != nil {
		return failed(ctx, err, nil), true
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		p, _ := x.(map[string]any)
		if p == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": p["id"], "name": p["name"], "status": p["status"],
			"organization_unit_id": p["organization_unit_id"],
		})
	}
	// active: the project commands work in, as 'evo project list' marks
	// it (spec REQ-4).
	return toJSON(map[string]any{"projects": out, "active": active}), false
}

// orgProjectPages reads every page of the project list of the Organization
// API with the token of tp (#98): api.op.v3.yaml answers next_page_token
// and takes page_token. laterPage: err is the failure of a page after the
// first.
func orgProjectPages(ctx context.Context, tp auth.TokenProvider) (raw []any, laterPage bool, err error) {
	err = pager.All("organization: projects", func(token string) (string, error) {
		u := OrgProjectsURL
		if token != "" {
			u += "?page_token=" + neturl.QueryEscape(token)
		}
		m, err := tokenGET(ctx, tp, u)
		if err != nil {
			laterPage = token != ""
			return "", err
		}
		page, _ := m["projects"].([]any)
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		return next, nil
	})
	return raw, laterPage, err
}

// orgNoProjectHint is the hint of org_projects when the key cannot list
// projects and none is configured (docs/auth.md, «UUID проекта»).
const orgNoProjectHint = "No project is set, and this key cannot list projects (a service-account key never can). " +
	"Take the project UUID from the Cloud.ru console (the project where the service account has its roles) and set it: " +
	"evo configure --project-id <uuid>, CLOUDRU_PROJECT_ID, or project_id where a tool takes one."

// orgHTTPError gives an answer of the Organization API its reason: 401 —
// AUTH_FAILED, 429 — API_RATE_LIMITED, the rest — API_ERROR (not
// NOT_FOUND from a "not found" in the body).
func orgHTTPError(he *adminHTTPError) error {
	code, hint := clierrors.CodeAPIError, "Cloud.ru Organization API failed; retry later. The key still works in the active project (whoami)."
	switch {
	case he.Status == http.StatusUnauthorized:
		code, hint = clierrors.CodeAuthFailed, "Check the key (evo whoami); re-create it in the Cloud.ru console if it was revoked."
	case he.Status == http.StatusTooManyRequests:
		code, hint = clierrors.CodeAPIRateLimited, "Too many requests; retry in a minute."
	}
	return clierrors.Wrap(code, "Organization API: "+he.Error(), hint, he)
}

// handleOrgQuotas lists quota usages (limits vs usage) — useful for capacity
// planning. Requires the caller to have customer/project quota permissions.
func (s *Registry) handleOrgQuotas(ctx context.Context, args map[string]any) (string, bool) {
	url := "https://organization.api.cloud.ru/v3/quotas/usages?page_size=200"
	if pid := stringArg(args, "project_id"); pid != "" {
		url += "&subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=" + neturl.QueryEscape(pid)
	}
	// Every page (#98): next_page_token in the answer, page_token in the
	// next request (api.op.v3.yaml).
	var raw []any
	err := pager.All("organization: quota usages", func(token string) (string, error) {
		u := url
		if token != "" {
			u += "&page_token=" + neturl.QueryEscape(token)
		}
		m, err := s.adminGET(ctx, u)
		if err != nil {
			return "", err
		}
		page, _ := m["usages"].([]any)
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		return next, nil
	})
	if err != nil {
		// The org quota endpoint needs a customer/organization scope a service
		// account typically lacks (403 "dont have permissions to customers").
		// That's an expected limitation, not a failure — degrade to a soft
		// "unavailable" so it doesn't read as a hard error on an early turn.
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "403") || strings.Contains(low, "permission") {
			// reason is a code, as everywhere else; the phrase is the note
			// (QA 20.10, QA-6).
			return toJSON(map[string]any{
				"status":       "unavailable",
				"quota_usages": []any{},
				"reason":       clierrors.CodeAPIError,
				"note":         "this service account lacks customer/organization quota scope; quota usages are not readable with the current credentials",
				"detail":       err.Error(),
			}), false
		}
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		u, _ := x.(map[string]any)
		if u == nil {
			continue
		}
		out = append(out, map[string]any{
			"resource": u["resource_type_code"], "limit": u["individual_limit"], "usage": u["individual_usage"],
		})
	}
	return toJSON(map[string]any{"quota_usages": out}), false
}
