// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// Audit cli-ids-1.6, §3.2 (consent gap): the preview of lb_create named
// neither the target group nor the VPC the billable balancer serves, and
// that of tg_create said «targets: 1». Both are read before the question:
// the preview names them by id and by name.
func TestLbTgCreate_PreviewNamesWhatTheyServe(t *testing.T) {
	a := &faultAPI{made: []faultResource{{host: "vpc.api.cloud.ru", path: "/v1/vpcs", ID: "vpc-1", Name: "net", before: true}}}
	r := faultAPIRegistry(t, a)
	m, _, _ := callNamed(t, r, "lb_create", map[string]any{"name": "evo-lb", "target_group_id": "tg-1", "port": float64(80), "zone": "ru.AZ-1"})
	p := previewParams(t, m)
	if m["status"] != "confirmation_required" || p["target_group_id"] != "tg-1" || p["target_group_name"] != "tg" ||
		p["vpc_id"] != "vpc-1" || p["vpc_name"] != "net" {
		t.Errorf("lb_create preview does not name its target group and VPC: %v", p)
	}

	m, _, _ = callNamed(t, r, "tg_create", map[string]any{"name": "evo-tg", "targets": []any{map[string]any{"id": faultTargetVM, "interface_id": faultNIC}}})
	p = previewParams(t, m)
	targets, _ := p["targets"].([]any)
	first, _ := func() (map[string]any, bool) {
		if len(targets) != 1 {
			return nil, false
		}
		v, ok := targets[0].(map[string]any)
		return v, ok
	}()
	if m["status"] != "confirmation_required" || first["id"] != faultTargetVM || first["name"] != "web" {
		t.Errorf("tg_create preview does not name its target VMs: %v", p)
	}
}

func TestLBStackResumeSameJob(t *testing.T) {
	j := pausedLBWorldJob(t)
	f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	a := map[string]any{"job_id": j.ID, "confirmed": true}
	q, raw, _ := callNamed(t, r, "stack_apply", a)
	if q["status"] != "confirmation_required" || f.postCount() != 0 {
		t.Fatal(raw)
	}
	old := q["preview_id"]
	for i := 0; i < 2; i++ {
		m, raw, _ := callNamed(t, r, "stack_status", map[string]any{"job_id": j.ID})
		if m["status"] != "awaiting_lb_approval" || f.postCount() != 0 {
			t.Fatal(raw)
		}
	}
	f.targetVMs = `{"id":"vm-1","zone_name":"ru.AZ-2","vpc_id":"vpc-1"}`
	a["preview_id"] = old
	for i := 0; i < 2; i++ {
		q, raw, _ = callNamed(t, r, "stack_apply", a)
		if q["status"] != "confirmation_required" || q["preview_id"] == old || f.postCount() != 0 {
			t.Fatal(raw)
		}
	}
	a["preview_id"] = q["preview_id"]
	_, raw, _ = callNamed(t, r, "stack_apply", a)
	job, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.postCount() != 1 || job.Outputs["vm"] != raMissingLB || job.Outputs["tg"] != raTargetGroup || job.Steps[len(job.Steps)-1].OpID != "op-lb" {
		t.Fatalf("wrong continuation: %s", raw)
	}
}

func TestLBReceiptStorageFailure(t *testing.T) {
	for _, when := range []string{"before-offer", "after-acceptance", "saved"} {
		t.Run(when, func(t *testing.T) {
			f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
			r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
			root, _ := r.stateDir()
			blockDirectory := func(path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if when == "before-offer" {
				blockDirectory(filepath.Join(root, "lb-previews"))
			}
			a := map[string]any{"name": "receipt", "port": float64(80), "target_group_id": "tg-1"}
			q, raw, failed := callNamed(t, r, "lb_create", a)
			if when == "before-offer" {
				if !failed || q["preview_id"] != nil || f.postCount() != 0 {
					t.Fatal(raw)
				}
				return
			}
			if when == "after-acceptance" {
				blockDirectory(MadeDir(root))
			}
			a["confirmed"] = true
			a["preview_id"] = q["preview_id"]
			m, raw, failed := callNamed(t, r, "lb_create", a)
			if f.postCount() != 1 || m["operation_id"] != "op-lb" {
				t.Fatal(raw)
			}
			if when == "after-acceptance" {
				if !failed || m["receipt_error"] != true || len(intent.Pending(intent.Dir(root))) != 1 {
					t.Fatal(raw)
				}
				callNamed(t, r, "lb_create", a)
				if f.postCount() != 1 {
					t.Fatal("lost durable receipt resulted in a second POST")
				}
				return
			}
			files, err := os.ReadDir(MadeDir(root))
			if failed || err != nil || len(files) != 1 {
				t.Fatalf("%s receipts=%v %v", raw, files, err)
			}
			m, raw, _ = callNamed(t, r, "lb_create", a)
			if m["repeat_of"] == nil || f.postCount() != 1 {
				t.Fatal(raw)
			}
		})
	}
}

func pausedLBWorldJob(t *testing.T) *stack.Job {
	t.Helper()
	stackHome(t)
	j := planJob(t, `{"name":"stack","vm":{"name":"vm"},"nlb":{"name":"web","port":80}}`)
	j.ProjectID = raProject
	for _, st := range j.Steps {
		switch st.Key {
		case "vm", "tg":
			st.Status = "done"
			st.ResourceID = raTargetGroup
			if st.Key == "vm" {
				st.ResourceID = raMissingLB
			}
			j.Outputs[st.Key] = st.ResourceID
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestLBStackResourcePostOutcome(t *testing.T) {
	for _, status := range []int{403, 408, 503, 0} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			j := pausedLBWorldJob(t)
			f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
			sends := 0
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.Path != "/v2/networkLoadBalancers" {
					return f.RoundTrip(req)
				}
				sends++
				if status == 0 {
					return nil, io.ErrUnexpectedEOF
				}
				rec := httptest.NewRecorder()
				rec.WriteHeader(status)
				return rec.Result(), nil
			})
			r := raRegistry(t, rt, raCloud{subnets: raSubnets()}, nil)
			a := map[string]any{"job_id": j.ID, "confirmed": true}
			q, raw, _ := callNamed(t, r, "stack_apply", a)
			if q["preview_id"] == nil || sends != 0 {
				t.Fatal(raw)
			}
			a["preview_id"] = q["preview_id"]
			_, raw, _ = callNamed(t, r, "stack_apply", a)
			job, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			step := job.Steps[len(job.Steps)-1]
			if status == 403 {
				if job.Status != "error" || step.Status != "error" || step.Reason != "API_ERROR" || !strings.Contains(step.Detail, "403") || job.LBPending != nil || job.LBUncertain {
					t.Fatalf("job=%+v step=%+v: %s", job, step, raw)
				}
			} else if job.Status != "awaiting_lb_approval" || job.LBPending == nil || !job.LBUncertain {
				t.Fatalf("job=%+v: %s", job, raw)
			}
			callNamed(t, r, "stack_status", map[string]any{"job_id": j.ID})
			callNamed(t, r, "stack_apply", a)
			if sends != 1 {
				t.Fatalf("resource sends: %d", sends)
			}
		})
	}
}

func TestLBApprovalIdentity(t *testing.T) {
	key := "same-first-last"
	api := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
	r := raRegistry(t, api, raCloud{subnets: raSubnets()}, func(c *config.Config) { c.KeyID = key })
	a := map[string]any{"name": "test", "port": float64(80), "target_group_id": raTargetGroup}
	q, raw, _ := callNamed(t, r, "lb_create", a)
	old := q["preview_id"]
	if old == nil {
		t.Fatal(raw)
	}
	next := "same-other-last"
	if config.MaskKeyID(key) != config.MaskKeyID(next) {
		t.Fatal("fixture masks differ")
	}
	key = next
	a["confirmed"], a["preview_id"] = true, old
	q, raw, _ = callNamed(t, r, "lb_create", a)
	if q["preview_id"] == nil || q["preview_id"] == old || api.postCount() != 0 || strings.Contains(raw, key) {
		t.Fatal(raw)
	}
	a["preview_id"] = q["preview_id"]
	if _, raw, failed := callNamed(t, r, "lb_create", a); failed || api.postCount() != 1 {
		t.Fatal(raw)
	}
}
func TestLBApprovalTargetProject(t *testing.T) {
	for _, tc := range []struct{ label, owner, returned, reason string }{
		{"missing project", "", raTargetGroup, "API_ERROR"},
		{"other allowed project", "second-allowed-project", raTargetGroup, "NOT_FOUND"},
		{"wrong group", raProject, raMissingLB, "NOT_FOUND"},
		{"explicit id", raProject, raTargetGroup, ""},
		{"id omitted", raProject, "", ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				res, err := f.RoundTrip(req)
				if err == nil && req.URL.Path == "/v2/targetGroups/"+raTargetGroup {
					var card map[string]any
					err = json.NewDecoder(res.Body).Decode(&card)
					res.Body.Close()
					if err != nil {
						return nil, err
					}
					card["project_id"] = tc.owner
					if tc.returned == "" {
						delete(card, "id")
					} else {
						card["id"] = tc.returned
					}
					b, err := json.Marshal(card)
					if err != nil {
						return nil, err
					}
					res.Body = io.NopCloser(strings.NewReader(string(b)))
				}
				return res, err
			})
			r := raRegistry(t, rt, raCloud{subnets: raSubnets()}, nil)
			r.SetProjectBound()
			a := map[string]any{"name": "test", "port": float64(80), "target_group_id": raTargetGroup}
			q, raw, _ := callNamed(t, r, "lb_create", a)
			if tc.reason == "" {
				if q["preview_id"] == nil {
					t.Fatal(raw)
				}
			} else if q["preview_id"] != nil || q["reason"] != tc.reason || strings.Contains(raw, "second-allowed-project") {
				t.Fatal(raw)
			}
			if tc.reason == "" {
				a["preview_id"], a["confirmed"] = q["preview_id"], true
				if _, txt, failed := callNamed(t, r, "lb_create", a); failed || f.postCount() != 1 {
					t.Fatal(txt)
				}
				return
			}
			if f.postCount() != 0 {
				t.Fatal("unexpected write")
			}
		})
	}
}
