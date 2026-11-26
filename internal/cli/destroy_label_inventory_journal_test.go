// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// After a lost VM response, inventory of rules is the only way to
// reconcile the original operation. The rule ID was never in state.
type labelInventoryCloud struct {
	provision.CloudAPI
	available bool
	present   bool
	deletes   int
}

func (c *labelInventoryCloud) ListSecurityGroups(_ context.Context, _ string) ([]*provision.SecurityGroup, error) {
	if !c.available {
		return nil, errors.New("test inventory unavailable")
	}
	return []*provision.SecurityGroup{{ID: "sg-test"}}, nil
}
func (c *labelInventoryCloud) ListSecurityGroupRules(_ context.Context, group string) ([]*provision.SGRule, error) {
	if group != "sg-test" || !c.available {
		return nil, errors.New("test inventory unavailable")
	}
	if !c.present {
		return nil, nil
	}
	return []*provision.SGRule{{
		ID: "label-test", Direction: "ingress", IPProtocol: "tcp",
		PortRange: "8081:8081", Description: sgrules.RuleLabel("8081", parityVM),
	}}, nil
}
func (c *labelInventoryCloud) DeleteSecurityGroupRule(_ context.Context, group, id string) error {
	if group != "sg-test" || id != "label-test" || !c.present {
		return errors.New("unexpected cleanup")
	}
	c.present = false
	c.deletes++
	return nil
}

func TestDestroy_LabelInventoryResumesOriginalJournalEntry(t *testing.T) {
	w, fake := withJSONWorld(t)
	inventory := &labelInventoryCloud{present: true}
	base := toolEngine
	toolEngine = func(over map[string]any) (*engine.Engine, error) {
		e, err := base(over)
		if err == nil {
			inventory.CloudAPI = e.Cloud
			e.Cloud = inventory
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = base })
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := state.DeploymentRecord{ID: "dep-with-label", VMID: parityVM, ProjectID: pvProject, Status: "running", CreatedBy: eng.Maker()}
	if err := state.Write(state.DefaultPath(w.dir), &state.File{Deployments: []state.DeploymentRecord{rec}}); err != nil {
		t.Fatal(err)
	}
	previous := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/vms/"+parityVM {
			fake.mu.Lock()
			fake.vmGone = true
			fake.mu.Unlock()
			return nil, io.ErrUnexpectedEOF
		}
		return previous(r)
	}
	if _, failed := destroyLegacyPrimary(t, "CLI", false); !failed {
		t.Fatal("first VM response should be lost")
	}
	w.rt.extra = previous
	dir := intent.Dir(w.dir)
	pending := intent.Pending(dir)
	if len(pending) != 1 {
		t.Fatalf("lost response should leave one intent, got %d", len(pending))
	}
	original := pending[0].ID
	for _, available := range []bool{false, true} {
		inventory.available = available
		answer, failed := destroyLegacyPrimary(t, "CLI", false)
		var result struct{ Status string }
		if failed || json.Unmarshal([]byte(answer), &result) != nil {
			t.Fatalf("destroy failed: %s", answer)
		}
		pending = intent.Pending(dir)
		log, err := history.ReadAll(history.DirOf(dir), func(l history.Line) bool {
			return l.IntentID == original && l.Outcome == history.Deleted && l.ResourceID == parityVM
		})
		if err != nil {
			t.Fatal(err)
		}
		if !available {
			if result.Status != "partial" || len(pending) != 1 || pending[0].ID != original ||
				len(log.Lines) != 0 || !inventory.present || inventory.deletes != 0 {
				t.Errorf("unavailable: status=%s original pending=%v confirmed=%d rule present=%t deletes=%d",
					result.Status, pending, len(log.Lines), inventory.present, inventory.deletes)
			}
		} else if result.Status != "destroyed" || len(pending) != 0 || len(log.Lines) != 1 || inventory.present || inventory.deletes != 1 {
			t.Errorf("restored: status=%s pending=%d original confirmed=%d rule present=%t deletes=%d",
				result.Status, len(pending), len(log.Lines), inventory.present, inventory.deletes)
		}
	}
}
