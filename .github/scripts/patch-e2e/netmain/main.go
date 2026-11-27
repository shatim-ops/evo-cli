// SPDX-License-Identifier: Apache-2.0
// Run the pinned CLI with one permitted mutation. Its direct response is recorded
// before the CLI can recover a create by name or advance to a later cloud call.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shatim-ops/evo-cli/internal/cli"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

type obj = map[string]any
type spec struct {
	Key, Host, Path, Method, Project, Shape string
	Query                                   map[string]string
	Expected                                any
	GroupID                                 string
}

var denied = errors.New("network request or receipt outside the approved step")
var uuid = regexp.MustCompile("^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$")

func valid(v any) bool {
	s, ok := v.(string)
	return ok && uuid.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func match(got, want any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !match(g[k], v) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i, v := range w {
			if !match(g[i], v) {
				return false
			}
		}
		return true
	default:
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		return bytes.Equal(a, b)
	}
}
func privateVM(v any) bool {
	rows, ok := v.([]any)
	if !ok || len(rows) != 1 {
		return false
	}
	vm, ok := rows[0].(obj)
	if !ok {
		return false
	}
	for k := range vm {
		switch k {
		case "project_id", "name", "flavor_name", "image_name", "availability_zone_id", "disks", "interfaces", "image_metadata":
		default:
			return false
		}
	}
	arr, ok := vm["interfaces"].([]any)
	if !ok || len(arr) != 1 {
		return false
	}
	card, ok := arr[0].(obj)
	if !ok {
		return false
	}
	for k := range card {
		switch k {
		case "subnet_id", "security_groups", "type", "interface_security_enabled":
		default:
			return false
		}
	}
	return true
}
func read(b io.ReadCloser) ([]byte, error) {
	defer b.Close()
	data, e := io.ReadAll(io.LimitReader(b, 4<<20+1))
	if e != nil || len(data) > 4<<20 {
		return nil, denied
	}
	return data, nil
}
func checkpoint(key string, fields obj) error {
	b, err := json.Marshal(obj{"kind": "network-" + key, "key": "network." + key, "fields": fields})
	if err != nil {
		return err
	}
	cmd := exec.Command("python3", os.Getenv("P90_DRIVER"), "checkpoint")
	cmd.Stdin = bytes.NewReader(b)
	return cmd.Run()
}
func direct(shape string, data []byte) (obj, error) {
	var result any
	if json.Unmarshal(data, &result) != nil {
		return nil, denied
	}
	if rows, ok := result.([]any); ok {
		if shape != "vm" || len(rows) != 1 {
			return nil, denied
		}
		result = rows[0]
	}
	m, ok := result.(map[string]any)
	if !ok || !valid(m["id"]) {
		return nil, denied
	}
	out := obj{"id": m["id"]}
	if shape == "vpcop" || shape == "nlbop" || shape == "dnsop" {
		out = obj{"operation": m["id"]}
		field := "resourceId"
		if shape == "nlbop" {
			field = "resource_id"
		}
		value := m[field]
		if shape == "dnsop" { // DNS accepts both wire spellings of the resource UUID.
			alt := m["resource_id"]
			if alt != nil && alt != "" {
				if !valid(alt) || value != nil && value != "" && (!valid(value) || value != alt) {
					return nil, denied
				}
				value = alt
			}
		}
		if v := value; v != nil && v != "" {
			if !valid(v) {
				return nil, denied
			}
			out["id"] = v
		}
	}
	return out, nil
}

type guard struct {
	write     func(string, obj) error
	s         spec
	base      http.RoundTripper
	sent      bool
	operation string
	mu        sync.Mutex
}

func queryEqual(r *http.Request, expected map[string]string) bool {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) != len(expected) {
		return false
	}
	for k, v := range expected {
		if len(q[k]) != 1 || q[k][0] != v {
			return false
		}
	}
	return true
}
func (g *guard) RoundTrip(r *http.Request) (*http.Response, error) {
	h, p := r.URL.Host, r.URL.EscapedPath()
	if r.URL.Scheme != "https" || r.URL.User != nil || r.URL.Fragment != "" {
		return nil, denied
	}
	if h == "iam.api.cloud.ru" && r.Method == "POST" && p == "/api/v1/auth/token" && r.URL.RawQuery == "" {
		return g.base.RoundTrip(r)
	}
	isDNS := dnsCollection(g.s.Key) != ""
	if isDNS {
		if !validDNSWrite(g.s) {
			return nil, denied
		}
		if r.Method == "GET" {
			if !g.dnsRead(r) {
				return nil, denied
			}
			return g.base.RoundTrip(r)
		}
		if h != dnsHost {
			return nil, denied
		}
	} else if h != "compute.api.cloud.ru" && h != "vpc.api.cloud.ru" && h != "nlb.api.cloud.ru" {
		return nil, denied
	}
	// The provision tool chooses a default from its discovery result. Limit only
	// this fixture's selector to the exact run-owned group, read live by its ID.
	if r.Method == "GET" && g.s.Key == "backend" && g.s.Method == "POST" && h == "compute.api.cloud.ru" && p == "/api/v1/security-groups" {
		if !valid(g.s.GroupID) || r.URL.Query().Get("project_id") != g.s.Project {
			return nil, denied
		}
		card := r.Clone(r.Context())
		u := *r.URL
		u.Path = p + "/" + g.s.GroupID
		u.RawPath = ""
		u.RawQuery = ""
		card.URL = &u
		result, e := g.base.RoundTrip(card)
		if e != nil {
			return nil, e
		}
		body, e := read(result.Body)
		var entry obj
		if e != nil || result.StatusCode != 200 || result.Header.Get("Content-Range") != "" || result.ContentLength >= 0 && int64(len(body)) != result.ContentLength || json.Unmarshal(body, &entry) != nil || entry["id"] != g.s.GroupID {
			return nil, denied
		}
		if pid, ok := entry["project_id"]; ok && pid != g.s.Project {
			return nil, denied
		}
		body, _ = json.Marshal(obj{"items": []obj{entry}, "offset": 0, "total": 1})
		result.Body = io.NopCloser(bytes.NewReader(body))
		result.Header.Del("Content-Length")
		result.ContentLength = int64(len(body))
		return result, nil
	}
	if r.Method == "GET" {
		return g.base.RoundTrip(r)
	}
	if h == "compute.api.cloud.ru" && r.Method == "POST" && p == "/api/v1/projects/"+g.s.Project+"/price-calculation" && r.URL.RawQuery == "" {
		return g.base.RoundTrip(r)
	}
	if h != g.s.Host || p != g.s.Path || r.Method != g.s.Method || !queryEqual(r, g.s.Query) {
		return nil, denied
	}
	if r.Method == "POST" {
		if r.Body == nil {
			return nil, denied
		}
		b, e := read(r.Body)
		if e != nil {
			return nil, e
		}
		var v any
		if json.Unmarshal(b, &v) != nil {
			return nil, denied
		}
		if !match(v, g.s.Expected) || isDNS && !reflect.DeepEqual(v, g.s.Expected) || g.s.Shape == "vm" && !privateVM(v) {
			return nil, denied
		}
		r = r.Clone(r.Context())
		r.Body = io.NopCloser(bytes.NewReader(b))
	} else if r.Method != "DELETE" || isDNS && r.Body != nil && r.Body != http.NoBody {
		return nil, denied
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sent {
		return nil, denied
	}
	g.sent = true
	creating := r.Method == "POST"
	dispatched := "delete_dispatched"
	if creating {
		dispatched = "create_dispatched"
	}
	if e := g.write(g.s.Key, obj{dispatched: true}); e != nil {
		return nil, denied
	}
	resp, e := g.base.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	data, e := read(resp.Body)
	if e != nil || resp.StatusCode == 206 || resp.Header.Get("Content-Range") != "" || resp.ContentLength >= 0 && int64(len(data)) != resp.ContentLength {
		return nil, denied
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if resp.StatusCode/100 != 2 {
		return resp, nil
	}
	fields := obj{"delete_accepted": true}
	if creating || g.s.Shape == "vpcop" || g.s.Shape == "nlbop" {
		shape := g.s.Shape
		if isDNS {
			shape = "dnsop"
		}
		got, e := direct(shape, data)
		if e != nil {
			return nil, denied
		}
		if isDNS {
			if !creating && got["id"] != nil && g.s.Path != dnsCollection(g.s.Key)+"/"+got["id"].(string) {
				return nil, denied
			}
			g.operation, _ = got["operation"].(string)
		}
		if creating {
			fields = got
			fields["returned_directly"] = true
		} else {
			fields["delete_operation"] = got["operation"]
			if rid := got["id"]; rid != nil {
				fields["delete_resource_id"] = rid
			}
		}
	}
	if e = g.write(g.s.Key, fields); e != nil {
		return nil, denied
	}
	return resp, nil
}
func main() {
	var s spec
	if json.Unmarshal([]byte(os.Getenv("P90_NET_CALL")), &s) != nil || !valid(s.Project) || !regexp.MustCompile("^(vpc|subnet|route|tg|lb|snat|backend|boot|dns-zone|dns-record)$").MatchString(s.Key) || !strings.HasPrefix(s.Path, "/") {
		fmt.Fprintln(os.Stderr, denied)
		os.Exit(1)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	tr.Protocols = new(http.Protocols)
	tr.Protocols.SetHTTP1(true)
	g := &guard{s: s, base: tr, write: checkpoint}
	httpretry.DefaultBase = g
	http.DefaultTransport = g
	cli.Main("1.9.1-candidate", "d7645da5efea04122f984cfd8cb9a28408d08177", "")
}
