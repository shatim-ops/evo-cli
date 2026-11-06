// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package iamadmin reads the Cloud.ru Administration API (IAM,
// https://iam.api.cloud.ru, iam-gw-public.yaml): the permissions of a
// subject on a resource — the roles of the key that whoami and doctor name
// (A0-1, A0-2, release plan 1.8) — and the access keys of a service
// account, for the key's term (A0-3) — and the requests of evo login (B-3):
// evaluate, the service account, its role and its key (login.go).
//
// What it returns is a whitelist: each type names the fields evo uses, and
// nothing else of an answer goes on («Риск», R-43). Answers are read
// in both spellings: the spec's camelCase and the live API's snake_case
// (register IAM-2). A list is read to its end or fails, never cut short,
// and at most MaxPages pages of it are read.
package iamadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://iam.api.cloud.ru"

// MaxPages bounds every list of the client: IAM lists of one project or
// one service account are short, and a list past the bound fails rather
// than be read for long («Риск», conclusion 22).
const MaxPages = 20

// Client reads the Administration API with the token of tp.
type Client struct {
	http  *http.Client
	base  string
	token auth.TokenProvider
}

// New builds a client. baseURL empty → https://iam.api.cloud.ru.
func New(tp auth.TokenProvider, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{http: httpretry.NewClient(30 * time.Second), base: strings.TrimRight(baseURL, "/"), token: tp}
}

// Permission is a role of a subject on a resource. RoleID is the role's
// UUID (primaryRoleId; the spec's roleId is the deprecated name).
type Permission struct {
	ID, RoleName, RoleID, ResourceID, SubjectID, SubjectType, ExpiresAt string
	Enabled                                                             bool
}

// Permissions lists the permissions on resourceID, of subjectID only when
// it is not "". The spec requires the resource.
func (c *Client) Permissions(ctx context.Context, resourceID, subjectID string) ([]Permission, error) {
	if resourceID == "" {
		return nil, badValue("resource_id", resourceID)
	}
	q := url.Values{}
	if err := setFilter(q, "resource_id", resourceID, "subject_id", subjectID); err != nil {
		return nil, err
	}
	var out []Permission
	err := c.list(ctx, "permissions", "/api/v3/permissions", q, "permissions", func(m map[string]any) {
		out = append(out, permissionOf(m))
	})
	return out, err
}

// AccessKey is an access key of a service account, without its secret:
// the answer of the list carries the secret, AccessKey has no field for it
// («Риск», R-43).
type AccessKey struct {
	ID, KeyID, ExpiredAt string
}

// AccessKeys lists the access keys of a service account.
func (c *Client) AccessKeys(ctx context.Context, serviceAccountID string) ([]AccessKey, error) {
	if !pathID.MatchString(serviceAccountID) {
		return nil, badValue("serviceAccountId", serviceAccountID)
	}
	var m map[string]any
	if err := c.do(ctx, "/api/v2/serviceAccounts/"+serviceAccountID+"/accessKeys", nil, &m); err != nil {
		return nil, err
	}
	var out []AccessKey
	for _, x := range items(m, "accessKeys") {
		out = append(out, AccessKey{ID: str(x, "id"), KeyID: str(x, "keyId"), ExpiredAt: str(x, "expiredAt")})
	}
	return out, nil
}

// filterValue is what a value of a filter may be: an id, a name, a
// product code. A space, a quote or a line break would let the value
// widen the expression (the filter is AND of key=value), so it is refused
// before the request.
var filterValue = regexp.MustCompile(`^[A-Za-z0-9._*:-]+$`)

// pathID is what an id in a path may be: a UUID's letters, digits and
// dashes, never «.» or «/», which would lead the request elsewhere.
var pathID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// setFilter sets q's filter to the AND of the key=value pairs of kv whose
// value is not "".
func setFilter(q url.Values, kv ...string) error {
	var parts []string
	for i := 0; i < len(kv); i += 2 {
		k, v := kv[i], kv[i+1]
		if v == "" {
			continue
		}
		if !filterValue.MatchString(v) {
			return badValue(k, v)
		}
		parts = append(parts, k+"="+v)
	}
	if len(parts) > 0 {
		q.Set("filter", strings.Join(parts, " AND "))
	}
	return nil
}

func badValue(key, v string) error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("iam: %s %q is not an id or a name; nothing was sent", "iam: %s %q — не идентификатор и не имя; ничего не отправлено", key, v),
		i18n.T("Pass an id or a name: letters, digits, '.', '_', '-', ':', '*'.",
			"Передайте идентификатор или имя: буквы, цифры, «.», «_», «-», «:», «*»."))
}

// list reads every page of a list, at most MaxPages of them, handing each
// item to add.
func (c *Client) list(ctx context.Context, what, path string, q url.Values, key string, add func(map[string]any)) error {
	more, err := pager.Upto(MaxPages, func(tok string) (string, error) {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		if tok != "" {
			pq.Set("pageToken", tok)
		}
		var m map[string]any
		if err := c.do(ctx, path, pq, &m); err != nil {
			return "", err
		}
		for _, x := range items(m, key) {
			add(x)
		}
		return str(m, "nextPageToken"), nil
	})
	if err == nil && more {
		err = pager.TooManyPages(MaxPages)
	}
	if err != nil {
		return pager.Named("iam: "+what, err)
	}
	return nil
}

// do sends one GET with the token and decodes a 2xx answer into out.
func (c *Client) do(ctx context.Context, path string, q url.Values, out any) error {
	return c.send(ctx, http.MethodGet, path, q, nil, out)
}

// send sends one request with the token — body as JSON when it is not nil
// — and decodes a 2xx answer into out when it is not nil; any other status
// is an error of the API (StatusOf names its status) that never carries the
// token.
func (c *Client) send(ctx context.Context, method, path string, q url.Values, body, out any) error {
	tok, err := c.token.Token(ctx)
	if err != nil {
		return err
	}
	u := c.base + path
	if len(q) > 0 {
		// A space goes as %20, not «+»: IAM reads «+» in the query as
		// itself, and a filter of two parts «a=1 AND b=2» was refused
		// (audit of 06.11, B-5, 2 of 2). A «+» of a value is %2B already.
		u += "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf(i18n.T("iam request failed: %w", "запрос к iam не удался: %w"), err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(resp.StatusCode, data)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("iam: the answer to %s %s is not JSON", "iam: ответ на %s %s — не JSON", method, path), "")
	}
	return nil
}

// StatusError is a non-2xx answer of the API: its status and message. A
// 403 or 429 comes wrapped in their CLIError (errors.As finds it).
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string { return fmt.Sprintf("iam API %d: %s", e.Status, e.Msg) }

// StatusOf is the status of err's answer, 0 when err is no answer of the API.
func StatusOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(status)
	}
	se := &StatusError{Status: status, Msg: msg}
	if status == http.StatusTooManyRequests {
		return clierrors.Wrap(clierrors.CodeAPIRateLimited,
			i18n.Tf("iam API %d: %s", "iam API %d: %s", status, msg), i18n.T("Retry in a minute.", "Повторите через минуту."), se)
	}
	if status == http.StatusForbidden {
		return clierrors.Wrap(clierrors.CodeAPIError, i18n.Tf("iam API %d: %s", "iam API %d: %s", status, msg),
			i18n.T("The role does not let it do this in IAM.", "Роль не даёт сделать это в IAM."), se)
	}
	// Any other status: «iam API <status>: …», whose reason the status, or
	// a «not found» of a 400, 422 or 499, decides where every service
	// client's does (TestStatusBeforeText_*).
	return se
}

// field is m's value under key in the spec's camelCase or, failing that,
// the live API's snake_case (IAM-2).
func field(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return m[snake(key)]
}

func snake(k string) string {
	var b strings.Builder
	for _, r := range k {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func str(m map[string]any, key string) string {
	s, _ := field(m, key).(string)
	return s
}

func flag(m map[string]any, key string) bool {
	b, _ := field(m, key).(bool)
	return b
}

func items(m map[string]any, key string) []map[string]any {
	xs, _ := field(m, key).([]any)
	out := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}
