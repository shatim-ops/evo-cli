// Package main is a bounded read-only observation of the Compute image catalogue.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

const (
	iamURL    = "https://iam.api.cloud.ru/api/v1/auth/token"
	imagesURL = "https://compute.api.cloud.ru/api/v1/images"
	maxBody   = 8 << 20
)

type reply struct {
	status int
	data   []byte
	fault  string
}

// A private client opens one connection per request. No redirects, retries,
// proxies, implicit pagination, token renewal, or cloud SDK discovery.
func exchange(client *http.Client, req *http.Request) reply {
	res, err := client.Do(req)
	if err != nil {
		return reply{fault: "transport_error"}
	}
	defer res.Body.Close()
	out := reply{status: res.StatusCode}
	if res.Header.Get("Content-Range") != "" || res.StatusCode == http.StatusPartialContent {
		out.fault = "partial_http"
		return out
	}
	out.data, err = io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil || len(out.data) > maxBody || (res.ContentLength >= 0 && int64(len(out.data)) != res.ContentLength) {
		out.data, out.fault = nil, "incomplete_body"
	}
	return out
}

func login(client *http.Client, key, secret string) string {
	body, _ := json.Marshal(map[string]string{"keyId": key, "secret": secret})
	req, _ := http.NewRequest(http.MethodPost, iamURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := exchange(client, req)
	var auth struct {
		Token string `json:"access_token"`
		Camel string `json:"accessToken"`
	}
	if res.fault != "" || res.status != 200 || json.Unmarshal(res.data, &auth) != nil {
		return ""
	}
	if auth.Token != "" {
		return auth.Token
	}
	return auth.Camel
}

type getPage func(scope string, limit, offset int) reply

func remote(client *http.Client, project, token string) getPage {
	return func(scope string, limit, offset int) reply {
		q := url.Values{"limit": {strconv.Itoa(limit)}}
		if scope != "public" {
			q.Set("project_id", project)
		}
		if offset > 0 || scope == "sample" {
			q.Set("offset", strconv.Itoa(offset))
		}
		req, _ := http.NewRequest(http.MethodGet, imagesURL+"?"+q.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		return exchange(client, req)
	}
}

// Only these explicitly approved counts, classes and constants reach stdout.
type page struct {
	Scope     string            `json:"scope"`
	Limit     int               `json:"limit"`
	Offset    int               `json:"offset"`
	HTTP      int               `json:"http,omitempty"`
	Items     string            `json:"items_shape"`
	TotalType string            `json:"total_type"`
	Total     *int              `json:"total,omitempty"`
	Rows      int               `json:"rows"`
	IDs       int               `json:"filled_ids"`
	Names     int               `json:"filled_names"`
	Unique    int               `json:"unique_ids"`
	Types     map[string]int    `json:"types,omitempty"`
	Fault     string            `json:"fault,omitempty"`
	members   map[string]string // raw ID and name, process memory only
}

type result struct {
	Pages         []*page `json:"catalogue_gets"`
	Coverage      string  `json:"project_coverage"`
	Stable        string  `json:"project_total_stability"`
	ProjectUnique int     `json:"observed_unique_project_ids"`
	Overlap       *int    `json:"project_observed_ids_on_public_first_page,omitempty"`
	PublicNames   *int    `json:"overlapping_ids_with_matching_names,omitempty"`
	Natural404    string  `json:"natural_404"`
	OK            bool    `json:"ok"`
}

func shape(v any, exists bool) string {
	if !exists {
		return "missing"
	}
	switch x := v.(type) {
	case nil:
		return "null"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	default:
		return "string"
	}
}

func natural404(b []byte) string {
	// Use the candidate's service/gateway discriminator. Ambiguous JSON null
	// is not affirmative evidence of a service reply.
	trim := bytes.TrimSpace(b)
	if len(trim) > 0 && json.Valid(trim) && trim[0] != '[' && trim[0] != '{' {
		return "unclassified_404"
	}
	req, _ := http.NewRequest(http.MethodGet, imagesURL, nil)
	res := &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))}
	classified := httpretry.RouteNotFoundAsBadGateway(req, res)
	defer classified.Body.Close()
	if classified.StatusCode != 404 {
		return "gateway_shape_404"
	}
	if !json.Valid(b) {
		return "unclassified_404"
	}
	return "service_shape_404"
}

// Reject duplicate object keys instead of silently accepting the last value.
func uniqueValue(dec *json.Decoder, depth int) bool {
	token, err := dec.Token()
	if err != nil || depth > 64 {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	if delim != '{' && delim != '[' {
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		if delim == '{' {
			key, err := dec.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
		}
		if !uniqueValue(dec, depth+1) {
			return false
		}
	}
	_, err = dec.Token()
	return err == nil
}

func (p *page) inspect(res reply) {
	p.HTTP = res.status
	if res.fault != "" {
		p.Fault = res.fault
		return
	}
	if res.status == 404 {
		p.Fault = natural404(res.data)
		return
	}
	if res.status != 200 {
		p.Fault = "http_error"
		return
	}
	keys := json.NewDecoder(bytes.NewReader(res.data))
	keys.UseNumber()
	if !json.Valid(res.data) || !uniqueValue(keys, 0) {
		p.Fault = "invalid_json"
		return
	}
	dec := json.NewDecoder(bytes.NewReader(res.data))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		p.Fault = "invalid_envelope"
		return
	}
	items, ok := obj["items"]
	p.Items = shape(items, ok)
	total, ok := obj["total"]
	p.TotalType = shape(total, ok)
	if p.TotalType == "integer" {
		n, err := strconv.Atoi(total.(json.Number).String())
		if err == nil && n >= 0 {
			p.Total = &n
		}
	}
	rows, ok := items.([]any)
	if !ok {
		p.Fault = "invalid_items"
		return
	}
	p.Rows = len(rows)
	p.members = make(map[string]string)
	p.Types = make(map[string]int)
	for _, row := range rows {
		fields, _ := row.(map[string]any)
		id, _ := fields["id"].(string)
		name, _ := fields["name"].(string)
		if strings.TrimSpace(id) != "" {
			p.IDs++
			p.members[id] = name
		}
		if strings.TrimSpace(name) != "" {
			p.Names++
		}
		t, ok := fields["type"]
		kind := "other"
		if s, isString := t.(string); isString && (s == "public" || s == "private" || s == "marketplace") {
			kind = s
		}
		if !ok || t == nil || t == "" {
			kind = "missing"
		}
		p.Types[kind]++
	}
	p.Unique = len(p.members)
	switch {
	case p.Total == nil:
		p.Fault = "invalid_total"
	case p.IDs != p.Rows || p.Names != p.Rows:
		p.Fault = "missing_identity"
	case p.Unique != p.Rows:
		p.Fault = "duplicate_id"
	case p.Rows > p.Limit || p.Offset > *p.Total || p.Rows > *p.Total-p.Offset:
		p.Fault = "inconsistent_count"
	case p.Rows == 0 && p.Offset < *p.Total:
		p.Fault = "early_empty_page"
	default:
		if echo, exists := obj["offset"]; exists && echo != json.Number(strconv.Itoa(p.Offset)) {
			p.Fault = "inconsistent_offset"
		}
	}
}

func observe(get getPage) result {
	r := result{Coverage: "incomplete", Stable: "unobserved", Natural404: "not_observed"}
	var firstTotal *int
	take := func(scope string, limit, offset int) *page {
		p := &page{Scope: scope, Limit: limit, Offset: offset, Items: "unobserved", TotalType: "unobserved"}
		if len(r.Pages) >= 5 {
			panic("internal GET budget exceeded")
		} // before any request
		r.Pages = append(r.Pages, p)
		p.inspect(get(scope, limit, offset))
		if p.HTTP == 404 {
			r.Natural404 = "observed_see_page"
		}
		if scope != "public" && p.Total != nil {
			if firstTotal == nil {
				firstTotal = p.Total
				r.Stable = "one_observation"
			} else if *p.Total != *firstTotal {
				r.Stable = "changed"
				p.Fault = "changed_total"
			} else if r.Stable != "changed" {
				r.Stable = "stable"
			}
		}
		return p
	}
	project := make(map[string]string)
	validProjectPage := false
	for pageNo := 1; pageNo <= 3; pageNo++ {
		p := take("project", 100, len(project))
		if p.Fault != "" {
			break
		}
		for id := range p.members {
			if _, dup := project[id]; dup {
				p.Fault = "repeated_page_member"
				break
			}
		}
		if p.Fault != "" {
			break
		}
		validProjectPage = true
		for id, name := range p.members {
			project[id] = name
		}
		if len(project) == *p.Total {
			r.Coverage = "complete"
			break
		}
		if pageNo == 3 {
			r.Coverage = "page_cap"
		}
	}
	r.ProjectUnique = len(project)
	if r.Coverage == "complete" && len(r.Pages) == 1 && len(project) >= 2 {
		sampled := make(map[string]bool)
		for offset := 0; offset < min(len(project), 3); offset++ {
			p := take("sample", 1, offset)
			if p.Fault == "" {
				for id, name := range p.members {
					if project[id] != name || sampled[id] {
						p.Fault = "sample_mismatch"
						break
					}
					sampled[id] = true
				}
			}
			if p.Fault != "" {
				r.Coverage = "incomplete"
				break
			}
		}
	}
	public := take("public", 100, 0)
	if validProjectPage && public.Fault == "" {
		hits, sameNames := 0, 0
		for id, name := range public.members {
			if other, ok := project[id]; ok {
				hits++
				if name == other {
					sameNames++
				}
			}
		}
		r.Overlap, r.PublicNames = &hits, &sameNames
	}
	r.OK = r.Coverage == "complete" && public.Fault == ""
	return r
}

func main() {
	project, key, secret := os.Getenv("CLOUDRU_PROJECT_ID"), os.Getenv("CLOUDRU_KEY_ID"), os.Getenv("CLOUDRU_SECRET")
	if strings.TrimSpace(project) == "" || key == "" || secret == "" {
		fmt.Println("G11: missing credential setting")
		os.Exit(1)
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DisableCompression: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token := login(client, key, secret)
	if token == "" {
		fmt.Println("G11: IAM authentication unconfirmed; catalogue GETs=0")
		os.Exit(1)
	}
	r := observe(remote(client, project, token))
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil || !r.OK {
		os.Exit(1)
	}
}
