// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// subnetRegistryWaiting is subnetRegistry whose waits for a resource made
// end in 20 ms: a create the list does not show answers waiting at once.
func subnetRegistryWaiting(t *testing.T, api *subnetAPI) *Registry { return waitingRegistry(t, api) }

// waitingRegistry is a registry over api whose waits for a resource made
// end in 20 ms.
func waitingRegistry(t *testing.T, api http.RoundTripper) *Registry {
	t.Helper()
	old := httpretry.DefaultBase
	httpretry.DefaultBase = api
	t.Cleanup(func() { httpretry.DefaultBase = old })
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: raProject, KeyID: "k", Secret: "s"}, Auth: staticToken{}, Cloud: cloud,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 20 * time.Millisecond}}, nil
	})
	r.SetStateDir(t.TempDir())
	return r
}

// Астра #1963 (release plan 1.9, the line of #1958): a create that answered
// waiting — the platform took it and named its id, and the list does not
// show it — is not sent again by the same call repeated with its consent:
// the repeat answers the state of that id and sends no second create. Once
// the list shows it, the repeat answers it made, and a later call is a new
// one again.
func TestSubnetCreate_ARepeatAfterWaitingSendsNoSecondCreate(t *testing.T) {
	api := newSubnetAPI()
	api.unlisted = true
	r := subnetRegistryWaiting(t, api)
	args := map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "confirmed": true}
	m, text, _ := callNamed(t, r, "subnet_create", args)
	if m["status"] != "waiting" || m["subnet_id"] != subnetNew {
		t.Fatalf("the first call: %s", text)
	}
	for _, confirmed := range []bool{true, false} {
		args["confirmed"] = confirmed
		m, text, _ = callNamed(t, r, "subnet_create", args)
		if n := len(api.writes); n != 1 {
			t.Fatalf("confirmed=%v: %d creates sent, want 1: %v", confirmed, n, api.writes)
		}
		if rep, _ := m["repeat_of"].(map[string]any); rep["id"] != subnetNew || m["next_tool"] != "list_subnets" {
			t.Errorf("confirmed=%v: the repeat does not read the first create: %s", confirmed, text)
		}
	}
	// The list shows it now: the repeat answers it made, no create.
	api.mu.Lock()
	api.unlisted = false
	api.mu.Unlock()
	args["confirmed"] = true
	_, text, _ = callNamed(t, r, "subnet_create", args)
	if n := len(api.writes); n != 1 || !strings.Contains(text, subnetNew) {
		t.Errorf("after the list shows it: %d creates, %s", n, text)
	}
	if n := madeFiles(t, r); n != 0 {
		t.Errorf("the file of the create stays after the list shows it: %d", n)
	}
}

// natWaitAPI is compute's SNAT gateways with none listed: a POST is taken
// (CREATING, an id) and counted, and the list shows it only once listed.
type natWaitAPI struct {
	mu     sync.Mutex
	posts  int
	listed bool
}

func (a *natWaitAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/nat-gateways" && r.Method == http.MethodPost:
		a.posts++
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e2","name":"evo-n","status":"CREATING"}`)
	case r.URL.Path == "/api/v1/nat-gateways" && a.listed:
		_, _ = io.WriteString(rec, `{"items":[{"id":"00000000-0000-4000-8000-0000000000e2","name":"evo-n","status":"CREATED"}],"total":1}`)
	case r.URL.Path == "/api/v1/nat-gateways":
		_, _ = io.WriteString(rec, `{"items":[],"total":0}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// The same for snat_create (Астра #1963): the repeat of a gateway taken and
// not listed sends no second POST, with consent or without.
func TestSnatCreate_ARepeatAfterWaitingSendsNoSecondCreate(t *testing.T) {
	api := &natWaitAPI{}
	r := waitingRegistry(t, api)
	args := map[string]any{"name": "evo-n", "availability_zone_id": "00000000-0000-4000-8000-0000000000a1", "confirmed": true}
	m, text, _ := callNamed(t, r, "snat_create", args)
	if m["status"] != "waiting" || m["nat_gateway_id"] != "00000000-0000-4000-8000-0000000000e2" {
		t.Fatalf("the first call: %s", text)
	}
	for _, confirmed := range []bool{true, false} {
		args["confirmed"] = confirmed
		m, text, _ = callNamed(t, r, "snat_create", args)
		if rep, _ := m["repeat_of"].(map[string]any); api.posts != 1 || rep["id"] != "00000000-0000-4000-8000-0000000000e2" {
			t.Errorf("confirmed=%v: %d POSTs, %s", confirmed, api.posts, text)
		}
	}
	api.mu.Lock()
	api.listed = true
	api.mu.Unlock()
	args["confirmed"] = true
	if _, text, _ = callNamed(t, r, "snat_create", args); api.posts != 1 || !strings.Contains(text, "00000000-0000-4000-8000-0000000000e2") {
		t.Errorf("listed: %d POSTs, %s", api.posts, text)
	}
}

// The full audit of Астра 14.11, finding 01 (release plan 1.9, the line of
// #1992): a create whose answer came, whose operation goes on in another
// tool's read, while the list does not show it yet — the repeat with the
// consent of the first call reads that operation and sends no second
// create. Every create of the matrix whose answer goes on that way.
// finding01 are the ten creates of finding 01 of the full audit of Астра
// 14.11.
var finding01 = map[string]bool{"pg_create": true, "kafka_create": true, "redis_create": true, "k8s_create": true, "k8s_add_nodepool": true,
	"lb_create": true, "ar_create": true, "vpc_create": true, "tg_create": true, "pg_backup_create": true}

func TestCreate_ARepeatWhileItsOperationGoesOnSendsNoSecondCreate(t *testing.T) {
	ran := map[string]bool{}
	for _, c := range faultMatrixCases {
		if !finding01[c.tool] {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			api, r, _, _, _ := paidCreateWorld(t, c, 1000)
			first, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if _, _, _, goesOn := madeGoesOn(c.tool, first); !goesOn {
				t.Skipf("the first call's answer does not go on in another read: %.600s", first)
			}
			ran[c.tool] = true
			sent := creates(api)
			text, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if creates(api) != sent {
				t.Fatalf("the repeat sent a second create: %d, were %d\n%s", creates(api), sent, text)
			}
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if rep, _ := m["repeat_of"].(map[string]any); rep == nil || m["next_tool"] == nil {
				t.Errorf("the repeat does not name the first create and its read: %s", text)
			}
		})
	}
	for tool := range finding01 {
		if !ran[tool] {
			t.Errorf("%s: the case of the finding was not run (its answer in the matrix does not go on in another read)", tool)
		}
	}
}

// A call by the same name with other arguments is not a repeat of the one
// that goes on: it is planned as any other — here refused before the
// question for its own reason — and the record of the first stays.
func TestSubnetCreate_ACallOfOtherArgumentsIsNotARepeat(t *testing.T) {
	api := newSubnetAPI()
	api.unlisted = true
	r := subnetRegistryWaiting(t, api)
	args := map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "confirmed": true}
	callNamed(t, r, "subnet_create", args)
	other := map[string]any{"name": "web", "cidr": "10.0.0.0/25", "zone": "ru.AZ-1", "confirmed": true}
	m, text, _ := callNamed(t, r, "subnet_create", other)
	if m["repeat_of"] != nil || m["reason"] != "USAGE" || len(api.writes) != 1 {
		t.Errorf("another call by the name: %d creates, %s", len(api.writes), text)
	}
	if m, text, _ = callNamed(t, r, "subnet_create", args); m["repeat_of"] == nil || len(api.writes) != 1 {
		t.Errorf("the same call after it: %d creates, %s", len(api.writes), text)
	}
}

// madeFiles counts the files of creates going on in the registry's state.
func madeFiles(t *testing.T, r *Registry) int {
	t.Helper()
	root, _ := r.stateDir()
	entries, _ := os.ReadDir(MadeDir(root))
	return len(entries)
}

// «Риск», conclusion 268, 1: a create taken, then the resource not there —
// past the grace of a list that lags, the read by id lists none of it: the
// repeat with consent sends one new create, and the file goes. Within the
// grace the same read is a list that does not show it yet: no create.
func TestSubnetCreate_ARepeatAfterTheResourceIsGoneCreatesAgain(t *testing.T) {
	api := newSubnetAPI()
	api.unlisted = true
	r := subnetRegistryWaiting(t, api)
	now := time.Date(2026, 11, 14, 22, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = time.Now })
	args := map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "confirmed": true}
	callNamed(t, r, "subnet_create", args)
	now = now.Add(madeListGrace / 2)
	if m, text, _ := callNamed(t, r, "subnet_create", args); m["repeat_of"] == nil || len(api.writes) != 1 {
		t.Fatalf("within the grace: %d creates, %s", len(api.writes), text)
	}
	now = now.Add(madeListGrace)
	if m, text, _ := callNamed(t, r, "subnet_create", args); m["repeat_of"] != nil || len(api.writes) != 2 {
		t.Errorf("past the grace, the resource not listed by id: %d creates, %s", len(api.writes), text)
	}
}

// «Риск», conclusion 268, 2: a file older than madeMaxAge is not read; it
// goes, and the call is planned as any other.
func TestSubnetCreate_AnOldFileOfACreateIsNotRead(t *testing.T) {
	api := newSubnetAPI()
	api.unlisted = true
	r := subnetRegistryWaiting(t, api)
	now := time.Date(2026, 11, 14, 22, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = time.Now })
	args := map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "confirmed": true}
	callNamed(t, r, "subnet_create", args)
	if madeFiles(t, r) != 1 {
		t.Fatal("no file of the create going on")
	}
	now = now.Add(madeMaxAge + time.Minute)
	if m, text, _ := callNamed(t, r, "subnet_create", args); m["repeat_of"] != nil || len(api.writes) != 2 {
		t.Errorf("an old file: %d creates, %s", len(api.writes), text)
	}
}

// What tells a create ended for its repeat, and what does not.
func TestMadeEnded(t *testing.T) {
	for _, c := range []struct {
		read  string
		isErr bool
		age   time.Duration
		want  bool
	}{
		{`{"reason":"NOT_FOUND","error":"x"}`, true, 0, true},
		{`{"reason":"API_UNREACHABLE","error":"x"}`, true, time.Hour, false},
		{`{"status":"error_creating"}`, false, 0, true},
		{`{"state":"ERROR"}`, false, 0, true},
		{`{"status":"failed"}`, false, 0, true},
		{`{"status":"creating"}`, false, time.Hour, false},
		{`{"count":0,"subnets":[]}`, false, madeListGrace / 2, false},
		{`{"count":0,"subnets":[]}`, false, madeListGrace * 2, true},
		{`{"count":1,"subnets":[{}]}`, false, time.Hour, false},
	} {
		if got := madeEnded(c.read, c.isErr, c.age); got != c.want {
			t.Errorf("madeEnded(%s, %v, %v) = %v, want %v", c.read, c.isErr, c.age, got, c.want)
		}
	}
}
