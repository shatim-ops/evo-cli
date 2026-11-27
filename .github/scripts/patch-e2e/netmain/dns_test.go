// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/dns"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

func zoneSpec() spec {
	return spec{Key: "dns-zone", Method: "POST", Host: dnsHost, Path: "/v1/privateZones", Project: a, Shape: "vpcop",
		Expected: obj{"domain": "evo-p90-123.test.", "name": "evo-p90-123.test.", "description": "p90-private-d7645da5efea", "project_id": a, "linkedVpcIds": []any{b}}}
}
func recordSpec() spec {
	return spec{Key: "dns-record", Method: "POST", Host: dnsHost, Path: "/v1/privateRecordsSole", Project: a, Shape: "vpcop",
		Expected: obj{"private_zone_id": a, "name": "www", "type": "a", "values": []any{"198.18.255.10"}, "ttl": float64(300)}}
}
func TestPrivateDNSCandidateDirectWrites(t *testing.T) {
	t.Setenv("P90_SOURCE_RUN", "123")
	old := httpretry.DefaultBase
	t.Cleanup(func() { httpretry.DefaultBase = old })
	for _, base := range []spec{zoneSpec(), recordSpec()} {
		for _, method := range []string{"POST", "DELETE"} {
			s := base
			if method == "DELETE" {
				s.Method = "DELETE"
				s.Path += "/" + a
				s.Expected = nil
			}
			t.Run(s.Key+"/"+method, func(t *testing.T) {
				writes := []obj{}
				seen := 0
				guard := &guard{s: s, write: func(key string, fields obj) error {
					if key != s.Key {
						t.Fatal("wrong receipt key")
					}
					writes = append(writes, fields)
					return nil
				},
					base: roundFunc(func(r *http.Request) (*http.Response, error) {
						seen++
						if r.Method != method || r.URL.Path != s.Path {
							t.Fatal("wrong client request", r.Method, r.URL.Path)
						}
						return &http.Response{StatusCode: 202, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"` + b + `","resource_id":"` + a + `","done":false}`))}, nil
					})}
				httpretry.DefaultBase = guard
				cli := dns.New("offline-fixture", a, "")
				var op *dns.Operation
				var err error
				ctx := context.Background()
				switch {
				case s.Key == "dns-record" && method == "POST":
					op, err = cli.CreateRecord(ctx, "private", s.Expected.(obj))
				case s.Key == "dns-record":
					op, err = cli.DeleteRecord(ctx, "private", a)
				case method == "POST":
					op, err = cli.CreateZone(ctx, "private", s.Expected.(obj))
				default:
					op, err = cli.DeleteZone(ctx, "private", a)
				}
				if err != nil || op == nil || op.ResourceID != a || seen != 1 || len(writes) != 2 {
					t.Fatal(err, seen, writes)
				}
				if method == "POST" && (writes[1]["returned_directly"] != true || writes[1]["id"] != a || writes[1]["operation"] != b) {
					t.Fatal("create receipt", writes)
				}
				if method == "DELETE" && (writes[1]["delete_accepted"] != true || writes[1]["delete_resource_id"] != a || writes[1]["delete_operation"] != b) {
					t.Fatal("delete receipt", writes)
				}
				if _, err := direct("dnsop", []byte(`{"id":"`+b+`","resourceId":"`+a+`","resource_id":"`+b+`"}`)); err == nil {
					t.Fatal("ambiguous direct response")
				}
			})
		}
	}
}

func TestPrivateDNSNoUnscopedReadsOrCrossServiceWrites(t *testing.T) {
	t.Setenv("P90_SOURCE_RUN", "123")
	calls := 0
	base := roundFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	g := &guard{s: zoneSpec(), base: base, write: func(_ string, _ obj) error { t.Fatal("unexpected mutation checkpoint"); return nil }}
	check := func(method, address string, data []byte, want bool) {
		t.Helper()
		r, err := http.NewRequest(method, address, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		old := calls
		_, err = g.RoundTrip(r)
		if (err == nil) != want || ((calls-old) == 1) != want {
			t.Fatalf("%s %s: %v, forwarded %d", method, address, err, calls-old)
		}
	}
	check("GET", "https://vpc.api.cloud.ru/v1/vpcs?project_id="+a, nil, true)
	check("GET", "https://dns.api.cloud.ru/v1/dnsServers?project_id="+a+"&pageToken=second", nil, true)
	check("GET", "https://dns.api.cloud.ru/v1/privateZones?project_id="+a, nil, true)
	for _, u := range []string{"https://dns.api.cloud.ru/v1/publicZones?project_id=" + a, "https://vpc.api.cloud.ru/v1/vpcs?project_id=" + b, "https://compute.api.cloud.ru/api/v1/vms", "https://dns.api.cloud.ru/v1/privateZones?project_id=" + a + "&scope=all", "https://dns.api.cloud.ru/v1/operations/" + b} {
		check("GET", u, nil, false)
	}
	check("POST", "https://compute.api.cloud.ru/api/v1/projects/"+a+"/price-calculation", []byte("{}"), false)
	g.s = recordSpec()
	collection := "https://dns.api.cloud.ru/v1/privateRecordsSole"
	check("GET", collection+"?private_zone_id="+a, nil, true)
	check("GET", collection+"?private_zone_id="+b, nil, false)
	body := recordSpec().Expected.(obj)
	body["note"] = "unexpected"
	payload, _ := json.Marshal(body)
	check("POST", collection, payload, false)
	delete(body, "note")
	body["private_zone_id"] = b
	payload, _ = json.Marshal(body)
	check("POST", collection, payload, false)
	check("PATCH", "https://dns.api.cloud.ru/v1/privateZones/"+a, []byte(`{"linkedVpcIds":[]}`), false)
	g.s = spec{Key: "dns-record", Method: "DELETE", Host: dnsHost, Path: "/v1/privateRecordsSole/" + a, Project: a, Shape: "vpcop"}
	check("GET", collection+"/"+a, nil, true)
	check("GET", collection+"/"+b, nil, false)
	check("DELETE", collection+"/"+b, nil, false)
	g.s.Key = "backend"
	check("GET", collection+"/"+a, nil, false)
}
