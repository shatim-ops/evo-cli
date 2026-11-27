// SPDX-License-Identifier: Apache-2.0
package main

import (
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
)

const dnsHost = "dns.api.cloud.ru"

func dnsCollection(key string) string {
	switch key {
	case "dns-zone":
		return "/v1/privateZones"
	case "dns-record":
		return "/v1/privateRecordsSole"
	}
	return ""
}

func collectionPage(r *http.Request, field, value string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || value == "" || len(query[field]) != 1 || query.Get(field) != value {
		return false
	}
	switch len(query) {
	case 1:
		return true
	case 2:
		token := query["pageToken"]
		return len(token) == 1 && token[0] != "" && len(token[0]) <= 2048
	}
	return false
}

func (g *guard) dnsRead(r *http.Request) bool {
	path := r.URL.EscapedPath()
	host := r.URL.Host
	if g.s.Method == "POST" && g.s.Key == "dns-zone" {
		if host == "vpc.api.cloud.ru" && path == "/v1/vpcs" {
			return collectionPage(r, "project_id", g.s.Project)
		}
		if host == dnsHost && (path == "/v1/privateZones" || path == "/v1/dnsServers") {
			return collectionPage(r, "project_id", g.s.Project)
		}
	}
	if host != dnsHost {
		return false
	}
	if g.s.Method == "POST" && g.s.Key == "dns-record" {
		expected, _ := g.s.Expected.(obj)
		parent, _ := expected["private_zone_id"].(string)
		if !valid(parent) {
			return false
		}
		if path == "/v1/privateRecordsSole" {
			return collectionPage(r, "private_zone_id", parent)
		}
		if path == "/v1/privateZones/"+parent && r.URL.RawQuery == "" {
			return true
		}
	}
	if r.URL.RawQuery != "" {
		return false
	}
	if g.s.Method == "DELETE" && path == g.s.Path && valid(strings.TrimPrefix(path, dnsCollection(g.s.Key)+"/")) {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return valid(g.operation) && path == "/v1/operations/"+g.operation
}

func validDNSWrite(s spec) bool {
	collection := dnsCollection(s.Key)
	if collection == "" || s.Host != dnsHost || !valid(s.Project) || len(s.Query) != 0 || s.Shape != "vpcop" {
		return false
	}
	if s.Method == "DELETE" {
		return strings.HasPrefix(s.Path, collection+"/") && valid(strings.TrimPrefix(s.Path, collection+"/")) && s.Expected == nil
	}
	if s.Method != "POST" || s.Path != collection {
		return false
	}
	expected, ok := s.Expected.(obj)
	if !ok {
		return false
	}
	if s.Key == "dns-record" {
		return valid(expected["private_zone_id"]) && reflect.DeepEqual(expected, obj{
			"private_zone_id": expected["private_zone_id"], "name": "www", "type": "a",
			"ttl": float64(300), "values": []any{"198.18.255.10"},
		})
	}
	domain := "evo-p90-" + os.Getenv("P90_SOURCE_RUN") + ".test."
	links, ok := expected["linkedVpcIds"].([]any)
	return ok && len(links) == 1 && valid(links[0]) && reflect.DeepEqual(expected, obj{
		"name": domain, "domain": domain, "description": "p90-private-d7645da5efea",
		"linkedVpcIds": links, "project_id": s.Project,
	})
}
