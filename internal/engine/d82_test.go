// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"reflect"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// nextOf is the step logs names for an unfinished deployment: next_tool
// and next_args, nil when it names none.
func nextOf(t *testing.T, rec state.DeploymentRecord) (any, map[string]any) {
	t.Helper()
	rec.Status = "provisioned"
	eng, _, _ := engineWithDeployment(t, rec)
	err := eng.Logs(context.Background(), LogsOptions{Tail: 10})
	var ue *DeployUnfinishedError
	if !stderrors.As(err, &ue) {
		t.Fatalf("logs of an unfinished deployment: %v, want DEPLOY_IN_PROGRESS", err)
	}
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) {
		t.Fatalf("not a CLIError: %v", err)
	}
	args, _ := ce.Details["next_args"].(map[string]any)
	return ce.Details["next_tool"], args
}

// D82 (FB172-1; owner's decision 05.11, variant A; «Риск», conclusion 10):
// the step «go on with deploy» of D76 is the one of D69 — the same deploy
// with confirmed: true, one consent — but only for a record deploy began,
// with the arguments the record knows (R-36); a record of deploy_apply
// goes on by deploy_apply with its plan (R-35, FB172-2); a record whose
// way on evo cannot tell gets no machine step.
func TestDeployUnfinished_NextStepIsTheRecordsOwn(t *testing.T) {
	base := verifyRecord
	for _, tc := range []struct {
		name string
		rec  func(r *state.DeploymentRecord)
		tool any
		args map[string]any
	}{
		{"deploy with its cidr", func(r *state.DeploymentRecord) {
			r.Source, r.ComposePath, r.Cidr = "", "/srv/app/docker-compose.yml", "203.0.113.0/24"
		}, "deploy", map[string]any{"compose_file": "/srv/app/docker-compose.yml", "cidr": "203.0.113.0/24", "confirmed": true}},
		// A record from before 1.8 does not know the cidr its ports were
		// opened from: the repeat could open the missing ones wider, so
		// it asks (no confirmed).
		{"deploy without a cidr", func(r *state.DeploymentRecord) {
			r.Source, r.ComposePath, r.Cidr = "", "/srv/app/docker-compose.yml", ""
		}, "deploy", map[string]any{"compose_file": "/srv/app/docker-compose.yml"}},
		{"deploy_apply with its plan", func(r *state.DeploymentRecord) {
			r.Source, r.PlanID, r.ComposePath = state.SourceDeployApply, "plan-0123456789ab", ""
		}, "deploy_apply", map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}},
		{"deploy_apply without a plan", func(r *state.DeploymentRecord) {
			r.Source, r.PlanID, r.ComposePath = state.SourceDeployApply, "", ""
		}, nil, nil},
		// provision takes back a VM it left creating by its name; the step
		// is not deploy's to name (R-35).
		{"provision", func(r *state.DeploymentRecord) {
			r.Source, r.ComposePath = state.SourceProvision, ""
		}, nil, nil},
		// Before 1.8 a record of deploy_apply had no source, and no
		// compose file either: deploy is not its way on.
		{"no source, no compose file", func(r *state.DeploymentRecord) {
			r.Source, r.ComposePath = "", ""
		}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := base
			tc.rec(&rec)
			tool, args := nextOf(t, rec)
			if tool != tc.tool || (tc.args != nil || args != nil) && !reflect.DeepEqual(args, tc.args) {
				t.Errorf("next_tool %v, next_args %v; want %v, %v", tool, args, tc.tool, tc.args)
			}
		})
	}
}

// D82, R-36: the record of deploy keeps the source its ports were opened
// from, so the step that goes on with it (wayOn) opens the missing ones
// from the same, not wider.
func TestDeploy_RecordKeepsItsCidr(t *testing.T) {
	for _, cidr := range []string{"", "203.0.113.0/24"} {
		eng, dir := newTestEngine(t, &mockCloudAPI{}, newMockDeployer())
		opts := newDeployOpts(t, dir)
		opts.Cidr = cidr
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("cidr %q: %v", cidr, err)
		}
		want := cidr
		if want == "" {
			want = "0.0.0.0/0"
		}
		if rec := readActive(t, dir); rec == nil || rec.Cidr != want {
			t.Errorf("cidr %q: the record keeps %+v, want %q", cidr, rec, want)
		}
	}
}
