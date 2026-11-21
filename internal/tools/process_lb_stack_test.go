// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProcessLBStackApproval(t *testing.T) {
	for _, label := range []string{"pinned_placement", "new_placement", "changed_saved_placement", "running_status_pinned", "running_status_new"} {
		bound := label != "new_placement" && label != "running_status_new"
		t.Run(label, func(t *testing.T) {
			stackHome(t)
			job := planJob(t, `{"name":"s","vm":{"name":"vm"},"nlb":{"name":"web","port":80}}`)
			const vpc = "00000000-0000-4000-8000-000000000077"
			const subnet = "00000000-0000-4000-8000-000000000078"
			job.ProjectID = raProject
			for _, step := range job.Steps {
				switch step.Key {
				case "vm", "tg":
					step.Status = "done"
					step.ResourceID = raTargetGroup
					if step.Key == "vm" {
						step.ResourceID = raMissingLB
					}
					job.Outputs[step.Key] = step.ResourceID
				case "lb":
					if bound {
						step.Args["vpc_id"] = vpc
						step.Args["zone"] = "ru.AZ-1"
						step.Args["subnet_id"] = subnet
					}
				default:
					t.Fatalf("unexpected step %s", step.Key)
				}
			}
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
			subs := raSubnets()
			for _, sn := range subs {
				if sn.VPCID == "vpc-1" {
					sn.VPCID = vpc
				}
				if sn.ID == "sn-1" {
					sn.ID = subnet
				}
			}
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				resp, err := f.RoundTrip(req)
				if err == nil && req.Method == "GET" && req.URL.Path == "/v2/targetGroups/"+raTargetGroup {
					b, e := io.ReadAll(resp.Body)
					resp.Body.Close()
					if e != nil {
						return nil, e
					}
					resp.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(string(b), "vpc-1", vpc)))
				}
				return resp, err
			})
			r := raRegistry(t, rt, raCloud{subnets: subs}, nil)
			input := map[string]any{"job_id": job.ID}
			q, shown, _ := callNamed(t, r, "stack_apply", input)
			if q["status"] != "confirmation_required" || f.postCount() != 0 {
				t.Fatalf("question %s", shown)
			}
			if bound && (!strings.Contains(shown, subnet) || !strings.Contains(shown, "ru.AZ-1") || !strings.Contains(shown, vpc)) {
				t.Fatalf("bound placement not visible: %s", shown)
			}
			wantSubnet := subnet
			if label == "changed_saved_placement" {
				wantSubnet = "00000000-0000-4000-8000-000000000079"
				for _, sn := range subs {
					if sn.ID == subnet {
						sn.ID = wantSubnet
					}
				}
				job.Steps[len(job.Steps)-1].Args["subnet_id"] = wantSubnet
				if err := job.Save(); err != nil {
					t.Fatal(err)
				}
			}
			operation := "stack_apply"
			if strings.HasPrefix(label, "running_status_") {
				job.Status = "running"
				if err := job.Save(); err != nil {
					t.Fatal(err)
				}
				operation = "stack_status"
				input["wait_seconds"] = float64(1)
			} else {
				input["confirmed"] = true
			}
			answer, out, _ := callNamed(t, r, operation, input)
			saved, err := stack.LoadJob(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			body := f.post("/v2/networkLoadBalancers")
			t.Logf("stack status=%v step=%s op=%s POST=%d body=%v", answer["status"], saved.Steps[len(saved.Steps)-1].Status, saved.Steps[len(saved.Steps)-1].OpID, f.postCount(), body)
			if label != "pinned_placement" && label != "running_status_pinned" {
				if f.postCount() != 0 || !strings.Contains(out, "confirmation_required") || !strings.Contains(out, wantSubnet) || !strings.Contains(out, "ru.AZ-1") {
					t.Fatalf("new placement needs a visible question before POST: %s", out)
				}
				return
			}
			b, _ := json.Marshal(body)
			if f.postCount() != 1 || body["vpc_id"] != vpc || !strings.Contains(string(b), `"subnet_ids":["`+subnet+`"]`) || !strings.Contains(string(b), `"name":"ru.AZ-1"`) || !strings.Contains(string(b), raTargetGroup) {
				t.Fatalf("pinned body: %s / %s", b, out)
			}
		})
	}
}

func TestProcessLBPostAfterLostReply(t *testing.T) {
	for _, c := range faultMatrixCases {
		if c.tool != "lb_create" {
			continue
		}
		for _, lag := range []int{0, 1000} {
			label := "visible"
			if lag > 0 {
				label = "list_lags"
			}
			t.Run(label, func(t *testing.T) {
				api, r, dir, k, ctx := paidCreateWorld(t, &c, lag)
				api.failAt(k, faultLost)
				first, _ := r.handlers[c.tool](ctx, c.args)
				api.disarm()
				before := creates(api)
				previous := intent.Pending(dir)
				second, _ := r.handlers[c.tool](ctx, c.args)
				current := intent.Pending(dir)
				t.Logf("first=%s next=%s posts %d -> %d billed=%d receipts %d -> %d", first, second, before, creates(api), len(api.billed()), len(previous), len(current))
				if before != 1 || creates(api) != 1 || len(api.billed()) != 1 {
					t.Fatal("a repeated call sent a second creation")
				}
				if lag > 0 && (len(previous) != 1 || answerStatus(second) != "confirmation_required" || !strings.Contains(second, `"interrupted"`)) {
					t.Fatal("unresolved repeat must report interrupted creation and ask")
				}
			})
		}
		return
	}
	t.Fatal("missing LB world")
}
