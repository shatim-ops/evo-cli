// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package mk8s is a small client for the Cloud.ru Managed Kubernetes (Evolution)
// v3 API (https://mk8s.api.cloud.ru). It is a gRPC-gateway REST API: project_id
// goes in the query string, create/delete return an async Operation to poll.
//
// The request/response shapes were confirmed against the live API + the v3
// OpenAPI spec.
package mk8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://mk8s.api.cloud.ru"

// Client talks to the mk8s v3 API with a bearer token.
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

// New builds a client. baseURL empty → default.
func New(token, projectID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		http:      httpretry.NewClient(60 * time.Second),
		base:      baseURL,
		token:     token,
		projectID: projectID,
	}
}

// do performs an HTTP request and returns the raw body + status code.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, int, error) {
	if query == nil {
		query = url.Values{}
	}
	// The addon calls send projectId (camelCase); the others get project_id.
	if query.Get("project_id") == "" && query.Get("projectId") == "" && c.projectID != "" {
		query.Set("project_id", c.projectID)
	}
	u := c.base + path
	if enc := query.Encode(); enc != "" {
		u += "?" + enc
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf(i18n.T("mk8s request failed: %w", "запрос к mk8s не удался: %w"), err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// apiError extracts a readable message from a gRPC-gateway error body.
func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
		Details []struct {
			FieldViolations []struct {
				Field       string `json:"field"`
				Description string `json:"description"`
			} `json:"field_violations"`
		} `json:"details"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = string(body)
	}
	var viol []string
	for _, d := range e.Details {
		for _, v := range d.FieldViolations {
			viol = append(viol, v.Field+": "+v.Description)
		}
	}
	if len(viol) > 0 {
		return fmt.Errorf("mk8s API %d: %s (%v)", status, msg, viol)
	}
	return fmt.Errorf("mk8s API %d: %s", status, msg)
}

// --- Types ---

// Cluster is a summary of a cluster from the list/get endpoints.
type Cluster struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
}

// Operation is an async operation handle.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resourceId,omitempty"`
	Done       bool            `json:"done"`
	Error      json.RawMessage `json:"error,omitempty"`
}

// CreateClusterReq mirrors the confirmed CreateClusterRequest.
type CreateClusterReq struct {
	Name                      string                    `json:"name"`
	ProjectID                 string                    `json:"projectId"`
	ControlPlaneConfiguration ControlPlaneConfiguration `json:"controlPlaneConfiguration"`
	NetworkConfiguration      NetworkConfiguration      `json:"networkConfiguration"`
	IdentityConfiguration     IdentityConfiguration     `json:"identityConfiguration"`
	ReleaseChannel            string                    `json:"releaseChannel,omitempty"`
}

type ControlPlaneConfiguration struct {
	Version             string              `json:"version"`
	SizingConfiguration SizingConfiguration `json:"sizingConfiguration"`
	Zones               []string            `json:"zones,omitempty"`
}

type SizingConfiguration struct {
	MasterCount int    `json:"masterCount"`
	FlavorID    string `json:"flavorId"`
}

type NetworkConfiguration struct {
	PrivateVipSubnetID string `json:"privateVipSubnetId"`
	KubeApiInternet    bool   `json:"kubeApiInternet,omitempty"`
	ServicesSubnetCidr string `json:"servicesSubnetCidr,omitempty"`
	PodsSubnetCidr     string `json:"podsSubnetCidr,omitempty"`
}

type IdentityConfiguration struct {
	ClusterSaID string `json:"clusterSaId"`
}

// ProductConfig is the reference data for planning.
type ProductConfig struct {
	DefaultZone     string `json:"defaultZone"`
	DefaultChannel  string `json:"defaultChannel"`
	DefaultDiskType string `json:"defaultDiskType"`
	Zones           []struct {
		ZoneID           string   `json:"zoneId"`
		Name             string   `json:"name"`
		AvailableFlavors []string `json:"availableFlavors"`
	} `json:"zones"`
	Flavors  []Flavor `json:"flavors"`
	Channels []struct {
		Channel           string   `json:"channel"`
		AvailableVersions []string `json:"availableVersions"`
	} `json:"channels"`
}

// Flavor is a configuration of the catalogue for a master or a node.
type Flavor struct {
	FlavorID string `json:"flavorId"`
	Name     string `json:"name"`
	CPU      int    `json:"cpu"`
	RAM      int    `json:"ram"`
	GPU      int    `json:"gpu"`
	// Purpose is what the flavor is for: CONTROLPLANE (masters) or
	// DATAPLANE (nodes) (FlavorConfiguration.purpose); "" when the
	// catalogue does not say.
	Purpose string `json:"purpose,omitempty"`
}

// --- Operations ---

// ListClusters returns all clusters in the project, every page
// (openapi-v3.yaml of mk8s: nextPageToken in the answer, pageToken in the
// next request).
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var all []Cluster
	err := pager.All("mk8s: clusters", func(token string) (string, error) {
		var q url.Values
		if token != "" {
			q = url.Values{"pageToken": {token}}
		}
		data, st, err := c.do(ctx, http.MethodGet, "/v3/clusters", q, nil)
		if err != nil {
			return "", err
		}
		if st != 200 {
			return "", apiError(st, data)
		}
		var r struct {
			Clusters      []Cluster `json:"clusters"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf(i18n.T("parse clusters: %w", "разбор кластеров: %w"), err)
		}
		all = append(all, r.Clusters...)
		return r.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetCluster returns the raw cluster object by id.
func (c *Client) GetCluster(ctx context.Context, id string) (map[string]any, error) {
	data, st, err := c.do(ctx, http.MethodGet, "/v3/clusters/"+seg(id), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf(i18n.T("parse cluster: %w", "разбор кластера: %w"), err)
	}
	return m, nil
}

// CreateCluster submits a create and returns the async operation.
func (c *Client) CreateCluster(ctx context.Context, req CreateClusterReq) (*Operation, error) {
	if req.ProjectID == "" {
		req.ProjectID = c.projectID
	}
	data, st, err := c.do(ctx, http.MethodPost, "/v3/clusters", nil, req)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}

// DeleteCluster deletes a cluster by id and returns the async operation.
func (c *Client) DeleteCluster(ctx context.Context, id string) (*Operation, error) {
	data, st, err := c.do(ctx, http.MethodDelete, "/v3/clusters/"+seg(id), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	_ = json.Unmarshal(data, &op)
	return &op, nil
}

// GetKubeConfig fetches the kubeconfig (base64) for a cluster. network is
// "NETWORK_TYPE_PUBLIC" or "NETWORK_TYPE_PRIVATE" (empty → default).
func (c *Client) GetKubeConfig(ctx context.Context, id, network string) (string, error) {
	body := map[string]any{}
	if network != "" {
		body["network"] = network
	}
	data, st, err := c.do(ctx, http.MethodPost, "/v3/clusters/"+seg(id)+":getKubeConfig", nil, body)
	if err != nil {
		return "", err
	}
	if st != 200 {
		return "", apiError(st, data)
	}
	var r struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf(i18n.T("parse kubeconfig: %w", "разбор kubeconfig: %w"), err)
	}
	return r.Config, nil
}

// ProductConfiguration returns the reference data (versions, flavors, zones).
func (c *Client) ProductConfiguration(ctx context.Context) (*ProductConfig, error) {
	data, st, err := c.do(ctx, http.MethodGet, "/v3/productConfiguration", nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var pc ProductConfig
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf(i18n.T("parse product configuration: %w", "разбор конфигурации продукта: %w"), err)
	}
	return &pc, nil
}

// GetOperation polls an async operation by id.
func (c *Client) GetOperation(ctx context.Context, id string) (*Operation, error) {
	data, st, err := c.do(ctx, http.MethodGet, "/v3/operations/"+seg(id), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}
