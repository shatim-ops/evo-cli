// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// listPageSize is the documented maximum of limit on compute lists
// (openapi-v3: default 50, maximum 100); listMaxPages bounds the walk.
const (
	listPageSize = 100
	listMaxPages = 100
)

// listProjectScoped fetches every page of a project-scoped list endpoint.
// listRaw is listProjectScoped for answers kept as raw objects.
func listRaw(a *CloudRuAdapter, ctx context.Context, path, projectID string) ([]map[string]any, error) {
	items, err := listProjectScoped[map[string]any](a, ctx, path, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = *it
	}
	return out, nil
}

// listProjectScoped lists a compute collection of the project. The API
// requires project_id there: without one nothing is sent, and the caller
// gets CONFIG_INVALID instead of the API's 422 (release audit, R-06).
func listProjectScoped[T any](a *CloudRuAdapter, ctx context.Context, path, projectID string) ([]*T, error) {
	if projectID == "" {
		return nil, clierrors.ProjectRequired()
	}
	return listAll[T](a, ctx, path, url.Values{"project_id": {projectID}}, true)
}

// listAll walks a compute list by offset with limit=100 until total
// (sending limit=200, as before, is over the documented maximum and left
// projects with more than a page of subnets, floating IPs or images with a
// partial list — docs/internal/api-contract.md §4.5, §4.7). It decodes the
// documented {items,total} envelope; a bare JSON array is taken as the
// whole list. paged=false sends neither limit nor offset (disk-types has
// no such parameters).
func listAll[T any](a *CloudRuAdapter, ctx context.Context, path string, q url.Values, paged bool) ([]*T, error) {
	var out []*T
	offset := 0
	for range listMaxPages {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		if paged {
			pq.Set("limit", fmt.Sprint(listPageSize))
			if offset > 0 {
				pq.Set("offset", fmt.Sprint(offset))
			}
		}
		full := path
		if enc := pq.Encode(); enc != "" {
			full += "?" + enc
		}
		resp, err := a.doWithRetry(ctx, http.MethodGet, full, nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxListBody))
		resp.Body.Close()
		if err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.Tf("failed to read response for %s", "не удалось прочитать ответ %s", path), hintUnexpectedFormat(), err)
		}

		var page paginatedResponse[T]
		if err := json.Unmarshal(body, &page); err == nil && page.Items != nil {
			for i := range page.Items {
				out = append(out, &page.Items[i])
			}
			offset += len(page.Items)
			// Без total (спецификация его обещает) — до неполной страницы.
			done := offset >= page.Total
			if page.Total == 0 {
				done = len(page.Items) < listPageSize
			}
			if !paged || len(page.Items) == 0 || done {
				return out, nil
			}
			continue
		}
		var arr []T
		if err := json.Unmarshal(body, &arr); err == nil {
			for i := range arr {
				out = append(out, &arr[i])
			}
			return out, nil
		}
		return nil, clierrors.New(clierrors.CodeAPIError, i18n.Tf("failed to decode list response for %s", "не удалось разобрать список %s", path), hintUnexpectedFormatVerbose())
	}
	return out, nil
}

// ListVMsInProject lists VMs scoped to a project (the /vms endpoint requires
// project_id in this tenant).
func (a *CloudRuAdapter) ListVMsInProject(ctx context.Context, projectID string) ([]*VM, error) {
	return listProjectScoped[VM](a, ctx, "/api/v1/vms", projectID)
}

// DeleteVMInProject deletes a VM by ID, passing project_id.
func (a *CloudRuAdapter) DeleteVMInProject(ctx context.Context, id, projectID string) error {
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// AddSecurityGroupRule creates one rule via
// POST /api/v1/security-groups/{id}/rules and returns it as created. The
// documented answer is 201 with the rule (id required); a body that does not
// decode yields a rule without an ID rather than an error — the rule exists.
func (a *CloudRuAdapter) AddSecurityGroupRule(ctx context.Context, sgID, projectID string, rule SGRule) (*SGRule, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to marshal SG rule", "не удалось сформировать правило группы безопасности"), i18n.T("Check parameters", "Проверьте параметры"), err)
	}
	path := fmt.Sprintf("/api/v1/security-groups/%s/rules", seg(sgID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var created SGRule
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&created); err != nil {
		created = SGRule{}
	}
	return &created, nil
}

const (
	// sgRulesPageSize is the API's maximum limit for the rules list.
	sgRulesPageSize = 100
	// sgRulesMaxPages bounds the walk: a server that keeps answering full
	// pages must not keep the CLI looping.
	sgRulesMaxPages = 100
	maxListBody     = 8 << 20
)

// ListSecurityGroupRules lists a group's rules via
// GET /api/v1/security-groups/{id}/rules, following offset pages. The group
// list (GET /api/v1/security-groups) carries only rule counts.
func (a *CloudRuAdapter) ListSecurityGroupRules(ctx context.Context, sgID string) ([]*SGRule, error) {
	var out []*SGRule
	offset := 0
	for range sgRulesMaxPages {
		path := fmt.Sprintf("/api/v1/security-groups/%s/rules?offset=%d&limit=%d", seg(sgID), offset, sgRulesPageSize)
		resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var page paginatedResponse[SGRule]
		err = json.NewDecoder(io.LimitReader(resp.Body, maxListBody)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode security group rules", "не удалось разобрать правила группы безопасности"), hintUnexpectedFormatVerbose(), err)
		}
		for i := range page.Items {
			out = append(out, &page.Items[i])
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return out, nil
		}
	}
	return out, nil
}

// DeleteSecurityGroupRule deletes one rule via
// DELETE /api/v1/security-groups/{id}/rules/{rule_id} (204; 404 when the
// rule is gone, which the adapter reports as CodeVMNotFound).
func (a *CloudRuAdapter) DeleteSecurityGroupRule(ctx context.Context, sgID, ruleID string) error {
	path := fmt.Sprintf("/api/v1/security-groups/%s/rules/%s", seg(sgID), seg(ruleID))
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListDiskTypes lists disk types via GET /api/v1/disk-types.
func (a *CloudRuAdapter) ListDiskTypes(ctx context.Context, projectID string) ([]*DiskType, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	return listAll[DiskType](a, ctx, "/api/v1/disk-types", q, false)
}

// ListSubnets lists subnets via GET /api/v1/subnets.
func (a *CloudRuAdapter) ListSubnets(ctx context.Context, projectID string) ([]*Subnet, error) {
	return listProjectScoped[Subnet](a, ctx, "/api/v1/subnets", projectID)
}

// ListSecurityGroups lists security groups via GET /api/v1/security-groups.
func (a *CloudRuAdapter) ListSecurityGroups(ctx context.Context, projectID string) ([]*SecurityGroup, error) {
	return listProjectScoped[SecurityGroup](a, ctx, "/api/v1/security-groups", projectID)
}

// ListFloatingIPs lists floating IPs via GET /api/v1/floating-ips.
func (a *CloudRuAdapter) ListFloatingIPs(ctx context.Context, projectID string) ([]*FloatingIP, error) {
	return listProjectScoped[FloatingIP](a, ctx, "/api/v1/floating-ips", projectID)
}

// AttachFloatingIP binds an existing floating IP to a VM interface via
// PUT /api/v1/floating-ips/{id} (floating-IP "variant B": attach after the VM
// is running). Returns the updated floating IP.
func (a *CloudRuAdapter) AttachFloatingIP(ctx context.Context, fipID, projectID, interfaceID, name string) (*FloatingIP, error) {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(fipID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	body, err := json.Marshal(map[string]string{"interface_id": interfaceID, "name": name})
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to marshal attach request", "не удалось сформировать запрос на привязку floating IP"), i18n.T("Check parameters", "Проверьте параметры"), err)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		// PUT may return 204/empty; treat decode failure as soft success.
		return &FloatingIP{ID: fipID}, nil
	}
	return &fip, nil
}

// DeleteFloatingIP releases a floating IP via DELETE /api/v1/floating-ips/{id}.
func (a *CloudRuAdapter) DeleteFloatingIP(ctx context.Context, fipID, projectID string) error {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(fipID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
