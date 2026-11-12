// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A rule added to the group of a VM by sg_rule_create, open_ports or the
// console is not the deployment's: destroy does not delete it. The
// preview of the delete says what stays open to the internet in the VM's
// group («Риск», conclusion 146, У4) — a fact, by the same threshold as
// D105, and never an advice to delete: other VMs of a shared group use
// those rules.

func leftWorld(rules []provision.SGRule) *mockCloudAPI {
	return &mockCloudAPI{
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-1", Default: true, Rules: rules}}, nil
		},
		listInterfacesFn: func(context.Context, string) ([]*provision.Interface, error) {
			groups := []provision.InterfaceGroup{{ID: "sg-1"}}
			return []*provision.Interface{
				{ID: "if-1", VM: &provision.InterfaceVM{ID: "vm-1", Name: "web"}, SecurityGroups: groups},
				{ID: "if-2", VM: &provision.InterfaceVM{ID: "vm-2", Name: "db"}, SecurityGroups: groups},
			}, nil
		},
	}
}

func ingress(id, ports, source string) provision.SGRule {
	return provision.SGRule{ID: id, Direction: "ingress", IPProtocol: "tcp", PortRange: ports, RemoteIPPrefix: source, State: "created"}
}

func TestOpenLeft_NamesWhatStaysOpenToTheInternet(t *testing.T) {
	fromGroup := ingress("r-group", "5432:5432", "")
	fromGroup.RemoteSecurityGroup = &provision.SGRuleGroup{ID: "sg-2", Name: "db"}
	egress := ingress("r-egress", "443:443", "0.0.0.0/0")
	egress.Direction = "egress"
	going := ingress("r-going", "8443:8443", "0.0.0.0/0")
	going.State = "deleting"
	coming := ingress("r-coming", "9000:9000", "::/0")
	coming.State = "creating"
	cloud := leftWorld([]provision.SGRule{
		ingress("r-ssh", "22:22", "0.0.0.0/0"), ingress("r-half", "443:443", "128.0.0.0/1"), coming,
		ingress("r-own", "80:80", "0.0.0.0/0"), ingress("r-private", "8080:8080", "10.0.0.0/8"),
		ingress("r-nosource", "8081:8081", ""), fromGroup, egress, going,
	})
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	left := eng.OpenLeft(context.Background(), "p", "vm-1", []string{"sg-1"}, map[string]bool{"r-own": true})
	if left == nil || len(left.Groups) != 1 || left.Unread != "" {
		t.Fatalf("OpenLeft = %+v, want one group", left)
	}
	g := left.Groups[0]
	var ids []string
	for _, r := range g.Rules {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "r-ssh,r-half,r-coming" {
		t.Errorf("rules that stay: %v, want the three open to the internet that this delete does not close", ids)
	}
	if !g.Shared || g.Interfaces == nil || *g.Interfaces != 2 || g.Name != "SSH-access_ru.AZ-1" {
		t.Errorf("the group: %+v, want shared with its 2 interfaces", g)
	}
	text := strings.Join(left.Lines(), "\n")
	for _, want := range []string{"SSH-access_ru.AZ-1", "tcp 22:22", "128.0.0.0/1", "shared"} {
		if !strings.Contains(text, want) {
			t.Errorf("the lines do not say %q: %s", want, text)
		}
	}
	// A fact, not an advice: no tool that deletes a rule is named.
	for _, bad := range []string{"sg_rule_delete", "close_ports", "close-ports", "sg-rule-delete"} {
		if strings.Contains(text, bad) {
			t.Errorf("the lines advise %s: %s", bad, text)
		}
	}
	// The groups of the VM's interfaces, when no record names one.
	if left := eng.OpenLeft(context.Background(), "p", "vm-1", nil, nil); left == nil || len(left.Groups) != 1 || len(left.Groups[0].Rules) != 4 {
		t.Errorf("by the VM's interfaces: %+v, want the group with four rules (the deployment's own is not closed here)", left)
	}
	// Nothing stays: nothing is said.
	if left := eng.OpenLeft(context.Background(), "p", "vm-1", []string{"sg-1"}, map[string]bool{"r-ssh": true, "r-half": true, "r-coming": true, "r-own": true}); left != nil {
		t.Errorf("nothing stays open, OpenLeft = %+v", left)
	}
}

func TestOpenLeft_ALongListIsCutAndAFailedReadIsSaid(t *testing.T) {
	var many []provision.SGRule
	for i := 0; i < MaxOpenLeftRules+3; i++ {
		many = append(many, ingress(fmt.Sprintf("r-%d", i), fmt.Sprintf("%d:%d", 8000+i, 8000+i), "0.0.0.0/0"))
	}
	eng, _ := newTestEngine(t, leftWorld(many), newMockDeployer())
	left := eng.OpenLeft(context.Background(), "p", "vm-1", []string{"sg-1"}, nil)
	if left == nil || len(left.Groups[0].Rules) != MaxOpenLeftRules || left.Groups[0].More != 3 {
		t.Fatalf("a long list: %+v", left)
	}
	if text := strings.Join(left.Lines(), "\n"); !strings.Contains(text, "3 more") {
		t.Errorf("the lines do not say how many more: %s", text)
	}
	// The groups cannot be read: said, not silence.
	cloud := leftWorld(nil)
	cloud.listSecurityGroupsFn = func(context.Context, string) ([]*provision.SecurityGroup, error) {
		return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
	}
	eng, _ = newTestEngine(t, cloud, newMockDeployer())
	left = eng.OpenLeft(context.Background(), "p", "vm-1", []string{"sg-1"}, nil)
	if left == nil || left.Unread != clerrors.CodeAPIUnreachable || !strings.Contains(strings.Join(left.Lines(), "\n"), "could not read") {
		t.Errorf("an unread group: %+v", left)
	}
	// The interfaces cannot be read and no record names a group: said.
	cloud = leftWorld(nil)
	cloud.listInterfacesFn = func(context.Context, string) ([]*provision.Interface, error) {
		return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
	}
	eng, _ = newTestEngine(t, cloud, newMockDeployer())
	if left := eng.OpenLeft(context.Background(), "p", "vm-1", nil, nil); left == nil || left.Unread == "" {
		t.Errorf("unread interfaces and no group known: %+v", left)
	}
	// Unread interfaces with the group known: the rules are named, the
	// count of interfaces is not.
	cloud.listSecurityGroupsFn = leftWorld([]provision.SGRule{ingress("r-ssh", "22:22", "0.0.0.0/0")}).listSecurityGroupsFn
	if left := eng.OpenLeft(context.Background(), "p", "vm-1", []string{"sg-1"}, nil); left == nil || len(left.Groups) != 1 || left.Groups[0].Interfaces != nil || !left.Groups[0].Shared {
		t.Errorf("unread interfaces with the group known: %+v", left)
	}
}

// destroy's preview carries it: the line and the field, less the rules
// the destroy itself deletes.
func TestDestroyPreview_SaysWhatStaysOpenToTheInternet(t *testing.T) {
	cloud := leftWorld([]provision.SGRule{ingress("r-ssh", "22:22", "0.0.0.0/0"), ingress("r-own", "80:80", "0.0.0.0/0")})
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	rec := verifyRecord
	rec.ProjectID = eng.Config.ProjectID
	rec.SecurityGroupID, rec.SecurityGroupName = "sg-1", "SSH-access_ru.AZ-1"
	rec.OpenedRules = []state.SGRule{{ID: "r-own", SecurityGroupID: "sg-1", Port: "80"}}
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	p, asks, err := eng.DestroyPreview(context.Background())
	if err != nil || !asks {
		t.Fatalf("DestroyPreview: asks %v, %v", asks, err)
	}
	details, _ := p.Details.(map[string]any)
	raw, _ := json.Marshal(details["open_to_internet_left"])
	if !strings.Contains(string(raw), `"r-ssh"`) || strings.Contains(string(raw), `"r-own"`) {
		t.Errorf("open_to_internet_left = %s, want the SSH rule and not the deployment's own", raw)
	}
	if lines := strings.Join(p.Lines, "\n"); !strings.Contains(lines, "stay open to the internet") || !strings.Contains(lines, "tcp 22:22") {
		t.Errorf("the lines of the preview do not say it: %s", lines)
	}
}
