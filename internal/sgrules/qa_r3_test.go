// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// lostRuleCloud applies the rule, but the answer is lost.
type lostRuleCloud struct{ *fakeCloud }

func (c lostRuleCloud) AddSecurityGroupRule(ctx context.Context, sg, p string, r provision.SGRule) (*provision.SGRule, error) {
	c.fakeCloud.seq++
	r.ID = fmt.Sprintf("r%d", c.fakeCloud.seq)
	c.fakeCloud.rules[sg] = append(c.fakeCloud.rules[sg], r)
	return nil, errors.New("cannot reach Cloud.ru API: connection reset")
}

// R6: a rule created with its answer lost is found by its description and
// owned by the deployment, so destroy closes it; it used to stay open to
// 0.0.0.0/0 in the shared group for good.
func TestQAr3_Open_LostAnswerAdoptsRuleByDescription(t *testing.T) {
	cloud := lostRuleCloud{newFake()}
	var rec state.DeploymentRecord
	res := Open(context.Background(), cloud, "p", "sg", []int{80}, nil, func(l string) string { return "evo: port " + l }, &rec)
	if len(rec.OpenedRules) != 1 || len(res.Opened) != 1 || len(res.Failed) != 0 {
		t.Fatalf("Open = %+v, rules %v; want the lost rule adopted", res, rec.OpenedRules)
	}
}

// refusedCloud refuses the rule (4xx): nothing to adopt.
type refusedCloud struct{ *fakeCloud }

func (c refusedCloud) AddSecurityGroupRule(context.Context, string, string, provision.SGRule) (*provision.SGRule, error) {
	return nil, &provision.StatusError{Status: 422, Body: "bad rule"}
}

func TestQAr3_Open_RefusedRuleFails(t *testing.T) {
	var rec state.DeploymentRecord
	res := Open(context.Background(), refusedCloud{newFake()}, "p", "sg", []int{80}, nil, func(l string) string { return "evo: port " + l }, &rec)
	if len(res.Failed) != 1 || len(rec.OpenedRules) != 0 {
		t.Fatalf("Open = %+v, rules %v; want failed", res, rec.OpenedRules)
	}
}
