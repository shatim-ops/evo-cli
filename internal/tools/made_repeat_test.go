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
	"path/filepath"
	"slices"
	"strconv"
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

// madeRepeatCreates are the creates whose repeat, while the answer of the
// first goes on (madeGoesOn), reads its step instead of asking the consent
// again: the list the «Миграция» of D113 names
// (changelog.d/made-repeat-no-second-create.md). A create of the matrix
// whose answer goes on and is not here fails the test — the «Миграция»
// would miss it (Астра, #2075: disk_create, dns_zone_create) — and one here
// whose answer no longer goes on fails it too.
var madeRepeatCreates = map[string]bool{"pg_create": true, "kafka_create": true, "redis_create": true, "k8s_create": true,
	"k8s_add_nodepool": true, "lb_create": true, "ar_create": true, "vpc_create": true, "tg_create": true, "pg_backup_create": true,
	"subnet_create": true, "snat_create": true, "disk_create": true, "dns_zone_create": true}

// madeRepeatPending are the creates whose answer goes on, not yet in the
// «Миграция» of D113, with the line of the plan that brings them there.
var madeRepeatPending = map[string]string{}

// madeRepeatOwnWorld are the creates of the matrix the world of a paid
// create does not run, with why; their repeats are their own tests'.
var madeRepeatOwnWorld = map[string]string{
	"provision":    "the VM's create goes through the engine; its repeat after a lost answer — TestPaidCreate_*, provision's own",
	"deploy_apply": "a plan applied by id; its repeat is the plan's (deploy_apply tests)",
}

func TestCreate_EveryCreateWhoseAnswerGoesOnIsInTheMigration(t *testing.T) {
	for _, c := range faultMatrixCases {
		if rule, changes := everySendIsAChange[c.tool]; changes && !rule.made {
			continue // read before it is sent, not repeated by its answer
		}
		if _, isCreate := createdOf(c.tool); !isCreate || madeRepeatOwnWorld[c.tool] != "" {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			_, r, _, _, _ := paidCreateWorld(t, &c, 1000)
			first, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			_, _, _, goesOn := madeGoesOn(c.tool, first)
			// subnet_create's answer in this world is created at once: its
			// waiting answer is TestSubnetCreate_*'s.
			switch {
			case goesOn && !madeRepeatCreates[c.tool] && madeRepeatPending[c.tool] == "":
				t.Errorf("the answer goes on, so a repeat reads it with no question: add %s to madeRepeatCreates and to the «Миграция» of D113\n%.400s", c.tool, first)
			case !goesOn && (madeRepeatCreates[c.tool] || madeRepeatPending[c.tool] != "") && c.tool != "subnet_create":
				t.Errorf("in madeRepeatCreates, but the answer does not go on: %.400s", first)
			}
		})
	}
}

func TestCreate_ARepeatWhileItsOperationGoesOnSendsNoSecondCreate(t *testing.T) {
	ran := map[string]bool{}
	for _, c := range faultMatrixCases {
		if !finding01[c.tool] {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			api, r, _, _, _ := paidCreateWorld(t, &c, 1000)
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
			// An answer that names its operation goes on in the read of
			// it, not of the list (the remainder of finding 01: ar, lb, tg,
			// vpc; §6, axis 2).
			var f map[string]any
			_ = json.Unmarshal([]byte(first), &f)
			if f["operation_id"] != nil {
				if next, _ := m["next_tool"].(string); !strings.HasSuffix(next, "_operation") {
					t.Errorf("the create answers its operation %v, the repeat reads %v: %s", f["operation_id"], m["next_tool"], text)
				}
				if next, _ := f["next_tool"].(string); !strings.HasSuffix(next, "_operation") {
					t.Errorf("the create answers its operation %v and names the step %v: %s", f["operation_id"], f["next_tool"], first)
				}
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
		op    bool // the read is of an operation
	}{
		{`{"reason":"NOT_FOUND","error":"x"}`, true, 0, true, false},
		// An operation not found yet is not a resource gone: within
		// madeListGrace it goes on («Риск», conclusion 310, У1).
		{`{"reason":"NOT_FOUND","error":"x"}`, true, 0, false, true},
		{`{"reason":"NOT_FOUND","error":"x"}`, true, madeListGrace * 2, true, true},
		{`{"reason":"API_UNREACHABLE","error":"x"}`, true, time.Hour, false, false},
		{`{"status":"error_creating"}`, false, 0, true, false},
		{`{"state":"ERROR"}`, false, 0, true, false},
		{`{"status":"failed"}`, false, 0, true, false},
		{`{"status":"creating"}`, false, time.Hour, false, false},
		{`{"count":0,"subnets":[]}`, false, madeListGrace / 2, false, false},
		{`{"count":0,"subnets":[]}`, false, madeListGrace * 2, true, false},
		{`{"count":1,"subnets":[{}]}`, false, time.Hour, false, false},
		// The read of an operation: ended with an error, done, going on.
		{`{"id":"op-1","done":true,"error":{"code":13,"message":"x"}}`, false, 0, true, false},
		{`{"id":"op-1","done":true,"error":"x"}`, false, 0, true, false},
		{`{"id":"op-1","done":true,"resource_status":"ACTIVE"}`, false, 0, false, false},
		// Done with no error and the resource not listed (the repeat
		// read the list first): within madeListGrace the list may lag;
		// past it the resource is gone (A19-8, #2428).
		{`{"id":"op-1","done":true}`, false, madeListGrace / 2, false, true},
		{`{"id":"op-1","done":true}`, false, madeListGrace * 2, true, true},
		{`{"id":"op-1","done":false}`, false, madeListGrace * 2, false, true},
		{`{"id":"op-1","done":false}`, false, time.Hour, false, false},
	} {
		if got := madeEnded(c.read, c.isErr, c.age, c.op, true); got != c.want {
			t.Errorf("madeEnded(%s, %v, %v, op %v) = %v, want %v", c.read, c.isErr, c.age, c.op, got, c.want)
		}
	}
	// A list that did not answer tells nothing of the resource: a done
	// operation past the grace does not end the create («Риск», 405, У1).
	if madeEnded(`{"id":"op-1","done":true}`, false, madeListGrace*2, true, false) {
		t.Error("a done operation ends the create though the list was not read")
	}
	// done of a step that is no operation: no word of the resource
	// («Риск», 405, У3).
	if madeEnded(`{"id":"x","done":true}`, false, madeListGrace*2, false, true) {
		t.Error("done of a step that is no operation ends the create")
	}
}

// dnsWaitAPI is Evolution DNS with a public zone taken and not listed yet:
// a POST of a zone is taken (an operation that goes on, the zone's id) and
// counted; the private zones list another zone of the project.
type dnsWaitAPI struct {
	mu    sync.Mutex
	posts int
	calls []string
	// done: the operation of the create is done with no error.
	done bool
	// listFails: the lists of zones answer 500.
	listFails bool
	// listed: the public list shows the zone of the create, and its
	// delete is taken and done.
	listed bool
}

func (a *dnsWaitAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/v1/publicZones" && r.Method == http.MethodPost:
		a.posts++
		_, _ = io.WriteString(rec, `{"id":"op-1","resourceId":"00000000-0000-4000-8000-0000000000f1","done":false}`)
	case a.listFails && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "Zones"):
		http.Error(rec, `{"message":"internal"}`, http.StatusInternalServerError)
	case a.listed && r.URL.Path == "/v1/publicZones":
		_, _ = io.WriteString(rec, `{"zones":[{"id":"00000000-0000-4000-8000-0000000000f1","name":"evo-z","domain":"example.com."}]}`)
	case a.listed && r.Method == http.MethodDelete && r.URL.Path == "/v1/publicZones/00000000-0000-4000-8000-0000000000f1":
		_, _ = io.WriteString(rec, `{"id":"op-del","done":true}`)
	case r.URL.Path == "/v1/operations/op-del":
		_, _ = io.WriteString(rec, `{"id":"op-del","done":true}`)
	case r.URL.Path == "/v1/publicZones":
		_, _ = io.WriteString(rec, `{"zones":[]}`)
	case r.URL.Path == "/v1/privateZones":
		_, _ = io.WriteString(rec, `{"zones":[{"id":"00000000-0000-4000-8000-0000000000f9","name":"other","domain":"other.example."}]}`)
	case strings.HasPrefix(r.URL.Path, "/v1/operations/"):
		_, _ = io.WriteString(rec, `{"id":"op-1","resourceId":"00000000-0000-4000-8000-0000000000f1","done":`+strconv.FormatBool(a.done)+`}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// «Риск», conclusion 302, У1 (the line #2145): dns_zone_create's repeat,
// while the zone is taken and not listed, reads the step its answer names —
// the operation (dns_operation) — and answers waiting with repeat_of: no
// second POST, with consent or without. (A, #2146, took the read for
// dns_zones with no argument, the private zones only; dns_zones reads both
// visibilities — handleDnsZones — and the step is the operation now, C(7).
// Both visibilities: TestDNSZoneCreate_RepeatReadsItsOperationInBothVisibilities.)
func TestDNSZoneCreate_ARepeatWhileTheZoneIsNotListedSendsNoSecondCreate(t *testing.T) {
	api := &dnsWaitAPI{}
	r := waitingRegistry(t, api)
	args := map[string]any{"name": "evo-z", "domain": "example.com.", "confirmed": true}
	m, text, _ := callNamed(t, r, "dns_zone_create", args)
	if m["zone_id"] != "00000000-0000-4000-8000-0000000000f1" || api.posts != 1 {
		t.Fatalf("the first call: %d POSTs, %s\n%v", api.posts, text, api.calls)
	}
	for _, confirmed := range []bool{false, true} {
		args["confirmed"] = confirmed
		m, text, _ = callNamed(t, r, "dns_zone_create", args)
		rep, _ := m["repeat_of"].(map[string]any)
		if api.posts != 1 || rep["id"] != "00000000-0000-4000-8000-0000000000f1" || m["status"] != "waiting" {
			t.Errorf("confirmed=%v: %d POSTs, %s", confirmed, api.posts, text)
		}
	}
}

// A create whose operation is done with no error and whose resource is
// not listed — deleted after it was made: within madeListGrace the repeat
// says the operation is done and the list does not show it yet, not «its
// outcome is not known»; past it the file goes, and the same call creates
// anew — not «waiting» for two hours (A19-8, #2428). A delete of the
// resource takes its file away at once.
func TestDNSZoneCreate_ARepeatAfterADoneOperationOfAGoneZone(t *testing.T) {
	api := &dnsWaitAPI{done: true}
	r := waitingRegistry(t, api)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = time.Now })
	args := map[string]any{"name": "evo-z", "domain": "example.com.", "confirmed": true}
	callNamed(t, r, "dns_zone_create", args)
	if api.posts != 1 {
		t.Fatalf("the first call: %d POSTs", api.posts)
	}
	now = now.Add(madeListGrace / 2)
	m, text, _ := callNamed(t, r, "dns_zone_create", args)
	msg, _ := m["message"].(string)
	// The words: the list lags for the whole grace, and a new create is
	// the person's decision, not a fact of a deletion («Риск», 405, У2).
	if m["repeat_of"] == nil || api.posts != 1 || strings.Contains(msg, "not known") || !strings.Contains(msg, "done") ||
		!strings.Contains(msg, "10 minutes") || !strings.Contains(msg, "the person's decision") || strings.Contains(msg, "it was deleted") {
		t.Fatalf("within the grace: %d POSTs, want the repeat that says the operation is done: %s", api.posts, text)
	}
	now = now.Add(madeListGrace)
	if m, text, _ := callNamed(t, r, "dns_zone_create", args); m["repeat_of"] != nil || api.posts != 2 {
		t.Errorf("past the grace, done and not listed: %d POSTs, want a new create: %s", api.posts, text)
	}
}

// A delete the platform took and finished takes the file of the create
// going on away at once: the repeat of the create is planned as any call
// (A19-8).
func TestMade_ADeleteOfTheResourceTakesItsFileAway(t *testing.T) {
	api := &dnsWaitAPI{}
	r := waitingRegistry(t, api)
	args := map[string]any{"name": "evo-z", "domain": "example.com.", "confirmed": true}
	callNamed(t, r, "dns_zone_create", args)
	if n := madeFiles(t, r); n != 1 {
		t.Fatalf("no file of the create going on: %d", n)
	}
	api.mu.Lock()
	api.listed = true
	api.mu.Unlock()
	m, text, _ := callNamed(t, r, "dns_zone_delete", map[string]any{"id": "00000000-0000-4000-8000-0000000000f1", "confirmed": true})
	if m["status"] != "deleted" {
		t.Fatalf("the delete: %s", text)
	}
	if n := madeFiles(t, r); n != 0 {
		t.Errorf("the file of the create of a deleted zone stays: %d", n)
	}
}

// A delete that answers already_deleted — a read found nothing — within
// madeListGrace of the create is no word that the resource is gone: the
// list lags, and the platform may still be making it. The file stays, and
// the same create with consent sends no second POST — a second paid create
// (A19-47; «Риск» on main 714cf7c8). Past the grace the empty read is an
// answer, and the delete takes the file away.
func TestMade_ARepeatAfterAlreadyDeletedInTheGraceSendsNoSecondCreate(t *testing.T) {
	api := &dnsWaitAPI{}
	r := waitingRegistry(t, api)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = time.Now })
	args := map[string]any{"name": "evo-z", "domain": "example.com.", "confirmed": true}
	del := map[string]any{"id": "00000000-0000-4000-8000-0000000000f1", "confirmed": true}
	callNamed(t, r, "dns_zone_create", args)
	if api.posts != 1 || madeFiles(t, r) != 1 {
		t.Fatalf("the first call: %d POSTs, %d files", api.posts, madeFiles(t, r))
	}
	if m, text, _ := callNamed(t, r, "dns_zone_delete", del); m["status"] != "already_deleted" {
		t.Fatalf("the world no longer answers the delete already_deleted: %s", text)
	}
	if n := madeFiles(t, r); n != 1 {
		t.Errorf("already_deleted within the grace took the file of the create away: %d files", n)
	}
	now = now.Add(time.Minute)
	m, text, _ := callNamed(t, r, "dns_zone_create", args)
	if api.posts != 1 || m["repeat_of"] == nil {
		t.Errorf("the repeat after already_deleted within the grace: %d POSTs, want 1 and repeat_of: %s", api.posts, text)
	}
	now = now.Add(madeListGrace)
	callNamed(t, r, "dns_zone_delete", del)
	if n := madeFiles(t, r); n != 0 {
		t.Errorf("already_deleted past the grace keeps the file: %d files", n)
	}
}

// A list that does not answer past the grace keeps the file: a done
// operation is no word that the zone is gone, and the repeat sends no
// second create («Риск», conclusion 405, У1).
func TestDNSZoneCreate_ARepeatWhoseListFailsSendsNoSecondCreate(t *testing.T) {
	api := &dnsWaitAPI{done: true}
	r := waitingRegistry(t, api)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = time.Now })
	args := map[string]any{"name": "evo-z", "domain": "example.com.", "confirmed": true}
	callNamed(t, r, "dns_zone_create", args)
	api.mu.Lock()
	api.listFails = true
	api.mu.Unlock()
	now = now.Add(madeListGrace * 2)
	if _, text, _ := callNamed(t, r, "dns_zone_create", args); api.posts != 1 || madeFiles(t, r) != 1 {
		t.Errorf("the list failed past the grace: %d POSTs, %d files, %s", api.posts, madeFiles(t, r), text)
	}
}

// forgetMadeOf takes away only the file of the resource a delete answered
// gone: not on a delete that failed, not the files of other ids, and no
// file whose name is not of the form madeKey makes («Риск», conclusion
// 405, У3).
func TestMade_ForgetTakesOnlyTheFileOfTheDeletedResource(t *testing.T) {
	const gone, other = "00000000-0000-4000-8000-0000000000f1", "00000000-0000-4000-8000-0000000000f2"
	for _, c := range []struct {
		name, answer string
		left         []string
	}{
		{"deleted", `{"status":"deleted"}`, []string{"odd", "other"}},
		{"already deleted", `{"status":"already_deleted"}`, []string{"odd", "other"}},
		{"the delete failed", `{"status":"error","reason":"API_ERROR","error":"x"}`, []string{"gone", "odd", "other"}},
		{"the delete goes on", `{"status":"deleting"}`, []string{"gone", "odd", "other"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			dir := MadeDir(root)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"gone":  madeKey("dns_zone_create", nil, "a", ""),
				"other": madeKey("dns_zone_create", nil, "b", ""),
				// Of the id deleted, but not a name madeKey makes.
				"odd": "zone.json",
			}
			ids := map[string]string{"gone": gone, "other": other, "odd": gone}
			for k, f := range files {
				b, _ := json.Marshal(madeCall{Tool: "dns_zone_create", ID: ids[k], Next: "dns_operation"})
				if err := os.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			forgetMadeOf(root, "dns_zone_delete", map[string]any{"id": gone, "confirmed": true}, c.answer)
			var left []string
			for k, f := range files {
				if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
					left = append(left, k)
				}
			}
			slices.Sort(left)
			if !slices.Equal(left, c.left) {
				t.Errorf("files left %v, want %v", left, c.left)
			}
		})
	}
}
