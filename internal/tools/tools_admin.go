// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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

// adminGETRaw preserves the HTTP status and page structure for list readers.
func (s *Registry) adminGETRaw(ctx context.Context, rawurl string) ([]byte, int, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	return tokenGETRaw(ctx, eng.Auth, rawurl)
}

// tokenGET is adminGET with the token of tp.
func tokenGET(ctx context.Context, tp auth.TokenProvider, rawurl string) (map[string]any, error) {
	data, status, err := tokenGETRaw(ctx, tp, rawurl)
	if err != nil {
		return nil, err
	}
	if err := pager.PageStatus(status, data, false, adminStatusError); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return nil, pager.Incomplete(i18n.T("invalid administration API response", "неверный ответ API администрирования"))
	}
	return m, nil
}

func tokenGETRaw(ctx context.Context, tp auth.TokenProvider, rawurl string) ([]byte, int, error) {
	tok, err := tp.Token(ctx)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	resp, err := OrgClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	return httpretry.ReadAPIResponse(req, resp, 0)
}

func adminStatusError(status int, data []byte) error {
	var msg struct{ Message string }
	if err := json.Unmarshal(data, &msg); err != nil {
		return &adminHTTPError{Status: status, Msg: string(data[:min(len(data), 300)])}
	}
	return &adminHTTPError{Status: status, Msg: msg.Message}
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
	out := make([]map[string]any, 0)
	seen := make(map[string]bool)
	err := pager.All("iam: service accounts", func(token string) (string, error) {
		u := "https://iam.api.cloud.ru/api/v2/serviceAccounts?target.projectId=" + neturl.QueryEscape(pid)
		if token != "" {
			u += "&pageToken=" + neturl.QueryEscape(token)
		}
		data, status, err := s.adminGETRaw(ctx, u)
		if err != nil {
			return "", err
		}
		if err := pager.PageStatus(status, data, token != "", adminStatusError); err != nil {
			return "", err
		}
		page, next, err := pager.ObjectPage[map[string]any](data, "serviceAccounts", "nextPageToken",
			pager.PageFields{Items: "service_accounts", Token: "next_page_token"})
		if err != nil {
			return "", err
		}
		previous := len(seen)
		for _, account := range page {
			id, _ := account["id"].(string)
			if strings.TrimSpace(id) == "" {
				return "", pager.Incomplete(i18n.T("service account has no ID", "у сервисного аккаунта нет ID"))
			}
			if name, present := account["name"]; present {
				if _, ok := name.(string); !ok {
					return "", pager.Incomplete(i18n.T("service account name must be a string", "имя сервисного аккаунта должно быть строкой"))
				}
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, map[string]any{
					"id": id, "name": account["name"], "email": account["email"],
					"type": account["type"], "enabled": account["enabled"],
				})
			}
		}
		if token != "" && len(page) > 0 && previous == len(seen) {
			return "", pager.NoNewItems(previous, -1)
		}
		return next, nil
	})
	if err != nil {
		return errResult(err), true
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
// call (orgHTTPError), not a property of the key. A project-bound registry
// (SetProjectBound) lists the active project only.
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
	for _, p := range raw {
		// Bound to its project (SetProjectBound), the registry names no
		// other project of the organization.
		id, _ := p["id"].(string)
		if s.projectBound && strings.TrimSpace(id) == "" {
			return failed(ctx, pager.Incomplete(i18n.T("project without an ID", "проект без ID")), nil), true
		}
		if s.projectBound && id != active {
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
func orgProjectPages(ctx context.Context, tp auth.TokenProvider) (raw []map[string]any, laterPage bool, err error) {
	// An empty page ends the list of the Organization API (IAM-10).
	err = pager.AllUntilEmpty("organization: projects", pager.MaxPages, func(token string) (string, int, error) {
		u := OrgProjectsURL
		if token != "" {
			u += "?page_token=" + neturl.QueryEscape(token)
		}
		laterPage = token != ""
		body, status, err := tokenGETRaw(ctx, tp, u)
		if err != nil {
			return "", 0, err
		}
		if err := pager.PageStatus(status, body, laterPage, adminStatusError); err != nil {
			return "", 0, err
		}
		page, next, err := pager.ObjectPage[map[string]any](body, "projects", "next_page_token")
		if err != nil {
			return "", 0, err
		}
		raw = append(raw, page...)
		return next, len(page), nil
	})
	if err != nil {
		return nil, laterPage, err
	}
	return raw, false, nil
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
	code, hint := clierrors.CodeAPIError, i18n.T("Cloud.ru Organization API failed; retry later. The key still works in the active project (whoami).",
		"Ошибка Organization API Cloud.ru. Повторите запрос позже. Ключ по-прежнему работает в активном проекте (evo whoami).")
	switch {
	case he.Status == http.StatusUnauthorized:
		code, hint = clierrors.CodeAuthFailed, i18n.T("Check the key (evo whoami); re-create it in the Cloud.ru console if it was revoked.",
			"Проверьте ключ командой evo whoami. Если ключ отозван, создайте новый в консоли Cloud.ru.")
	case he.Status == http.StatusTooManyRequests:
		code, hint = clierrors.CodeAPIRateLimited, i18n.T("Too many requests; retry in a minute.", "Слишком много запросов. Повторите через минуту.")
	}
	return clierrors.Wrap(code, "Organization API: "+he.Error(), hint, he)
}

// quotaUsagesURL is the quota usages of the organization API, of the
// project when projectID is set.
func quotaUsagesURL(projectID string) string {
	url := "https://organization.api.cloud.ru/v3/quotas/usages?page_size=200"
	if projectID != "" {
		url += "&subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=" + neturl.QueryEscape(projectID)
	}
	return url
}

// QuotaProbe is engine.Engine.QuotaProbe: it reads the project's
// quota usages as org_quotas does; a 403 is engine.ErrQuotasForbidden — a
// key of a project reads none (P37, D60).
func QuotaProbe(ctx context.Context, tp auth.TokenProvider, projectID string) error {
	_, err := tokenGET(ctx, tp, quotaUsagesURL(projectID))
	var he *adminHTTPError
	if errors.As(err, &he) && he.Status == http.StatusForbidden {
		return fmt.Errorf("%w: %s", engine.ErrQuotasForbidden, he.Msg)
	}
	return err
}

// handleOrgQuotas lists quota usages (limits vs usage) — useful for capacity
// planning. Requires the caller to have customer/project quota permissions.
func (s *Registry) handleOrgQuotas(ctx context.Context, args map[string]any) (string, bool) {
	projectID := stringArg(args, "project_id")
	url := quotaUsagesURL(projectID)
	var raw []map[string]any
	firstPage := true
	// The same resource type is a distinct quota for each subject. Across
	// projects a row without its subject cannot prove repetition.
	type quotaKey struct{ customer, kind, subject, resource string }
	seen := make(map[quotaKey]bool)
	err := pager.All("organization: quota usages", func(token string) (string, error) {
		firstPage = token == ""
		u := url
		if !firstPage {
			u += "&page_token=" + neturl.QueryEscape(token)
		}
		body, status, err := s.adminGETRaw(ctx, u)
		if err != nil {
			return "", err
		}
		if err := pager.PageStatus(status, body, !firstPage, adminStatusError); err != nil {
			return "", err
		}
		page, next, err := pager.ObjectPage[map[string]any](body, "usages", "next_page_token")
		if err != nil {
			return "", err
		}
		previous := len(raw)
		for _, row := range page {
			code, _ := row["resource_type_code"].(string)
			if strings.TrimSpace(code) == "" {
				return "", pager.Incomplete(i18n.T("quota has no resource type code", "у квоты нет кода вида ресурса"))
			}
			subject, _ := row["subject_id"].(string)
			if subject = firstNonEmptyStr(subject, projectID); subject != "" {
				customer, _ := row["customer_id"].(string)
				kind, _ := row["subject_type_code"].(string)
				key := quotaKey{customer, kind, subject, code}
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			raw = append(raw, row)
		}
		if !firstPage && len(page) != 0 && len(raw) == previous {
			return "", pager.NoNewItems(previous, -1)
		}
		return next, nil
	})
	if err != nil {
		var he *adminHTTPError
		if firstPage && errors.As(err, &he) && he.Status == http.StatusForbidden {
			// reason is a code, as everywhere else; the phrase is the note
			// (QA 20.10, QA-6).
			return toJSON(map[string]any{
				"status":       "unavailable",
				"quota_usages": []any{},
				"reason":       clierrors.CodeAPIError,
				"note": i18n.T("this service account lacks customer/organization quota scope; quota usages are not readable with the current credentials",
					"у сервисного аккаунта нет прав на квоты организации: использование квот этим ключом не прочитать"),
				"detail": err.Error(),
			}), false
		}
		// Preserve the service refusal on the first request. A missing
		// continuation was already classified as an incomplete list.
		if !firstPage && errors.As(err, &he) {
			err = orgHTTPError(he)
		}
		return errResult(err), true
	}
	types := s.quotaTypes(ctx)
	out := make([]map[string]any, 0, len(raw))
	for _, u := range raw {
		row := map[string]any{
			"resource": u["resource_type_code"], "limit": u["individual_limit"], "usage": u["individual_usage"],
		}
		code, _ := u["resource_type_code"].(string)
		for k, v := range types[code] {
			row[k] = v
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"quota_usages": out}), false
}

// quotaTypes are the name, the unit and the product of each quota type
// by its code, from the catalogue of quota types (FB15-8); nil when it is
// not read — the rows keep their codes. The catalogue is one answer, not
// paged (organization api.op.v3.yaml, v3ListQuotaResourceTypesResponse:
// resource_types of v3QuotaResourceType — code, name, product_name, unit).
func (s *Registry) quotaTypes(ctx context.Context) map[string]map[string]any {
	m, err := s.adminGET(ctx, "https://organization.api.cloud.ru/v3/quotas/resourceTypes")
	if err != nil {
		return nil
	}
	items, _ := m["resource_types"].([]any)
	out := map[string]map[string]any{}
	for _, x := range items {
		t, _ := x.(map[string]any)
		code, _ := t["code"].(string)
		if code == "" {
			continue
		}
		named := map[string]any{}
		for field, key := range map[string]string{"name": "name", "unit": "unit", "product_name": "product"} {
			if v, _ := t[field].(string); v != "" {
				named[key] = v
			}
		}
		out[code] = named
	}
	return out
}

// OrgClient is the HTTP client of the Organization API, for every reader of
// it (the tools of org and evo configure): the Organization API answers 404
// to a key that cannot list projects (handleOrgProjects), in a form not
// probed, so a 404 stays a 404 (D63, Keep404) — one policy, not two
// (FB160-3). It repeats a GET on a dropped connection or a 502/503/504, as
// the service clients do.
func OrgClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &httpretry.Transport{Policy: httpretry.Policy{Keep404: true}}}
}
