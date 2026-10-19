// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package certmanager is a client for the Cloud.ru Certificate Manager API
// (https://certificatemanager.api.cloud.ru), built from the official swagger
// (ccm.swagger.yaml). IAM Bearer auth, JSON. Project is a query param on List /
// in the body on Import & IssueLENC. NB: resources live under the SINGULAR path
// /v1/certificate (the plural /v1/certificates 404s — the gateway answers a
// missing route with a bare 415, which is what made this API look opaque).
// Custom (gRPC) verbs use a ":verb" suffix, e.g. /v1/certificate:issue_lenc_cert
// and /v1/certificate/{id}:renew — the ":" must stay literal (not %3A).
package certmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://certificatemanager.api.cloud.ru"

// Client talks to the Certificate Manager API.
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

// New builds a client. baseURL empty → defaultBaseURL.
func New(token, projectID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{http: httpretry.NewClient(60 * time.Second), base: strings.TrimRight(baseURL, "/"), token: token, projectID: projectID}
}

// ProjectID exposes the configured project.
func (c *Client) ProjectID() string { return c.projectID }

// do issues a request. path is appended verbatim to the base (already escaped by
// the caller for id segments); q are query params; body (if non-nil) is JSON.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any) ([]byte, int, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("certmanager request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = e.Error
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("certmanager API %d: %s", status, msg)
}

func (c *Client) decode(data []byte, status int, out any) error {
	if status < 200 || status >= 300 {
		return apiError(status, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse certmanager response: %w", err)
	}
	return nil
}

// certPath escapes an id segment while keeping the /v1/certificate prefix.
func certPath(id string) string { return "/v1/certificate/" + url.PathEscape(id) }

// ListOptions filter a certificate list. All fields optional except the client's
// projectId (added automatically).
type ListOptions struct {
	Name       string
	Type       string // CERTIFICATE_TYPE_{IMPORTED,SELF_SIGNED,LETS_ENCRYPT}
	DNSName    string
	CommonName string
	State      string // CERTIFICATE_STATE_{ACTIVE,EXPIRED}
}

// List returns certificates matching the criteria (no versions embedded).
func (c *Client) List(ctx context.Context, o ListOptions) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("projectId", c.projectID)
	if o.Name != "" {
		q.Set("name", o.Name)
	}
	if o.Type != "" {
		q.Set("type", o.Type)
	}
	if o.DNSName != "" {
		q.Set("dnsName", o.DNSName)
	}
	if o.CommonName != "" {
		q.Set("commonName", o.CommonName)
	}
	if o.State != "" {
		q.Set("state", o.State)
	}
	data, status, err := c.do(ctx, http.MethodGet, "/v1/certificate", q, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Certificates []map[string]any `json:"certificates"`
	}
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out.Certificates, nil
}

// Get returns one certificate by id.
func (c *Client) Get(ctx context.Context, id string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, certPath(id), nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// IssueLetsEncrypt starts issuing a Let's Encrypt certificate for dnsNames.
// Returns the Operation (async — poll GetOperation until done). The domains must
// have delegated zones in Evolution DNS (else ERROR_CODE_NOT_DELEGATED).
func (c *Client) IssueLetsEncrypt(ctx context.Context, name, description string, dnsNames []string) (map[string]any, error) {
	body := map[string]any{"projectId": c.projectID, "name": name, "dnsNames": dnsNames}
	if description != "" {
		body["description"] = description
	}
	data, status, err := c.do(ctx, http.MethodPost, "/v1/certificate:issue_lenc_cert", nil, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Import creates a user-managed imported certificate. serverCertificate and
// privateKey are Base64-encoded PEM strings (intermediate optional).
func (c *Client) Import(ctx context.Context, body map[string]any) (map[string]any, error) {
	body["projectId"] = c.projectID
	data, status, err := c.do(ctx, http.MethodPost, "/v1/certificate", nil, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Renew triggers Let's Encrypt renewal for a certificate. Returns an Operation.
func (c *Client) Renew(ctx context.Context, id string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodPost, certPath(id)+":renew", nil, map[string]any{})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes a certificate. force deletes even if in use by other services.
func (c *Client) Delete(ctx context.Context, id string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	data, status, err := c.do(ctx, http.MethodDelete, certPath(id), q, nil)
	if err != nil {
		return err
	}
	return c.decode(data, status, nil)
}

// Usage returns the resources (LBs, etc.) that use a certificate.
func (c *Client) Usage(ctx context.Context, id string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, certPath(id)+":usage", nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// versionsPageSize is the pageSize of a versions request. The parameter is
// optional in the swagger, but the API refuses a request without it: 400
// "PageSize: value must be inside range [1, 1000]" — cert_versions failed on
// every certificate (release audit, L-17). 1000 is the upper bound.
const versionsPageSize = "1000"

// maxVersionPages stops a pagination that never ends (a token repeated by
// the server): 100 pages of 1000 versions is far beyond any certificate.
const maxVersionPages = 100

// Versions lists a certificate's versions, every page (nextPageToken), as
// {"versions": [...]}.
func (c *Client) Versions(ctx context.Context, id string) (map[string]any, error) {
	all := []any{}
	// Every token seen stops the read, not only the last one: A→B→A made
	// maxVersionPages requests (QA 20.10, QA-2).
	more, err := pager.Upto(maxVersionPages, func(token string) (string, error) {
		q := url.Values{}
		q.Set("pageSize", versionsPageSize)
		if token != "" {
			q.Set("pageToken", token)
		}
		data, status, err := c.do(ctx, http.MethodGet, certPath(id)+"/versions", q, nil)
		if err != nil {
			return "", err
		}
		var out struct {
			Versions      []any  `json:"versions"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.decode(data, status, &out); err != nil {
			return "", err
		}
		all = append(all, out.Versions...)
		return out.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	if more {
		return nil, fmt.Errorf("certmanager: versions of %s: more than %d pages", id, maxVersionPages)
	}
	return map[string]any{"versions": all}, nil
}

// PrivateKey returns the private key of a version (0 = latest): privateKey
// is the PEM Base64-encoded, as the API sends it.
func (c *Client) PrivateKey(ctx context.Context, id string, versionID int64) (map[string]any, error) {
	path := certPath(id) + "/version/" + fmt.Sprintf("%d", versionID) + ":private_key"
	data, status, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOperation returns an async Operation's status (poll until done=true).
func (c *Client) GetOperation(ctx context.Context, operationID string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/operation/"+url.PathEscape(operationID), nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.decode(data, status, &out); err != nil {
		return nil, err
	}
	return out, nil
}
