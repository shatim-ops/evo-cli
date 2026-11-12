// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
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
var lateRefusals = map[string]string{}

// alreadyAsks are the repeats of a change the world has (consentWorld.
// alreadyFor) known to ask for consent or to write: "tool: world: <name>"
// → why, and what closes it. It only shrinks. Closes by 2.0 (owner's
// decision 06.11, variant Г: not asking is a breaking change — exit 3 → 0
// — and waits for the major; release plan 1.8).
var alreadyAsks = map[string]string{
	"open_ports: world: ports open already": "every port open already: asks for consent to nothing (exit 3); in 1.8 its preview says so (variant Г), the answer without the question is breaking — 2.0",
}

// repeatIsANewChange are the changes with no repeat that is a success:
// every send is a change of its own (a reboot, a new version), so there is
// no "already" to answer (D99, П3). The value is the words of the tool's
// description that say so: the agent learns it there, before the call.
var repeatIsANewChange = map[string]string{}

// repeatUndeclared are the changes shipped before the invariant whose
// repeat nothing declares: no world has the change already, and no
// declaration says a repeat is a new change. "tool" → what a repeat does
// as far as the code shows, for the session that declares it. It only
// shrinks, and a new tool does not join it: a new change comes with its
// world (consentWorld.alreadyFor) or its line of repeatIsANewChange.
// Closes by 1.9: each line is a world or a declaration of «разработка»
// (release plan 1.9, «Инварианты до новых изменяющих операций», Н1).
var repeatUndeclared = map[string]string{
	"ca_start":           "a start of a running container app: no world",
	"ca_stop":            "a stop of a stopped container app: no world",
	"cert_renew":         "a renewal of a certificate being renewed: no world; or every send is a renewal",
	"deploy":             "a deploy over its own record: no world of consent has it",
	"deploy_apply":       "an apply of a plan applied: no world",
	"disk_resize":        "a resize to the size the disk has: no world («disk larger than requested» is the shrink)",
	"k8s_add_addon":      "an addon the cluster has: no world",
	"k8s_apply":          "the repeat is the cluster's (kubectl apply): not declared",
	"k8s_helm":           "the repeat is the cluster's (helm): not declared",
	"k8s_kubectl":        "the repeat is the cluster's (kubectl): not declared",
	"pg_db_create":       "a database the cluster has: no world, and no check of the name taken",
	"pg_user_create":     "a user the cluster has: no world, and no check of the name taken",
	"s3_bucket_create":   "a bucket the profile has: no world, and no check of the name taken",
	"s3_put":             "an object the bucket has: the repeat overwrites — not declared",
	"secret_add_version": "every send is a new version: not declared",
	"stack_apply":        "an apply of a stack applied: the repeat is the job's (stack_status), no world",
	"stack_destroy":      "a destroy of a stack destroyed answers already_destroyed: no world",
}

// TestConsent_EveryChangeDeclaresItsRepeat keeps the repeat of every
// change declared (CLAUDE.md §1: a repeat is safe; owner's decision 11.11,
// Н1). A tool that changes the cloud is of one of the classes:
//
//   - a delete — the registry declares what it deletes, and a repeat
//     answers already_deleted (D26; the contract map's "deletes");
//   - a create whose name is checked against what is there (axis 1; the
//     contract map's "taken");
//   - a change of a state — a world of consentWorlds has the change
//     already (alreadyFor), and TestConsent_RefusalWithoutWriteComesBefore
//     TheQuestion calls the tool in it: no question, no write, a status
//     already_<outcome>;
//   - a change every send of which is new (repeatIsANewChange), said in
//     the tool's description.
//
// The classes of the registry are read from docs/internal/contract-map.json,
// which TestContractMap keeps equal to the declarations.
func TestConsent_EveryChangeDeclaresItsRepeat(t *testing.T) {
	var declared map[string]struct {
		Taken   string          `json:"taken"`
		Deletes json.RawMessage `json:"deletes"`
	}
	if err := json.Unmarshal(mustReadRepo(t, "docs/internal/contract-map.json"), &declared); err != nil {
		t.Fatalf("docs/internal/contract-map.json: %v", err)
	}
	worlds := map[string][]string{}
	for name, w := range consentWorlds {
		for _, tool := range w.alreadyFor {
			worlds[tool] = append(worlds[tool], name)
		}
	}
	byName := map[string]tools.Tool{}
	for _, tl := range newRegistry().Tools() {
		byName[tl.Name] = tl
		if !tl.Mutating {
			continue
		}
		var classes []string
		if d, ok := declared[tl.Name]; ok {
			if len(d.Deletes) > 0 {
				classes = append(classes, "a delete (deleteTargets)")
			}
			if d.Taken != "" {
				classes = append(classes, "a create with its name checked (taken)")
			}
		} else {
			t.Errorf("%s changes the cloud and is not in docs/internal/contract-map.json: regenerate it (go test ./internal/tools -run TestContractMap -update-contract-map)", tl.Name)
		}
		if len(worlds[tl.Name]) > 0 {
			classes = append(classes, "a change a world already has (alreadyFor)")
			// The world counts only if the test of the worlds calls the
			// tool in it: it skips a tool without arguments of parity.
			if base, ok := parityArgs[tl.Name]; !ok || !tools.NeedsConsent(tl, base) {
				t.Errorf("%s: the world %q has its change already, but the tool has no call that asks for consent in parityArgs — the repeat is never called", tl.Name, worlds[tl.Name][0])
			}
		}
		if words := repeatIsANewChange[tl.Name]; words != "" {
			classes = append(classes, "a change every send of which is new (repeatIsANewChange)")
			if !strings.Contains(tl.Description, words) {
				t.Errorf("%s: repeatIsANewChange quotes %q, and the description does not say it — the agent learns from the description that a repeat is a new change", tl.Name, words)
			}
		}
		pending := repeatUndeclared[tl.Name] != ""
		switch {
		case len(classes) == 0 && !pending:
			t.Errorf("%s changes the cloud and does not declare its repeat: add a world of consentWorlds that has the change already (alreadyFor) — the repeat answers already_<outcome> without the question and without a write — or, if every send is a change of its own, a line of repeatIsANewChange with the words of the description that say so (D99, П3)", tl.Name)
		case len(classes) > 1 && repeatIsANewChange[tl.Name] != "":
			// A delete or a create may have a world of its repeat too
			// (close_ports); a change every send of which is new has no
			// repeat that is a success, so no other class.
			t.Errorf("%s declares that every send is a new change, and a repeat that is not one (%s)", tl.Name, strings.Join(classes, "; "))
		case len(classes) > 0 && pending:
			t.Errorf("repeatUndeclared names %s, which now is %s: remove the line (the list only shrinks)", tl.Name, classes[0])
		}
	}
	for _, table := range []struct {
		name  string
		tools map[string]string
	}{{"repeatIsANewChange", repeatIsANewChange}, {"repeatUndeclared", repeatUndeclared}} {
		for name := range table.tools {
			if tl, ok := byName[name]; !ok || !tl.Mutating {
				t.Errorf("%s names %s, which is not a tool that changes the cloud: remove the line", table.name, name)
			}
		}
	}
	for tool, names := range worlds {
		if tl, ok := byName[tool]; !ok || !tl.Mutating {
			t.Errorf("the world %q has the change of %s already, which is not a tool that changes the cloud", names[0], tool)
		}
	}
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
	// alreadyFor names the tools whose change the world already has (the
	// disk attached to this VM, the address on it): a repeat answers with
	// that, without the question and without a write (D26, D81; agent run
	// of 1.7.0, № 6: a repeated disk_attach asked, then failed with 422).
	alreadyFor []string
}

var consentWorlds = map[string]consentWorld{
	// The profile has no S3 keys (agent runs of Grok on 1.6.0, F5a): the
	// S3 tools know it from the configuration, before the question; the
	// write would fail on the client, without a request.
	"no S3 keys": {env: map[string]string{"CLOUDRU_S3_ACCESS_KEY": "", "CLOUDRU_S3_SECRET_KEY": ""},
		refusedBy: []string{"s3_bucket_create", "s3_put", "s3_rm", "s3_bucket_delete"}, names: "S3"},
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
	// The flavor of the configuration is in the catalogue, but not in the
	// zone of the call (agent run C2 on rc.2, Ц-12; D67): provision and
	// deploy read the flavor's zones for the preview, so they know the
	// call fails.
	"flavor of another zone": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return otherZoneFlavorCloud{c} },
		env: map[string]string{"CLOUDRU_FLAVOR": "lowcost10-2-4"}, refusedBy: []string{"provision", "deploy"}},
	// No flavor is set, and no flavor of the catalogue is in the zone of
	// the call: evo's own choice has nothing to pick, and provision and
	// deploy refuse before the question (D67; Fable, FB160-22).
	"no flavor of the zone": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return otherZoneFlavorCloud{c} },
		env: map[string]string{"CLOUDRU_FLAVOR": ""}, refusedBy: []string{"provision", "deploy"}},
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
		// Both disks of the parity world (disk_detach takes the attached
		// one, parityAttachedDisk) are changing.
		disk := func(id string) string { return `{"id":"` + id + `","name":"data","size":20,"state":"extending"}` }
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/disks/"):
			return consentJSON(http.StatusOK, disk(strings.TrimPrefix(r.URL.Path, "/api/v1/disks/")))
		case r.URL.Path == "/api/v1/disks":
			return consentJSON(http.StatusOK, `{"items":[`+disk(parityVM)+`,`+disk(parityAttachedDisk)+`],"total":2}`)
		}
		return nil
	}, refusedBy: []string{"disk_attach", "disk_detach", "disk_resize"}, names: "extending"},
	// The network of subnet_create overlaps a subnet of the VPC (the
	// spec's 409): subnet_create reads the project's subnets before the
	// question and refuses it.
	"subnet overlaps the VPC's": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return overlapSubnetCloud{c} },
		refusedBy: []string{"subnet_create"}},
	// Resources are still in the subnet (can_delete false; the spec's
	// 405 on DELETE): subnet_delete reads it before the question.
	"subnet with resources": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" || !strings.HasPrefix(r.URL.Path, "/api/v1/subnets/") {
			return nil
		}
		return consentJSON(http.StatusOK, `{"id":"`+parityVM+`","name":"sub","state":"created","can_delete":false}`)
	}, refusedBy: []string{"subnet_delete"}},
	// The disk is the boot disk of a VM (final of the Grok wave on rc.1,
	// the cleanup of the harness 18:38: 422 primary_disk_not_deleted after
	// consent, D59): disk_delete reads the disk for D26 before the
	// question, and its vms name the VM, so it refuses before it and names
	// the VM to remove first.
	"disk in use by a VM": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" {
			return nil
		}
		disk := `{"id":"` + parityVM + `","name":"boot","size":20,"state":"in_use","bootable":true,` +
			`"vms":[{"id":"` + consentOtherID + `","name":"web-in-use"}]}`
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/disks/"):
			return consentJSON(http.StatusOK, disk)
		case r.URL.Path == "/api/v1/disks":
			return consentJSON(http.StatusOK, `{"items":[`+disk+`],"total":1}`)
		}
		return nil
	}, refusedBy: []string{"disk_delete"}, names: "web-in-use"},
	// The change is there already (release plan 1.8, «повтор изменения,
	// которое уже есть, — не вопрос»; #902): the disk is attached to the
	// VM of disk_attach; no VM has the disk of disk_detach; the address of
	// fip_attach is on its VM.
	"disk attached to this VM": {answer: consentDiskAnswer(`"state":"in_use","vms":[{"id":"` + parityVM + `","name":"web"}]`),
		alreadyFor: []string{"disk_attach"}},
	"disk attached to no VM": {answer: consentDiskAnswer(`"state":"available","vms":[]`),
		alreadyFor: []string{"disk_detach"}},
	"floating IP on this VM": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return ownFIPCloud{c} },
		alreadyFor: []string{"fip_attach"}},
	// The group of open_ports allows its ports already; the group of
	// close_ports has no rule of its port.
	"ports open already": {answer: consentRulesAnswer(`{"id":"` + parityNIC + `","direction":"ingress","ip_protocol":"tcp","port_range":"80:80","remote_ip_prefix":"0.0.0.0/0"},` +
		`{"id":"` + consentOtherID + `","direction":"ingress","ip_protocol":"tcp","port_range":"443:443","remote_ip_prefix":"0.0.0.0/0"}`),
		alreadyFor: []string{"open_ports"}},
	"ports closed already": {answer: consentRulesAnswer(``), alreadyFor: []string{"close_ports"}},
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

// otherZoneFlavorCloud has the flavor of the call only in a zone of no
// call of the world.
type otherZoneFlavorCloud struct{ provision.CloudAPI }

func (otherZoneFlavorCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: consentOtherID, Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{},
		Zones: []provision.FlavorZone{{ID: "00000000-0000-4000-8000-0000000000b9", Name: "ru.AZ-9", Enabled: true}}}}, nil
}

// consentOtherID is a resource of the world the call did not make.
const consentOtherID = "00000000-0000-4000-8000-0000000000b8"

// consentDiskAnswer answers every disk of the compute API with the fields
// of the variant: a read of one, and the list.
func consentDiskAnswer(fields string) func(r *http.Request) *http.Response {
	return func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" {
			return nil
		}
		disk := func(id string) string { return `{"id":"` + id + `","name":"data","size":20,` + fields + `}` }
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/disks/"):
			return consentJSON(http.StatusOK, disk(strings.TrimPrefix(r.URL.Path, "/api/v1/disks/")))
		case r.URL.Path == "/api/v1/disks":
			return consentJSON(http.StatusOK, `{"items":[`+disk(parityVM)+`,`+disk(parityAttachedDisk)+`],"total":2}`)
		}
		return nil
	}
}

// consentRulesAnswer answers the rules of every security group with items.
func consentRulesAnswer(items string) func(r *http.Request) *http.Response {
	return func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" ||
			!strings.HasPrefix(r.URL.Path, "/api/v1/security-groups/") || !strings.Contains(r.URL.Path, "/rules") {
			return nil
		}
		return consentJSON(http.StatusOK, `{"items":[`+items+`]}`)
	}
}

// ownFIPCloud has the address of fip_attach on the VM of the call.
type ownFIPCloud struct{ provision.CloudAPI }

func (ownFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: parityVM, IPAddress: "203.0.113.10", State: "in_use",
		VM: []byte(`{"id":"` + parityVM + `","name":"web"}`)}}, nil
}

// busyFIPCloud has the address of fip_attach in use by another VM.
type busyFIPCloud struct{ provision.CloudAPI }

func (busyFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: parityVM, IPAddress: "203.0.113.10", State: "in_use",
		VM: []byte(`{"id":"00000000-0000-4000-8000-0000000000b7","name":"other"}`)}}, nil
}

func (noSubnetCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return nil, nil
}

// overlapSubnetCloud has, next to the world's subnets, one of the default
// VPC whose network is subnet_create's (parityArgs).
type overlapSubnetCloud struct{ provision.CloudAPI }

func (c overlapSubnetCloud) ListSubnets(ctx context.Context, projectID string) ([]*provision.Subnet, error) {
	subs, err := c.CloudAPI.ListSubnets(ctx, projectID)
	return append(subs, &provision.Subnet{ID: consentOtherID, Name: "other", SubnetAddress: "10.10.0.0/16", VPCID: parityVM}), err
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
	// already: the repeats of a change the world has that ask or write.
	already := map[string]string{}
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
			if slices.Contains(c.world.alreadyFor, tl.Name) {
				// A repeat of a change the world has: the answer of the
				// repeat, the same with confirmed, and no write either way.
				before, beforeErr, _ := call(tl, c, false)
				after, afterErr, writes := call(tl, c, true)
				switch {
				case before["status"] == "confirmation_required":
					already[key] = "asks for consent to a change the world already has: " + clipLine(fmt.Sprint(before))
				case beforeErr || afterErr:
					already[key] = fmt.Sprintf("refuses a change the world already has: %s / %s", clipLine(fmt.Sprint(before)), clipLine(fmt.Sprint(after)))
				case writes > 0:
					already[key] = fmt.Sprintf("writes %d time(s) for a change the world already has", writes)
				case before["status"] != after["status"]:
					already[key] = fmt.Sprintf("answers %v without confirmed, %v with it", before["status"], after["status"])
				case !strings.HasPrefix(fmt.Sprint(before["status"]), "already_"):
					// One form for every repeat (D26's already_deleted, D81,
					// D99 П3): the agent tells a repeat from a change by the
					// status, without reading the text.
					already[key] = fmt.Sprintf("answers a change the world already has with status %v, not already_<outcome>", before["status"])
				}
				continue
			}
			if slices.Contains(c.world.refusedBy, tl.Name) {
				before, _, _ := call(tl, c, false)
				msg := fmt.Sprint(before["error"], " ", before["hint"])
				switch {
				case before["status"] == "confirmation_required":
					found[key] = "asks for consent in a world it can read as refusing the write"
				case c.world.names != "" && !strings.Contains(msg, c.world.names):
					found[key] = fmt.Sprintf("refuses without naming %q: %s", c.world.names, clipLine(msg))
				default:
					// The call with confirmed=true in the same world: the
					// same refusal, and no write (Fable, FB160-15 — MCP
					// deploy went past its preview's refusal).
					after, _, writes := call(tl, c, true)
					if writes > 0 || after["reason"] != before["reason"] {
						found[key+" (confirmed)"] = fmt.Sprintf("refuses with %v before the question, with %v and %d writes after consent", before["reason"], after["reason"], writes)
					}
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
	for k, why := range already {
		if alreadyAsks[k] == "" {
			t.Errorf("%s: %s — a repeat of a change that is there answers it, without the question and without a write (D26, D81)", k, why)
		}
	}
	for k := range alreadyAsks {
		if already[k] == "" {
			t.Errorf("alreadyAsks names %s, which no longer asks or writes: remove the line (the list only shrinks)", k)
		}
	}
	for k := range lateRefusals {
		if found[k] == "" {
			t.Errorf("lateRefusals names %s, which no longer fails that way: remove the line", k)
		}
	}
}
