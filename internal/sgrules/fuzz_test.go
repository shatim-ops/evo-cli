// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A hand-edited or broken state.json must not panic destroy, and destroy
// must never delete a rule that another live deployment of the state needs.
func FuzzClose(f *testing.F) {
	f.Add(`{"deployments":[{"id":"a","security_group_id":"sg","opened_rules":[{"id":"r","security_group_id":"sg","port":"80/tcp"}]},{"id":"b","status":"running","security_group_id":"sg","security_group_ports":["80/tcp"]}]}`)
	f.Add(`{"deployments":[{"id":"a","opened_rules":[{"id":"","port":"x/"},{"id":"r","port":"/"}]},{"id":"a","exposed_ports":[0]}]}`)
	f.Add(`{"deployments":[{"id":"","status":"destroyed","opened_rules":[{"id":"../..","security_group_id":"?","port":"99999999999999999999/tcp"}]}]}`)
	f.Fuzz(func(t *testing.T, raw string) {
		var sf state.File
		if json.Unmarshal([]byte(raw), &sf) != nil || len(sf.Deployments) == 0 {
			return
		}
		id := sf.Deployments[0].ID
		_ = Plan(&sf, id)
		_ = Pending(&sf)
		cloud := newFake()
		// Every rule exists, so deletes succeed unless the rule is gone.
		for _, d := range sf.Deployments {
			for _, r := range d.OpenedRules {
				cloud.rules[r.SecurityGroupID] = append(cloud.rules[r.SecurityGroupID], provision.SGRule{ID: r.ID})
			}
		}
		before := sf.FindDeployment(id)
		var owned []state.SGRule
		if before != nil {
			owned = append(owned, before.OpenedRules...)
		}
		_ = Close(context.Background(), cloud, &sf, id)
		for _, del := range cloud.deleted {
			for _, r := range owned {
				if r.ID == del && neededBy(&sf, id, r.SecurityGroupID, r.Port) != nil {
					t.Fatalf("deleted %s although another deployment needs %s", del, r.Port)
				}
			}
		}
		Retire(&sf, id)
	})
}
