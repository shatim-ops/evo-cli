// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// lateRefusals are the calls known to ask for consent and then fail
// without a write (G13-3, class K1), or to answer a write the world refused
// as a success (G130-15): "tool: case" → the work item of the release plan
// that fixes it (W3…W6; the decisions of 1.3.1, D39…D42, with their PR).
// The list only shrinks: a case not here fails the test, and so does a line
// whose call no longer fails that way.
var lateRefusals = map[string]string{
	"disk_attach: world: disk in a transient state": "release plan 1.6, «Изменение диска в переходном состоянии» (#521): «разработка»",
	"disk_detach: world: disk in a transient state": "release plan 1.6, «Изменение диска в переходном состоянии» (#521): «разработка»",
	"disk_resize: world: disk in a transient state": "release plan 1.6, «Изменение диска в переходном состоянии» (#521): «разработка»",
}

// jobStarts answer a refused write as a success by design: the call starts
// a job, and the step's refusal is the job's state, not the call's answer.
var jobStarts = map[string]string{
	"stack_apply": "starts the job: the refusal of a step is stack_status's answer (G130-04)",
}

// consentAbsentID is an id no resource of the world has: every API answers
// it 404.
const consentAbsentID = "00000000-0000-4000-8000-0000000000ad"

// consentWorld is a variant of the JSON world that breaks one precondition
// of a write; nil fields leave the world as it is. The bucket of the world
// already holds an object (s3Parity), so s3_bucket_delete always runs on a
// non-empty bucket and needs no variant.
type consentWorld struct {
	cloud  func(provision.CloudAPI) provision.CloudAPI
	answer func(r *http.Request) *http.Response
	// env sets variables of the configuration for the call: "" unsets the
	// default an argument falls back to (configDefaultWorlds).
	env map[string]string
	// refusedBy names the tools that can see the broken precondition by a
	// read and must refuse before the question, though the platform's own
	// refusal comes only with the write (the test world refuses every
	// write, so a refusal after consent alone does not show them).
	refusedBy []string
	// names is what the refusal of refusedBy says (the state of the
	// resource), so that the agent knows to wait, not to fix an argument.
	names string
}

var consentWorlds = map[string]consentWorld{
	// provision, deploy and the creates that pick the zone's subnet.
	"zone without subnet": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return noSubnetCloud{c} }},
	// disk_resize asks for 50 GB: the disk has 100, a shrink the API
	// refuses. No tool reads the size before the write yet: the variant
	// keeps the check of W5 before the question once it is there.
	// The address is attached to another VM: fip_attach reads it before
	// the question (Fable, delta audit 1.3.0, FB130-1).
	"floating IP in use": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return busyFIPCloud{c} }, refusedBy: []string{"fip_attach"}},
	// The image of the configuration is not in the catalogue: provision
	// and deploy read the catalogue for the preview, so they know the call
	// fails (G130-02, D39).
	"image not in the catalogue": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return noImageCloud{c} },
		refusedBy: []string{"provision", "deploy"}},
	// The flavor of the configuration is not in the catalogue: provision
	// and deploy read the catalogue for the preview, so they know the call
	// fails, as of the image (#469, #470, D50). The flavor is named: auto
	// (the world's default) takes the catalogue's own and fails nothing.
	"flavor not in the catalogue": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return noFlavorCloud{c} },
		env: map[string]string{"CLOUDRU_FLAVOR": "lowcost10-2-4"}, refusedBy: []string{"provision", "deploy"}},
	// The path of secret_create is taken: secret_create reads the list
	// before the question and refuses the taken path (D49; was the
	// create's 4xx after it, G130-15, D42).
	"secret path taken": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "secretmanager.api.cloud.ru" || r.URL.Path != "/v2/secret" {
			return nil
		}
		return consentJSON(http.StatusOK, `{"secrets":[{"id":"`+consentOtherID+`","path":"app/db"}]}`)
	}, refusedBy: []string{"secret_create"}},
	// The disk is still changing (cloud-e2e of the 1.5 candidate, run 142,
	// #521: disk_detach a second after disk_resize, 422
	// disk_can_not_be_detached_from_current_state after consent): the
	// tools that change a disk read its state before the question and
	// refuse with it.
	"disk in a transient state": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" {
			return nil
		}
		disk := `{"id":"` + parityVM + `","name":"data","size":20,"state":"extending"}`
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/disks/"):
			return consentJSON(http.StatusOK, disk)
		case r.URL.Path == "/api/v1/disks":
			return consentJSON(http.StatusOK, `{"items":[`+disk+`],"total":1}`)
		}
		return nil
	}, refusedBy: []string{"disk_attach", "disk_detach", "disk_resize"}, names: "extending"},
	"disk larger than requested": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" || !strings.HasPrefix(r.URL.Path, "/api/v1/disks/") {
			return nil
		}
		return consentJSON(http.StatusOK, `{"id":"`+parityVM+`","name":"old","size":100,"state":"available"}`)
	}},
}

// configDefaults are the variables of the configuration (config.envMapping)
// a write falls back to when its call names no value: the image, the zone,
// the flavor and the size of the disk of a VM. Each is a variant of the
// world in which it is not set (G140-1, G140-2: deploy over MCP without an
// image or a zone asked for consent, then failed on "no boot image").
var configDefaults = []string{"CLOUDRU_IMAGE_ID", "CLOUDRU_REGION", "CLOUDRU_FLAVOR", "CLOUDRU_DISK_SIZE"}

func init() {
	for _, v := range configDefaults {
		consentWorlds["config without "+v] = consentWorld{env: map[string]string{v: ""}}
	}
}

// emptySubstitution finds a value that was empty where a message names it
// ("no subnet for zone  in the project", G140-2): two spaces in a row or
// empty quotes.
var emptySubstitution = regexp.MustCompile(`\S  +\S|""|''|«»|\(\)`)

type noSubnetCloud struct{ provision.CloudAPI }

// noImageCloud has no image at all: the catalogue answers not found.
type noImageCloud struct{ provision.CloudAPI }

func (noImageCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return nil, nil
}

func (noImageCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}

func (noImageCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return nil, clierrors.New(clierrors.CodeNotFound, "HTTP 404: image "+id+" not found", "")
}

// noFlavorCloud has one flavor, not the configuration's: its catalogue
// answers, and the name asked for is not in it.
type noFlavorCloud struct{ provision.CloudAPI }

func (noFlavorCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: consentOtherID, Name: "other-1-1", VCPUs: 1, RAMGB: 1, Share: "1:1", GPU: new(int), Type: &provision.FlavorType{}}}, nil
}

// consentOtherID is a resource of the world the call did not make.
const consentOtherID = "00000000-0000-4000-8000-0000000000b8"

// busyFIPCloud has the address of fip_attach in use by another VM.
type busyFIPCloud struct{ provision.CloudAPI }

func (busyFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: parityVM, IPAddress: "203.0.113.10", State: "in_use",
		VM: []byte(`{"id":"00000000-0000-4000-8000-0000000000b7","name":"other"}`)}}, nil
}

func (noSubnetCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return nil, nil
}

func consentJSON(status int, body string) *http.Response {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
	return w.Result()
}

// consentCase is one call of a tool that needs consent, its arguments
// broken one way, or its world.
type consentCase struct {
	name  string
	key   string // the argument broken, "" for a variant of the world
	args  map[string]any
	world consentWorld
}

// consentCases: each integer argument of the schema as -1, 0 and 10⁹ (in a
// list too: ports), each id of parityArgs as an id the world has not, each
// string with a "format" as "bad", and the base arguments in each variant
// of the world.
func consentCases(tl tools.Tool, base map[string]any) []consentCase {
	var out []consentCase
	with := func(name, key string, v any) {
		a := copyArgs(base)
		a[key] = v
		out = append(out, consentCase{name: fmt.Sprintf("%s=%v", key, name), key: key, args: a})
	}
	props, _ := tl.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "confirmed" {
			continue
		}
		p, _ := props[k].(map[string]any)
		typ, _ := p["type"].(string)
		if typ == "array" {
			if items, _ := p["items"].(map[string]any); items != nil {
				typ = "[]" + fmt.Sprint(items["type"])
			}
		}
		for _, n := range []float64{-1, 0, 1e9} {
			switch typ {
			case "integer", "number":
				with(fmt.Sprintf("%g", n), k, n)
			case "[]integer", "[]number":
				with(fmt.Sprintf("[%g]", n), k, []any{n})
			}
		}
		if typ != "string" {
			continue
		}
		if v, _ := base[k].(string); looksParityID(v) {
			with("absent id", k, consentAbsentID)
		} else if absent := consentAbsentEvoID(v); absent != "" {
			with("absent id", k, absent)
		}
		if _, ok := p["format"]; ok {
			with("bad", k, "bad")
		}
	}
	names := make([]string, 0, len(consentWorlds))
	for n := range consentWorlds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, consentCase{name: "world: " + n, args: copyArgs(base), world: consentWorlds[n]})
	}
	return out
}

// consentAbsentEvoID is an id of evo's own (a stack job stk-<10 hex>, a
// plan plan-<12 hex>) of the same form as v that no job or plan has, or ""
// when v is none: the job of stack_apply is read from the local store, not
// the cloud, and an absent one is as sure a refusal as a 404 (1.4.0: it
// asked for consent to a billable apply, then answered NOT_FOUND).
func consentAbsentEvoID(v string) string {
	switch {
	case strings.HasPrefix(v, "stk-"):
		return "stk-00000000ad"
	case strings.HasPrefix(v, "plan-"):
		return "plan-0000000000ad"
	}
	return ""
}

// looksParityID: an id of the world (parityVM, the zone, the cluster).
func looksParityID(v string) bool {
	return uuidInPath.MatchString(v) || v == addonClusterID
}

// CLAUDE.md §4: a call that is bound to fail is refused before the
// question of consent. In the JSON world every tool that needs consent is
// called with confirmed=true and each of its consentCases while the world
// refuses every write; a call that fails without trying one failed on a
// check the preview could have made, so the same call without confirmed
// must give that refusal (the same reason), not confirmation_required.
// A call whose write the world refused must not answer a success either:
// the resource is not there, or the one found is not the call's (G130-15),
// unless the call only starts a job (jobStarts). It sees only what the
// world models: each rule of the platform is a variant of consentWorlds,
// taken from the register of discrepancies and the decisions (D39, D42 —
// the first rules of the world of 4xx; G13-3, K1; release plan 1.3, W8).
func TestConsent_RefusalWithoutWriteComesBeforeTheQuestion(t *testing.T) {
	w, fake := withJSONWorld(t)
	srv := newROServers(t)
	var (
		mu       sync.Mutex
		variant  consentWorld
		refuse   bool
		attempts int
	)
	world := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		v, refusing := variant, refuse
		write := r.Method != http.MethodGet && r.Method != http.MethodHead &&
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") // the token of a kubeconfig
		if write && refusing {
			attempts++
		}
		mu.Unlock()
		if write && refusing {
			return consentJSON(http.StatusForbidden, `{"code":"FORBIDDEN","message":"the test refuses every write"}`), nil
		}
		if strings.Contains(r.URL.Path, consentAbsentID) || strings.Contains(r.URL.RawQuery, consentAbsentID) {
			return consentJSON(http.StatusNotFound, `{"code":"NOT_FOUND","message":"not found"}`), nil
		}
		if v.answer != nil {
			if resp := v.answer(r); resp != nil {
				return resp, nil
			}
		}
		return world(r)
	}

	call := func(tl tools.Tool, c consentCase, confirmed bool) (map[string]any, bool, int) {
		a := copyArgs(c.args)
		done := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, teardown := setup(t, w)
			for k, v := range extra {
				if k != c.key {
					a[k] = v
				}
			}
			done = teardown
		}
		// The variant's cloud wraps the tool's own world (paritySetup),
		// so what the variant breaks is what the tool reads.
		if c.world.cloud != nil {
			inner := toolEngine
			toolEngine = func(o map[string]any) (*engine.Engine, error) {
				e, err := inner(o)
				if err == nil {
					e.Cloud = c.world.cloud(e.Cloud)
				}
				return e, err
			}
			setupDone := done
			done = func() { toolEngine = inner; setupDone() }
		}
		for k, v := range c.world.env {
			old, had := os.LookupEnv(k)
			_ = os.Setenv(k, v)
			envDone := done
			done = func() {
				if had {
					_ = os.Setenv(k, old)
				} else {
					_ = os.Unsetenv(k)
				}
				envDone()
			}
		}
		if confirmed {
			a["confirmed"] = true
		}
		fake.reset()
		w.rt.takeWrites()
		mu.Lock()
		variant, refuse, attempts = c.world, confirmed, 0
		mu.Unlock()
		ans := first(srv.stdio(t, srv.plain, "tools/call", map[string]any{"name": tl.Name, "arguments": a}))
		mu.Lock()
		n := attempts
		variant, refuse = consentWorld{}, false
		mu.Unlock()
		done()
		if c.key != "" {
			// A broken argument reaches paths the fixtures need not
			// answer (a version -1 of a certificate): withParityWorld
			// checks the requests of the base arguments only.
			w.rt.takeUnknown()
		}
		return parseObject(ans.text), ans.isErr, n
	}

	found := map[string]string{}
	// A refusal names what is missing, not an empty value in its place.
	emptyIn := func(key string, ans map[string]any) {
		for _, f := range []string{"error", "hint"} {
			if msg, _ := ans[f].(string); emptySubstitution.MatchString(msg) {
				found[key+": empty value in "+f] = clipLine(msg)
			}
		}
	}
	for _, tl := range newRegistry().Tools() {
		base, ok := parityArgs[tl.Name]
		if !ok || !tools.NeedsConsent(tl, base) {
			continue
		}
		for _, c := range consentCases(tl, base) {
			key := tl.Name + ": " + c.name
			if slices.Contains(c.world.refusedBy, tl.Name) {
				before, _, _ := call(tl, c, false)
				msg := fmt.Sprint(before["error"], " ", before["hint"])
				switch {
				case before["status"] == "confirmation_required":
					found[key] = "asks for consent in a world it can read as refusing the write"
				case c.world.names != "" && !strings.Contains(msg, c.world.names):
					found[key] = fmt.Sprintf("refuses without naming %q: %s", c.world.names, clipLine(msg))
				}
				continue
			}
			after, failed, writes := call(tl, c, true)
			emptyIn(key+" (after consent)", after)
			if writes > 0 && !failed && jobStarts[tl.Name] == "" {
				// The world refused the write: a success is a resource
				// that is not there, or not the call's (K1 after the
				// question; G130-15).
				found[key] = fmt.Sprintf("the world refused its write (403), the answer is a success: %s", clipLine(fmt.Sprint(after)))
				continue
			}
			if !failed || writes > 0 {
				continue
			}
			reason, _ := after["reason"].(string)
			before, _, _ := call(tl, c, false)
			emptyIn(key, before)
			switch {
			case before["status"] == "confirmation_required":
				found[key] = fmt.Sprintf("asks for consent, then fails with %s: %s", reason, clipLine(fmt.Sprint(after["error"])))
			case before["reason"] != reason:
				found[key] = fmt.Sprintf("refuses with %v before the question, with %s after it", before["reason"], reason)
			}
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if lateRefusals[k] == "" {
			t.Errorf("%s: %s — check it before the question, or, while the plan moves it, add it to lateRefusals with the work item", k, found[k])
		}
	}
	for k := range lateRefusals {
		if found[k] == "" {
			t.Errorf("lateRefusals names %s, which no longer fails that way: remove the line", k)
		}
	}
}
