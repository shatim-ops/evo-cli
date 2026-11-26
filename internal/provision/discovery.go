// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
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
	q := url.Values{"project_id": {projectID}}
	for k, v := range NarrowOf(ctx, path) {
		q.Set(k, v)
	}
	items, err := listAll[T](a, ctx, path, q, true)
	return items, projectNotVisible(err, projectID)
}

type narrowKey struct{}

// Narrowed is ctx under which the project's list of path is asked with
// query parameter param=value besides its own: the list of a read that
// selects (tools, listSpecs), narrowed by the API as well. Only the list
// of that path under that ctx — a list read for a write or a cleanup has
// no such ctx and stays whole. The caller still checks the items by its
// own rule: the parameter is a speed-up, not the selection.
func Narrowed(ctx context.Context, path, param, value string) context.Context {
	old, _ := ctx.Value(narrowKey{}).(map[string]map[string]string)
	next := make(map[string]map[string]string, len(old)+1)
	for p, q := range old {
		next[p] = q
	}
	q := make(map[string]string, len(old[path])+1)
	for k, v := range old[path] {
		q[k] = v
	}
	q[param] = value
	next[path] = q
	return context.WithValue(ctx, narrowKey{}, next)
}

// NarrowOf are the query parameters Narrowed put on the list of path under
// ctx; nil without any.
func NarrowOf(ctx context.Context, path string) map[string]string {
	m, _ := ctx.Value(narrowKey{}).(map[string]map[string]string)
	return m[path]
}

// projectNotVisible is a 404 on a list of a project: Cloud.ru answers so
// for a project the key does not see, and «resource not found: GET
// /api/v1/vms» read as if the VMs were gone. The refusal names the
// project; its code stays (release plan 1.8, L3; «Риск», conclusion 43, (3)).
func projectNotVisible(err error, projectID string) error {
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || (ce.Code != clierrors.CodeVMNotFound && ce.Code != clierrors.CodeNotFound) {
		return err
	}
	return clierrors.Wrap(ce.Code,
		i18n.Tf("project %s is not visible to this key: Cloud.ru answered 404 to its list (%s)", "проект %s не виден этому ключу: Cloud.ru ответил 404 на его список (%s)", projectID, ce.Message),
		i18n.T("Check the project and the key: evo whoami names the key, evo project list the projects it sees.",
			"Проверьте проект и ключ: evo whoami называет ключ, evo project list — видимые ему проекты."), err)
}

// listAll walks a compute list by offset with limit=100 until total
// (sending limit=200, as before, is over the documented maximum and left
// projects with more than a page of subnets, floating IPs or images with a
// partial list — docs/internal/api-contract.md §4.5, §4.7). It decodes the
// documented {items,total} envelope; a bare JSON array is taken as the
// whole list. paged=false sends neither limit nor offset (disk-types has
// no such parameters).
func listAll[T any](a *CloudRuAdapter, ctx context.Context, path string, q url.Values, paged bool) ([]*T, error) {
	return listPages[T](a, ctx, path, q, paged, listCompatible)
}

var errMissingListPage = errors.New("a promised list page was not found")

func invalidListPage(path string) error {
	return clierrors.New(clierrors.CodeAPIError, i18n.Tf("incomplete or invalid list response for %s", "неполный или некорректный список %s", path), hintUnexpectedFormatVerbose())
}

// A caller that requires completeness needs whole HTTP 200 pages with
// consistent totals and no overlap. It validates the returned rows itself.
type listShape int

const (
	listCompatible listShape = iota
	listComplete
	listCompleteEnvelope
)

func listPages[T any](a *CloudRuAdapter, ctx context.Context, path string, q url.Values, paged bool, shape listShape) ([]*T, error) {
	complete := shape != listCompatible
	var out []*T
	seen := map[string]bool{}
	offset := 0
	expectedTotal := -1
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
			// A service 404 after a page that promised more rows leaves the
			// list incomplete. A gateway failure already carries API_ERROR.
			if complete && offset > 0 && clierrors.Reason(err) == clierrors.CodeNotFound {
				return nil, errMissingListPage
			}
			return nil, err
		}
		body, err := readListPage(resp, path, complete)
		if err != nil {
			return nil, err
		}
		var page paginatedResponse[T]
		if err := json.Unmarshal(body, &page); err == nil && page.Items != nil {
			// The ids of the page, to drop items already read: a server
			// that ignores offset answers the first page again and again
			// (QA-9: 300 items, 100 distinct).
			var ids struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
				Total  *int `json:"total"`
				Offset *int `json:"offset"`
			}
			idsErr := json.Unmarshal(body, &ids)
			if complete {
				if idsErr != nil || ids.Total == nil || page.Total < offset+len(page.Items) ||
					(expectedTotal >= 0 && expectedTotal != page.Total) ||
					(ids.Offset != nil && *ids.Offset != offset) {
					return nil, invalidListPage(path)
				}
				expectedTotal = page.Total
			}
			fresh := 0
			for i := range page.Items {
				id := ""
				if i < len(ids.Items) {
					id = ids.Items[i].ID
				}
				if id != "" {
					if seen[id] {
						continue
					}
					seen[id] = true
				}
				fresh++
				out = append(out, &page.Items[i])
			}
			if complete && fresh != len(page.Items) {
				return nil, invalidListPage(path)
			}
			offset += len(page.Items)
			if !paged {
				return out, nil
			}
			done, err := offsetPageEnd(len(page.Items), fresh, len(out), offset, page.Total, listPageSize)
			if err != nil {
				return nil, pager.Named(path, err)
			}
			if done {
				return out, nil
			}
			continue
		}
		var arr []T
		if err := json.Unmarshal(body, &arr); err == nil {
			if shape == listCompleteEnvelope || complete && (arr == nil || offset != 0) {
				return nil, invalidListPage(path)
			}
			for i := range arr {
				out = append(out, &arr[i])
			}
			return out, nil
		}
		return nil, clierrors.New(clierrors.CodeAPIError, i18n.Tf("failed to decode list response for %s", "не удалось разобрать список %s", path), hintUnexpectedFormatVerbose())
	}
	// Past listMaxPages the list is cut short: an error, as pager.All
	// gives, not a partial list without a word (QA-9).
	return nil, pager.Named(path, pager.TooManyPages(listMaxPages))
}

func readListPage(resp *http.Response, path string, complete bool) ([]byte, error) {
	defer resp.Body.Close()
	limit := int64(maxListBody)
	if complete {
		limit++ // distinguish an exactly full response from one cut off at the limit
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		if complete && errors.Is(err, context.Canceled) {
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, i18n.T("request cancelled", "запрос отменён"), hintCancelled(), err)
		}
		var timeout net.Error
		if complete && errors.As(err, &timeout) && timeout.Timeout() {
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, i18n.Tf("timed out reading %s", "истекло время ожидания ответа %s", path),
				i18n.T("Check your network connection and try again", "Проверьте подключение к сети и повторите"), err)
		}
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.Tf("failed to read response for %s", "не удалось прочитать ответ %s", path), hintUnexpectedFormat(), err)
	}

	if complete && (resp.StatusCode != http.StatusOK || len(body) > maxListBody) {
		return nil, invalidListPage(path)
	}
	return body, nil
}

// offsetPageEnd decides after a page of an offset walk whether the list is
// read: items is the size of the page, fresh how many of them were not read
// before, read how many the list has now, offset the next offset, total the
// answer's total (0: not given). A list read only in part is an error, not
// a shorter list: evo deletes what is left over and counts from it
// (break r4, R4-2…R4-5).
//
// With a total: a page of items already read is an error (the server
// ignores the offset, QA-9: 300 items, 100 distinct) — checked before the
// offset reaches the total, or such a server with total=2*pageSize would
// give half the list as the whole one (REL-2); offset at the total ends
// the list; an empty page before it is an error. Without one: an empty
// page or a page shorter than pageSize ends it, a page of items already
// read is an error.
func offsetPageEnd(items, fresh, read, offset, total, pageSize int) (bool, error) {
	if total > 0 {
		switch {
		case items > 0 && fresh == 0:
			return false, pager.NoNewItems(read, total)
		case offset >= total:
			return true, nil
		case items == 0:
			return false, pager.EmptyPage(read, total)
		}
		return false, nil
	}
	switch {
	case items == 0:
		return true, nil
	case fresh == 0:
		return false, pager.NoNewItems(read, -1)
	}
	return items < pageSize, nil
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

type cleanupRuleReadKey struct{}

// ReadSecurityGroupRulesForCleanup requires a complete listing before using
// the absence of a rule as proof. Forwarding the context keeps the same
// contract through CloudAPI decorators.
// projectID is set only when the caller has established the group's project.
// Without it a 404 cannot confirm that the group and its rules are gone.
func ReadSecurityGroupRulesForCleanup(ctx context.Context, cloud CloudAPI, sgID, projectID string) ([]*SGRule, error) {
	return cloud.ListSecurityGroupRules(context.WithValue(ctx, cleanupRuleReadKey{}, projectID), sgID)
}

// ListSecurityGroupRules lists a group's rules via
// GET /api/v1/security-groups/{id}/rules, following offset pages. The group
// list (GET /api/v1/security-groups) carries only rule counts. The walk
// ends as listAll's does (offsetPageEnd); past sgRulesMaxPages, on an empty
// page before the total or on a page of rules already read the list is not
// complete — an error: deploy and destroy decide from it which rule to add
// or delete (break r4, R4-4, R4-5).
func (a *CloudRuAdapter) ListSecurityGroupRules(ctx context.Context, sgID string) ([]*SGRule, error) {
	projectID, strict := ctx.Value(cleanupRuleReadKey{}).(string)
	expectedTotal := -1
	var out []*SGRule
	seen := map[string]bool{}
	offset := 0
	base := fmt.Sprintf("/api/v1/security-groups/%s/rules", seg(sgID))
	for range sgRulesMaxPages {
		path := fmt.Sprintf("%s?offset=%d&limit=%d", base, offset, sgRulesPageSize)
		resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
		if err != nil {
			// A missing later page contradicts a previously visible group.
			// A first-page service 404 can also mean the group is hidden.
			if offset == 0 && projectID != "" && errors.Is(err, errRuleGroupMissing) {
				absent, checkErr := a.groupAbsentInProject(ctx, projectID, sgID)
				if checkErr != nil {
					return nil, checkErr
				}
				if absent {
					return nil, nil
				}
			}
			return nil, err
		}
		body, err := readListPage(resp, base, strict)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items  []SGRule `json:"items"`
			Total  *int     `json:"total"`
			Offset *int     `json:"offset"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode security group rules", "не удалось разобрать правила группы безопасности"), hintUnexpectedFormatVerbose(), err)
		}
		total := 0
		if page.Total != nil {
			total = *page.Total
		}
		if strict {
			if page.Items == nil || page.Total == nil || total < offset+len(page.Items) ||
				(expectedTotal >= 0 && total != expectedTotal) || (page.Offset != nil && *page.Offset != offset) {
				return nil, invalidListPage(base)
			}
			expectedTotal = total
		}
		fresh := 0
		for i := range page.Items {
			id := page.Items[i].ID
			if strict && (id == "" || strings.TrimSpace(id) != id || seen[id]) {
				return nil, invalidListPage(base)
			}
			if id != "" {
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			fresh++
			out = append(out, &page.Items[i])
		}
		offset += len(page.Items)
		done, err := offsetPageEnd(len(page.Items), fresh, len(out), offset, total, sgRulesPageSize)
		if err != nil {
			return nil, pager.Named(base, err)
		}
		if done {
			return out, nil
		}
	}
	return nil, pager.Named(base, pager.TooManyPages(sgRulesMaxPages))
}

var errRuleGroupMissing = errors.New("compute reported a missing security group")

// Compute uses both JSON objects and arrays for resource errors. Only an
// explicit not-found message can trigger the independent inventory read.
func ruleGroupMissingReply(resp *http.Response) bool {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
	if err != nil || len(body) > maxErrorBody || (resp.ContentLength > 0 && int64(len(body)) != resp.ContentLength) {
		return false
	}
	type refusal struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	}
	var entries []refusal
	if json.Unmarshal(body, &entries) != nil {
		var entry refusal
		if json.Unmarshal(body, &entry) != nil {
			return false
		}
		entries = []refusal{entry}
	}
	if len(entries) != 1 {
		return false
	}
	if entries[0].Code != nil {
		var code string
		if json.Unmarshal(entries[0].Code, &code) != nil || code != "security_group_not_found" {
			return false
		}
	}
	switch strings.ToLower(strings.TrimSpace(entries[0].Message)) {
	case "security group not found", "security group with this id is not found":
		return true
	}
	return false
}

// groupAbsentInProject requires an unfiltered, complete list of the recorded
// project, distinct IDs, and a matching project on every row that supplies it.
func (a *CloudRuAdapter) groupAbsentInProject(ctx context.Context, projectID, sgID string) (bool, error) {
	const path = "/api/v1/security-groups"
	type identity struct {
		ID        string          `json:"id"`
		ProjectID json.RawMessage `json:"project_id"`
	}
	groups, err := listPages[identity](a, ctx, path, url.Values{"project_id": {projectID}}, true, listCompleteEnvelope)
	if errors.Is(err, errMissingListPage) {
		return false, invalidListPage(path)
	}
	if err != nil {
		return false, err
	}
	found := false
	for _, group := range groups {
		if group == nil || group.ID == "" || strings.TrimSpace(group.ID) != group.ID {
			return false, invalidListPage(path)
		}
		if group.ProjectID != nil {
			var owner string
			if json.Unmarshal(group.ProjectID, &owner) != nil || owner != projectID {
				return false, invalidListPage(path)
			}
		}
		if group.ID == sgID {
			found = true
		}
	}
	return !found, nil
}

// DeleteSecurityGroupRule deletes one rule via
// DELETE /api/v1/security-groups/{id}/rules/{rule_id} (204; 404 when the
// rule is gone, which the adapter reports as CodeNotFound).
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
