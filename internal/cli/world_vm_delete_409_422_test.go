// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Register CMP-7 (release plan 1.8, #1095, 10.11): Cloud.ru answered the
// DELETE of a VM in creating with 409 before, and now with 422. evo treats
// the two alike (isNotDeletableYet): in every case of evo destroy and the
// tool destroy, and in every state the VM reads in — creating among them —
// a 422 answers what a 409 answers, on both surfaces: the same exit code,
// reason, error and hint. The live half (≥2 deletes of a VM in creating)
// is a cloud-e2e run by the owner's yes.
func TestParityWorld_RefusedVMDelete409And422Alike(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	world := w.rt.extra
	var vmState string
	var code int
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "compute.api.cloud.ru" && r.URL.Path == "/api/v1/vms/"+parityVM {
			switch r.Method {
			case http.MethodDelete:
				fake.record(r)
				return consentJSON(code, `{"code":"vm_can_not_be_deleted","message":"VM can not be deleted in its current state"}`), nil
			case http.MethodGet:
				return consentJSON(http.StatusOK, `{"id":"`+parityVM+`","name":"web","state":"`+vmState+`"}`), nil
			}
		}
		return world(r)
	}
	ran := 0
	for _, c := range cases {
		if c.tool.Name != "destroy" || c.state == "no deployment" {
			continue
		}
		for _, st := range []string{"creating", "deleting", "error_deleting"} {
			vmState = st
			answers := map[int]string{}
			for _, code = range []int{http.StatusConflict, http.StatusUnprocessableEntity} {
				r := runJSONCase(t, c, w, fake)
				if !r.isErr || r.code == 0 {
					t.Errorf("%s (%s), DELETE %d: a refused delete is not a success: exit %d, %s", c.path, st, code, r.code, clipLine(r.text))
				}
				// Each call sends its own X-Request-Id: the rest is compared.
				answers[code] = requestIDNamed.ReplaceAllString(fmt.Sprintf("exit %d\nCLI: %s\nMCP: %s", r.code, r.errOut, r.text), "")
			}
			ran++
			if a, b := answers[http.StatusConflict], answers[http.StatusUnprocessableEntity]; a != b {
				t.Errorf("%s (%s): 409 and 422 are answered differently\n409: %s\n422: %s", c.path, st, a, b)
			}
		}
	}
	if ran == 0 {
		t.Fatal("no case of destroy reads the VM: the world tests nothing")
	}
}

// requestIDNamed is how an error names its request (X-Request-Id, release
// plan 1.9).
var requestIDNamed = regexp.MustCompile(` \(X-Request-Id [^()]*\)`)
