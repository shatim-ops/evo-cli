// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The normal parity world returns an empty project FIP list. Here an
// extra address can still be listed after an accepted cleanup request.
type destroyExtraAddressWorld struct {
	provision.CloudAPI
	id string
}

func (w destroyExtraAddressWorld) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: w.id, Name: "extra", State: "available"}}, nil
}

// Exercise both D127 answers through the CLI with a real saved deployment.
// An accepted DELETE is followed first by a still-visible paid address,
// then on the next destroy by a GET confirming its disappearance.
func TestDestroyJSON_OwnAddressConfirmedOnRetry(t *testing.T) {
	testDestroyPaidAddressOnRetry(t, false)
}

func TestDestroyJSON_ExtraAddressConfirmedOnRetry(t *testing.T) {
	testDestroyPaidAddressOnRetry(t, true)
}

func testDestroyPaidAddressOnRetry(t *testing.T, extra bool) {
	t.Helper()
	w, _ := withJSONWorld(t)
	_, done := paritySetup["deploy"](t, w)
	defer done()
	if code, out, errOut := runCLI(t, "", "--json", "deploy", "--yes"); code != 0 {
		t.Fatalf("deploy: exit %d, %s%s", code, out, errOut)
	}
	addressID := parityVM
	if extra {
		addressID = "00000000-0000-4000-8000-000000000008"
		base := toolEngine
		toolEngine = func(o map[string]any) (*engine.Engine, error) {
			e, err := base(o)
			if err == nil {
				e.Cloud = destroyExtraAddressWorld{e.Cloud, addressID}
			}
			return e, err
		}
		defer func() { toolEngine = base }()
		path := state.DefaultPath(w.dir)
		sf, err := state.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		rec := sf.ActiveDeployment()
		sf.LostFloatingIPs = []state.LostFloatingIP{{ID: addressID, Name: "extra", VMID: rec.VMID, ProjectID: rec.ProjectID}}
		if err := state.Write(path, sf); err != nil {
			t.Fatal(err)
		}
	}
	previous := w.rt.extra
	var deletes, reads int
	gone := false
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/floating-ips/"+addressID {
			switch r.Method {
			case http.MethodDelete:
				deletes++
				return contractRT{status: http.StatusAccepted, body: `{}`}.RoundTrip(r)
			case http.MethodGet:
				if deletes > 0 {
					reads++
					if gone {
						return contractRT{status: http.StatusNotFound, body: `{"message":"gone"}`}.RoundTrip(r)
					}
					return contractRT{status: http.StatusOK, body: `{"id":"` + addressID + `","state":"deleting"}`}.RoundTrip(r)
				}
				return contractRT{status: http.StatusOK, body: `{"id":"` + addressID + `","state":"available"}`}.RoundTrip(r)
			}
		}
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/security-groups/") && strings.Contains(r.URL.Path, "/rules/") {
			return contractRT{status: http.StatusOK, body: `{}`}.RoundTrip(r)
		}
		return previous(r)
	}
	for _, want := range []string{"partial", "destroyed"} {
		gone = want == "destroyed"
		before := reads
		code, out, errOut := runCLI(t, "", "--json", "destroy", "--yes")
		var result struct {
			Status  string `json:"status"`
			Pending *bool  `json:"cleanup_pending"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Status != want ||
			result.Pending == nil || *result.Pending == gone || reads == before {
			t.Fatalf("%s: exit %d, reads %d, %s%s", want, code, reads-before, out, errOut)
		}
		sf, err := state.Read(state.DefaultPath(w.dir))
		if err != nil {
			t.Fatal(err)
		}
		rec := sf.ActiveDeployment()
		if extra && ((!gone && (len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0].ID != addressID)) ||
			(gone && len(sf.LostFloatingIPs) != 0)) {
			t.Fatalf("%s: extra address receipt %+v", want, sf.LostFloatingIPs)
		}
		if (!gone && (rec == nil || rec.FloatingIPID != parityVM)) || (gone && rec != nil) {
			t.Fatalf("%s: receipt %+v", want, rec)
		}
	}
}
