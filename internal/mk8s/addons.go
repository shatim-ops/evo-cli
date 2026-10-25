// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// Addons (plugins) of Managed Kubernetes: the shared catalogue, the releases
// of one addon, and installing a release into a cluster.
//
// The catalogue shapes (GET /v3/addons, GET /v3/addons/{name}) were read on
// the live API (api-probe #5, 16.10.2026, docs/internal/standalone/BACKLOG.md
// [k8s-addons]). The cluster side (POST and GET /v3/clusterAddons) comes from
// the v3 spec only: the test project has no cluster, so it is not
// live-verified yet.

// addonPageSize is the page size of the catalogue calls; maxAddonPages stops
// a server that keeps returning a next page token.
const (
	addonPageSize = 100
	maxAddonPages = 50
)

// Addon is one addon of the shared catalogue.
type Addon struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// Categories are Russian words of the console ("Сети", "Мониторинг").
	Categories []string `json:"categories,omitempty"`
	// IsBillable is the catalogue's flag; the server-side filter on it does
	// not match it (api-probe #5), so callers filter themselves.
	IsBillable bool   `json:"isBillable"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// AddonRelease is one installable release of an addon.
type AddonRelease struct {
	AddonVersion   string `json:"addonVersion"`
	AppVersion     string `json:"appVersion"`
	AddonNamespace string `json:"addonNamespace,omitempty"`
	// ConfigurationSchema is a JSON Schema as a string (up to ~94 KB).
	ConfigurationSchema string            `json:"configurationSchema,omitempty"`
	Mode                string            `json:"mode,omitempty"`
	Requirements        AddonRequirements `json:"requirements"`
	CreatedAt           string            `json:"createdAt,omitempty"`
	UpdatedAt           string            `json:"updatedAt,omitempty"`
}

// AddonRequirements is what a release needs of the cluster.
type AddonRequirements struct {
	// KubeVersions is a semver range, e.g. ">=1.26.0-0".
	KubeVersions string  `json:"kubeVersions,omitempty"`
	ReadyWorkers flexInt `json:"readyWorkers,omitempty"`
}

// flexInt reads a JSON number or a quoted one: protojson writes int64 as a
// string, and the spec does not say which width readyWorkers has.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("readyWorkers: %q is not an integer", s)
	}
	*n = flexInt(v)
	return nil
}

// projectQuery is the query of the catalogue calls: they answer 400 without
// projectId (the camelCase name, as the probe sent it).
func (c *Client) projectQuery() url.Values {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("projectId", c.projectID)
	}
	return q
}

// ListAddons returns the whole addon catalogue, following nextPageToken.
func (c *Client) ListAddons(ctx context.Context) ([]Addon, error) {
	var out []Addon
	err := c.pages(ctx, "/v3/addons", nil, func(data []byte) (string, error) {
		var r struct {
			Addons        []Addon `json:"addons"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf(i18n.T("parse addons: %w", "разбор плагинов: %w"), err)
		}
		out = append(out, r.Addons...)
		return r.NextPageToken, nil
	})
	return out, err
}

// GetAddonReleases returns the releases of one addon, in the server's order
// (which is not defined: sort with SortReleases).
func (c *Client) GetAddonReleases(ctx context.Context, name string) ([]AddonRelease, error) {
	var out []AddonRelease
	err := c.pages(ctx, "/v3/addons/"+seg(name), nil, func(data []byte) (string, error) {
		var r struct {
			Releases      []AddonRelease `json:"releases"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf(i18n.T("parse addon releases: %w", "разбор релизов плагина: %w"), err)
		}
		for _, rel := range r.Releases {
			// The live API (16.10.2026) puts the addon version into
			// addonNamespace too ("1.21.1-sbc.0" for cert-manager): a value
			// that is not a namespace name (DNS-1123 label) is left out —
			// an agent would run "get pods -n 1.21.1-sbc.0" literally.
			// A value equal to a version is left out too ("1" passes the
			// pattern).
			// Versions compare without a leading "v" ("1" and "v1"), as in
			// parseSemver.
			ns := strings.TrimPrefix(rel.AddonNamespace, "v")
			if !namespaceRE.MatchString(rel.AddonNamespace) ||
				ns == strings.TrimPrefix(rel.AddonVersion, "v") || ns == strings.TrimPrefix(rel.AppVersion, "v") {
				rel.AddonNamespace = ""
			}
			out = append(out, rel)
		}
		return r.NextPageToken, nil
	})
	return out, err
}

// pages GETs path page by page; page decodes one body and returns the next
// page token ("" ends). A token already followed and more than
// maxAddonPages pages are errors, not the end of the list: the list is not
// complete (break r4, R4-6; both ended the list without a word before).
func (c *Client) pages(ctx context.Context, path string, extra url.Values, page func([]byte) (string, error)) error {
	return c.walk(ctx, path, func() url.Values {
		q := c.projectQuery()
		for k, v := range extra {
			q[k] = v
		}
		return q
	}, page)
}

// walk is pages with the query of each request from query (pageSize and
// pageToken are added).
func (c *Client) walk(ctx context.Context, path string, query func() url.Values, page func([]byte) (string, error)) error {
	more, err := pager.Upto(maxAddonPages, func(token string) (string, error) {
		q := query()
		q.Set("pageSize", strconv.Itoa(addonPageSize))
		if token != "" {
			q.Set("pageToken", token)
		}
		data, st, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return "", err
		}
		if st != 200 {
			return "", apiError(st, data)
		}
		return page(data)
	})
	if err != nil {
		return pager.Named("mk8s: "+path, err)
	}
	if more {
		return pager.Named("mk8s: "+path, pager.TooManyPages(maxAddonPages))
	}
	return nil
}

// Resolve modes of CreateClusterAddonReq: what the installation does with
// objects of the addon already in the cluster.
// The spec says PRESERVE does not apply to adding an addon, so only
// these two are accepted when installing.
var ResolveModes = []string{"NONE", "OVERWRITE"}

// DefaultResolveMode is sent when none is given: resolveMode is required
// (live API 16.10.2026: 400 "resolveMode: must be specified"), and NONE
// leaves manifests the user installed by hand as they are.
const DefaultResolveMode = "RESOLVE_CONFLICTS_MODE_NONE"

// CreateClusterAddonReq is the body of POST /v3/clusterAddons (v3 spec, not
// live-verified).
type CreateClusterAddonReq struct {
	ClusterID  string `json:"clusterId"`
	Name       string `json:"name"`
	AppVersion string `json:"appVersion"`
	// AddonConfiguration is a JSON document as a string; the server
	// substitutes {{clusterID}} and {{clusterName}} in it.
	AddonConfiguration string `json:"addonConfiguration,omitempty"`
	ResolveMode        string `json:"resolveMode"`
}

// ClusterAddonResult is the answer of CreateClusterAddon. Other mk8s
// mutations return an Operation; the spec names a ClusterAddon. Both are
// read: OperationID is set for an operation, ID and Status for an addon.
type ClusterAddonResult struct {
	OperationID string
	ID          string // the cluster addon (an operation's resourceId)
	Status      string
	Done        bool
}

// CreateClusterAddon installs an addon release into a cluster.
func (c *Client) CreateClusterAddon(ctx context.Context, req CreateClusterAddonReq) (*ClusterAddonResult, error) {
	if req.ResolveMode == "" {
		req.ResolveMode = DefaultResolveMode
	}
	data, st, err := c.do(ctx, http.MethodPost, "/v3/clusterAddons", c.projectQuery(), req)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var r struct {
		ID         string          `json:"id"`
		ResourceID string          `json:"resourceId"`
		Done       *bool           `json:"done"`
		Status     string          `json:"status"`
		Error      json.RawMessage `json:"error"`
		ClusterID  string          `json:"clusterId"`
		AppVersion string          `json:"appVersion"`
		Name       string          `json:"name"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf(i18n.T("parse cluster addon: %w", "разбор плагина кластера: %w"), err)
	}
	// The spec answers an Operation: with done or resourceId (the live
	// answer, 16.10.2026), and also bare — protojson leaves out done:false
	// and resourceId may be empty until the operation ends, so {"id":"op-1"}
	// is an operation too. Only an answer without these and with the fields
	// of a ClusterAddon (status, clusterId, appVersion, name) is the addon
	// itself.
	if r.ID == "" && r.ResourceID == "" {
		// Nothing to follow: not "installing" without a single identifier.
		// The POST answered 2xx, so the install may have started: say so
		// before a retry, and keep the API's own error text.
		msg := "unexpected answer of POST /v3/clusterAddons: no operation or addon id"
		if len(r.Error) > 0 && string(r.Error) != "null" {
			msg += ": " + string(r.Error)
		}
		return nil, fmt.Errorf("%s; the install may have started — check the addons of the cluster in the Cloud.ru console before retrying", msg)
	}
	if r.Done != nil || r.ResourceID != "" || (r.Status == "" && r.ClusterID == "" && r.AppVersion == "" && r.Name == "") {
		res := &ClusterAddonResult{OperationID: r.ID, ID: r.ResourceID, Done: r.Done != nil && *r.Done}
		if len(r.Error) > 0 && string(r.Error) != "null" {
			return res, fmt.Errorf("mk8s operation %s failed: %s", r.ID, r.Error)
		}
		return res, nil
	}
	return &ClusterAddonResult{ID: r.ID, Status: r.Status}, nil
}

// ListClusterAddons lists the addons installed in a cluster. Not
// live-verified: GET /v3/clusterAddons?projectId= alone answers 400, and
// clusterId (the filter of /v3/nodePools) is the most plausible filter.
// Callers treat an error as "unknown", never as "none installed".
func (c *Client) ListClusterAddons(ctx context.Context, clusterID string) ([]map[string]any, error) {
	var out []map[string]any
	err := c.pages(ctx, "/v3/clusterAddons", url.Values{"clusterId": {clusterID}}, func(data []byte) (string, error) {
		var r struct {
			ClusterAddons []map[string]any `json:"clusterAddons"`
			Items         []map[string]any `json:"items"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf(i18n.T("parse cluster addons: %w", "разбор плагинов кластера: %w"), err)
		}
		out = append(out, r.ClusterAddons...)
		out = append(out, r.Items...)
		return r.NextPageToken, nil
	})
	return out, err
}

// namespaceRE is a Kubernetes namespace name (a DNS-1123 label).
var namespaceRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
