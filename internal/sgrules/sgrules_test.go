// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// fakeCloud keeps a group's rules like the API: POST adds with an id,
// DELETE removes (404 when gone). Other CloudAPI calls panic.
type fakeCloud struct {
	provision.CloudAPI
	rules     map[string][]provision.SGRule // by group
	seq       int
	noID      bool  // create answers without an id
	listErr   error // ListSecurityGroupRules fails
	addErr    map[string]error
	deleteErr map[string]error // by rule id
	deleted   []string
}

func newFake() *fakeCloud {
	return &fakeCloud{rules: map[string][]provision.SGRule{}, addErr: map[string]error{}, deleteErr: map[string]error{}}
}

func (f *fakeCloud) ListSecurityGroupRules(_ context.Context, sg string) ([]*provision.SGRule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*provision.SGRule
	for i := range f.rules[sg] {
		r := f.rules[sg][i]
		out = append(out, &r)
	}
	return out, nil
}

func (f *fakeCloud) AddSecurityGroupRule(_ context.Context, sg, _ string, r provision.SGRule) (*provision.SGRule, error) {
	if err := f.addErr[r.PortRange]; err != nil {
		return nil, err
	}
	f.seq++
	r.ID = fmt.Sprintf("r%d", f.seq)
	f.rules[sg] = append(f.rules[sg], r)
	if f.noID {
		return &provision.SGRule{}, nil
	}
	return &r, nil
}

func (f *fakeCloud) DeleteSecurityGroupRule(_ context.Context, sg, id string) error {
	if err := f.deleteErr[id]; err != nil {
		return err
	}
	for i, r := range f.rules[sg] {
		if r.ID == id {
			f.rules[sg] = append(f.rules[sg][:i], f.rules[sg][i+1:]...)
			f.deleted = append(f.deleted, id)
			return nil
		}
	}
	return clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
}

func desc(label string) string { return "evo: port " + label }

// Only rules Open creates become the deployment's own; a port that a rule
// already opened (by anyone) is needed, but not owned.
func TestOpen_RecordsOnlyCreatedRules(t *testing.T) {
	f := newFake()
	f.rules["sg-1"] = []provision.SGRule{
		{ID: "old-443", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443"},
		{ID: "old-udp", Direction: "ingress", IPProtocol: "udp", PortRange: "80:80"},
		{ID: "dying", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", State: "deleting"},
	}
	var rec state.DeploymentRecord
	res := Open(context.Background(), f, "proj", "sg-1", []int{80, 443, 8080}, []int{53}, desc, &rec)

	if !reflect.DeepEqual(res.Opened, []string{"80", "8080", "53/udp"}) || !reflect.DeepEqual(res.AlreadyOpen, []string{"443"}) {
		t.Errorf("result %+v", res)
	}
	if rec.SecurityGroupID != "sg-1" {
		t.Errorf("sg = %q", rec.SecurityGroupID)
	}
	if !reflect.DeepEqual(rec.SecurityGroupPorts, []string{"80/tcp", "443/tcp", "8080/tcp", "53/udp"}) {
		t.Errorf("ports = %v", rec.SecurityGroupPorts)
	}
	var owned []string
	for _, r := range rec.OpenedRules {
		if r.SecurityGroupID != "sg-1" {
			t.Errorf("rule %+v", r)
		}
		owned = append(owned, r.ID+"="+r.Port)
	}
	if !reflect.DeepEqual(owned, []string{"r1=80/tcp", "r2=8080/tcp", "r3=53/udp"}) {
		t.Errorf("owned = %v", owned)
	}
}

// A re-run finds its own rules open and creates nothing twice.
func TestOpen_RerunCreatesNothing(t *testing.T) {
	f := newFake()
	var rec state.DeploymentRecord
	Open(context.Background(), f, "", "sg-1", []int{80}, nil, desc, &rec)
	res := Open(context.Background(), f, "", "sg-1", []int{80}, nil, desc, &rec)
	if len(res.Opened) != 0 || len(f.rules["sg-1"]) != 1 || len(rec.OpenedRules) != 1 || len(rec.SecurityGroupPorts) != 1 {
		t.Errorf("res %+v rules %v rec %+v", res, f.rules, rec)
	}
}

// No id in the create answer: the new rule is found in the list by what it
// is, and an older rule with the same port is never taken for it.
func TestOpen_NoIDInAnswerLooksTheRuleUp(t *testing.T) {
	f := newFake()
	f.noID = true
	f.rules["sg-1"] = []provision.SGRule{{ID: "foreign", Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", Description: "evo: port 80", State: "deleting"}}
	var rec state.DeploymentRecord
	res := Open(context.Background(), f, "", "sg-1", []int{80}, nil, desc, &rec)
	if len(rec.OpenedRules) != 1 || rec.OpenedRules[0].ID != "r1" || len(res.Untracked) != 0 {
		t.Errorf("rec %+v res %+v", rec.OpenedRules, res)
	}
}

// Without a listing from before, a looked-up rule could be someone else's:
// it is reported as untracked instead.
func TestOpen_NoIDAndNoListingIsUntracked(t *testing.T) {
	f := newFake()
	f.noID = true
	f.listErr = stderrors.New("list down")
	var rec state.DeploymentRecord
	res := Open(context.Background(), f, "", "sg-1", []int{80}, nil, desc, &rec)
	if len(rec.OpenedRules) != 0 || !reflect.DeepEqual(res.Untracked, []string{"80"}) || !reflect.DeepEqual(res.Opened, []string{"80"}) {
		t.Errorf("rec %+v res %+v", rec.OpenedRules, res)
	}
}

func TestOpen_FailureIsReported(t *testing.T) {
	f := newFake()
	f.addErr["80:80"] = stderrors.New("quota")
	var rec state.DeploymentRecord
	res := Open(context.Background(), f, "", "sg-1", []int{80, 81}, nil, desc, &rec)
	if !reflect.DeepEqual(res.Failed, []string{"80"}) || len(rec.OpenedRules) != 1 {
		t.Errorf("res %+v rec %+v", res, rec.OpenedRules)
	}
	if n := res.Note(); n != "opened ports 81; FAILED to open 80 — open them manually in the console" {
		t.Errorf("note %q", n)
	}
}

func TestOpen_NoGroupDoesNothing(t *testing.T) {
	var rec state.DeploymentRecord
	res := Open(context.Background(), nil, "", "", []int{80}, nil, desc, &rec)
	if res.Note() != "" || rec.SecurityGroupID != "" || len(rec.SecurityGroupPorts) != 0 {
		t.Errorf("res %+v rec %+v", res, rec)
	}
}

func twoDeployments() *state.File {
	return &state.File{Version: 1, Deployments: []state.DeploymentRecord{
		{ID: "a", Status: "running", SecurityGroupID: "sg-1", SecurityGroupPorts: []string{"80/tcp", "443/tcp"},
			OpenedRules: []state.SGRule{{ID: "r80", SecurityGroupID: "sg-1", Port: "80/tcp"}, {ID: "r443", SecurityGroupID: "sg-1", Port: "443/tcp"}}},
		{ID: "b", Status: "running", SecurityGroupID: "sg-1", SecurityGroupPorts: []string{"80/tcp"}},
	}}
}

// A rule another deployment of this state still needs (same group, same
// port) is kept and handed over to it; the rest are deleted.
func TestClose_KeepsRuleNeededByAnotherDeployment(t *testing.T) {
	f := newFake()
	f.rules["sg-1"] = []provision.SGRule{{ID: "r80"}, {ID: "r443"}}
	sf := twoDeployments()

	plan := Plan(sf, "a")
	if len(plan) != 2 || plan[0].KeptFor != "b" || plan[1].KeptFor != "" {
		t.Errorf("plan %+v", plan)
	}
	res := Close(context.Background(), f, sf, "a")
	if !reflect.DeepEqual(res.Closed, []string{"443"}) || len(res.Kept) != 1 || len(res.Failed) != 0 {
		t.Errorf("res %+v", res)
	}
	if !reflect.DeepEqual(f.deleted, []string{"r443"}) {
		t.Errorf("deleted %v", f.deleted)
	}
	if a := sf.FindDeployment("a"); len(a.OpenedRules) != 0 {
		t.Errorf("a keeps %v", a.OpenedRules)
	}
	if b := sf.FindDeployment("b"); len(b.OpenedRules) != 1 || b.OpenedRules[0].ID != "r80" {
		t.Errorf("b owns %v", b.OpenedRules)
	}
	Retire(sf, "a")
	if sf.FindDeployment("a") != nil {
		t.Error("a still in state")
	}
}

// Another group, another protocol, or a deployment already destroyed does
// not need the rule.
func TestClose_OtherGroupOrProtocolDoesNotKeep(t *testing.T) {
	for name, other := range map[string]state.DeploymentRecord{
		"other group":    {ID: "b", Status: "running", SecurityGroupID: "sg-2", SecurityGroupPorts: []string{"80/tcp"}},
		"other protocol": {ID: "b", Status: "running", SecurityGroupID: "sg-1", SecurityGroupPorts: []string{"80/udp"}},
		"destroyed":      {ID: "b", Status: "destroyed", SecurityGroupID: "sg-1", SecurityGroupPorts: []string{"80/tcp"}},
	} {
		f := newFake()
		f.rules["sg-1"] = []provision.SGRule{{ID: "r80"}}
		sf := &state.File{Deployments: []state.DeploymentRecord{
			{ID: "a", SecurityGroupID: "sg-1", OpenedRules: []state.SGRule{{ID: "r80", SecurityGroupID: "sg-1", Port: "80/tcp"}}},
			other,
		}}
		if res := Close(context.Background(), f, sf, "a"); !reflect.DeepEqual(res.Closed, []string{"80"}) {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

// A record from before security_group_id existed may be in any group: its
// exposed tcp ports keep the rule.
func TestClose_LegacyRecordKeepsByExposedPorts(t *testing.T) {
	f := newFake()
	sf := &state.File{Deployments: []state.DeploymentRecord{
		{ID: "a", SecurityGroupID: "sg-1", OpenedRules: []state.SGRule{{ID: "r80", SecurityGroupID: "sg-1", Port: "80/tcp"}}},
		{ID: "old", Status: "running", ExposedPorts: []int{80}},
	}}
	res := Close(context.Background(), f, sf, "a")
	if len(res.Kept) != 1 || len(f.deleted) != 0 {
		t.Errorf("res %+v deleted %v", res, f.deleted)
	}
}

// A failed delete is kept with its error for the next destroy; a rule
// already gone (404) counts as closed.
func TestClose_FailureStaysMarked(t *testing.T) {
	f := newFake()
	f.rules["sg-1"] = []provision.SGRule{{ID: "r443"}}
	f.deleteErr["r443"] = stderrors.New("API returned status 422: rule state")
	sf := twoDeployments()
	sf.Deployments = sf.Deployments[:1] // only a

	res := Close(context.Background(), f, sf, "a")
	if !reflect.DeepEqual(res.Closed, []string{"80"}) || len(res.Failed) != 1 {
		t.Errorf("res %+v", res)
	}
	a := sf.FindDeployment("a")
	if len(a.OpenedRules) != 1 || a.OpenedRules[0].ID != "r443" || a.OpenedRules[0].DeleteError == "" {
		t.Fatalf("a = %+v", a.OpenedRules)
	}

	Retire(sf, "a")
	if a := sf.FindDeployment("a"); a == nil || a.Status != "destroyed" {
		t.Fatalf("record not kept as destroyed: %+v", a)
	}
	if sf.ActiveDeployment() != nil {
		t.Error("a pending record counts as active")
	}
	if got := Pending(sf); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("pending %v", got)
	}

	// The next destroy finishes it.
	delete(f.deleteErr, "r443")
	res = Close(context.Background(), f, sf, "a")
	if !reflect.DeepEqual(res.Closed, []string{"443"}) {
		t.Errorf("retry %+v", res)
	}
	Retire(sf, "a")
	if len(sf.Deployments) != 0 || len(Pending(sf)) != 0 {
		t.Errorf("left %+v", sf.Deployments)
	}
}

// A hand-edited state with an empty rule ID sends no DELETE .../rules/ and
// is not kept for a retry that can never work.
func TestClose_EmptyIDsSendNothing(t *testing.T) {
	f := newFake()
	sf := &state.File{Deployments: []state.DeploymentRecord{{ID: "a", OpenedRules: []state.SGRule{
		{ID: "", SecurityGroupID: "sg-1", Port: "80/tcp"},
		{ID: "r1", SecurityGroupID: "", Port: "81/tcp"},
	}}}}
	f.deleteErr[""] = stderrors.New("DELETE with an empty id was sent")
	res := Close(context.Background(), f, sf, "a")
	if len(res.Failed) != 2 || len(f.deleted) != 0 {
		t.Errorf("res %+v deleted %v", res, f.deleted)
	}
	Retire(sf, "a")
	if len(sf.Deployments) != 0 {
		t.Errorf("kept %+v", sf.Deployments)
	}
}

func TestLabel(t *testing.T) {
	for key, want := range map[string]string{"80/tcp": "80", "53/udp": "53/udp"} {
		if got := Label(key); got != want {
			t.Errorf("Label(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestParsePorts(t *testing.T) {
	tcp, udp, err := ParsePorts([]string{"80", " 8080/TCP ", "53/udp", "80/tcp", ""})
	if err != nil || fmt.Sprint(tcp) != "[80 8080]" || fmt.Sprint(udp) != "[53]" {
		t.Errorf("tcp %v udp %v err %v", tcp, udp, err)
	}
	for _, bad := range []string{"http", "0", "65536", "80/sctp", "80/"} {
		if _, _, err := ParsePorts([]string{bad}); err == nil {
			t.Errorf("%q принят", bad)
		}
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*fakeCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}

// errNoPriceAPI — ответ подделок на PriceCalculation: расчёт цены не
// получен, превью и план показывают оценку по карте, как раньше.
var errNoPriceAPI = stderrors.New("price-calculation: not in this fake")
