// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func TestDestroyLostReplyHybridCleanup(t *testing.T) {
	for _, cmd := range []string{"targeted", "compose"} {
		for _, lost := range []string{"address", "rule", "both"} {
			for _, retry := range []string{"cli", "mcp"} {
				t.Run(cmd+"/"+lost+"/"+retry, func(t *testing.T) { checkDestroyLostReply(t, cmd, lost, retry) })
			}
		}
	}
}

func checkDestroyLostReply(t *testing.T, cmd, lost, retry string) {
	w := withParityWorld(t)
	t.Setenv("CLOUDRU_KEY_ID", "fixture-destroy")
	const address = "00000000-0000-4000-8000-0000000000f1"
	rec := state.DeploymentRecord{ID: parityVM, VMID: parityVM, Status: "running", ProjectID: pvProject,
		FloatingIPID: address, VMIP: "192.0.2.8", OpenedRules: []state.SGRule{{ID: "own-rule", SecurityGroupID: "own-group", Port: "80/tcp"}}}
	if err := state.Write(state.DefaultPath(w.dir), &state.File{Deployments: []state.DeploymentRecord{rec}}); err != nil {
		t.Fatal(err)
	}
	vmDeletes, addressDeletes, ruleDeletes := 0, 0, 0
	canRead := false
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		reply := func(code int, body string) (*http.Response, error) {
			return contractRT{status: code, body: body}.RoundTrip(r)
		}
		switch r.URL.Path {
		case "/api/v1/vms":
			if vmDeletes == 0 {
				return reply(200, `{"items":[{"id":"`+parityVM+`","name":"web","state":"running"}],"total":1}`)
			}
			return reply(200, `{"items":[],"total":0}`)
		case "/api/v1/vms/" + parityVM:
			if r.Method == "DELETE" {
				vmDeletes++
				return nil, errors.New("read tcp: connection reset by peer")
			}
			if vmDeletes == 0 {
				return reply(200, `{"id":"`+parityVM+`","state":"running"}`)
			}
			if !canRead {
				return reply(403, `{"detail":"temporarily denied"}`)
			}
			return reply(404, `{"detail":"VM not found"}`)
		case "/api/v1/floating-ips/" + address:
			if r.Method == "DELETE" {
				addressDeletes++
				if lost != "rule" && addressDeletes == 1 {
					return nil, errors.New("read tcp: connection reset by peer")
				}
				return reply(204, "")
			}
		case "/api/v1/security-groups/own-group/rules/own-rule":
			if r.Method == "DELETE" {
				ruleDeletes++
				if lost != "address" && ruleDeletes == 1 {
					return nil, errors.New("read tcp: connection reset by peer")
				}
				return reply(204, "")
			}
		}
		if r.Method == "GET" {
			return reply(200, `{"items":[],"total":0}`)
		}
		t.Fatalf("unexpected %s %s", r.Method, r.URL)
		return nil, nil
	})
	a := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
	a.Backoff = time.Millisecond
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := old(o)
		if err == nil {
			e.Cloud = a
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = old })
	pending := func() []intent.Intent { return intent.Pending(intent.Dir(w.dir)) }
	command := []string{"--yes", "--json", "vm", "delete", parityVM}
	if cmd == "compose" {
		command = []string{"--yes", "--json", "destroy"}
	}
	code, out, errOut := runCLI(t, "", command...)
	first := pending()
	if code == 0 || !strings.Contains(errOut, "API_UNREACHABLE") || len(first) != 1 || vmDeletes != 1 || addressDeletes != 0 || ruleDeletes != 0 {
		t.Fatalf("first: %d %s %s receipts=%+v writes=%d/%d/%d", code, out, errOut, first, vmDeletes, addressDeletes, ruleDeletes)
	}
	checkJournal := func(want string) {
		t.Helper()
		j, err := history.ReadAll(history.DirOf(intent.Dir(w.dir)), nil)
		if err != nil {
			t.Fatal(err)
		}
		var latest history.Line
		for _, l := range j.Lines {
			if l.IntentID == first[0].ID {
				latest = l
			}
		}
		if latest.Outcome != want || latest.ResourceID != parityVM || latest.Surface != "cli" {
			t.Fatalf("original journal line: %+v, want %s", latest, want)
		}
	}
	checkJournal(history.Unknown)
	canRead = true
	s := newROServers(t)
	call := func(name string, args map[string]any) string {
		t.Helper()
		ans, _ := s.stdio(t, s.plain, "tools/call", map[string]any{"name": name, "arguments": args})
		if ans.rpcErr != "" {
			t.Fatal(ans.rpcErr)
		}
		return ans.text
	}
	doctor := call("doctor", map[string]any{})
	if p := pending(); len(p) != 1 || p[0].ID != first[0].ID || !strings.Contains(doctor, "cleanup") {
		t.Fatalf("doctor: %s receipts=%+v", doctor, p)
	}
	args := map[string]any{"vm_id": parityVM, "confirmed": true}
	part := call("destroy", args)
	if parseObject(part)["cleanup_pending"] != true || addressDeletes != 1 || ruleDeletes != 1 || vmDeletes != 1 {
		t.Fatalf("partial: %s writes=%d/%d/%d", part, vmDeletes, addressDeletes, ruleDeletes)
	}
	if p := pending(); len(p) == 0 || p[0].ID != first[0].ID {
		t.Fatalf("partial cleanup retired original receipt: %+v", p)
	}
	checkJournal(history.Unknown)
	// A receipt from before argument fingerprints uses the original
	// name and target. An otherwise identical receipt for another VM stays.
	legacy := first[0]
	legacy.Canon, legacy.Print, legacy.PID = nil, "", 0
	if _, err := intent.Open(intent.Dir(w.dir), legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Target = "vm_id=" + parityStoppedVM
	foreign, err := intent.Open(intent.Dir(w.dir), legacy)
	if err != nil {
		t.Fatal(err)
	}
	var finish string
	if retry == "cli" {
		code, out, errOut := runCLI(t, "", "--yes", "--json", "vm", "delete", parityVM)
		if code != 0 {
			t.Fatalf("retry: %s %s", out, errOut)
		}
		finish = out
	} else {
		finish = call("destroy", args)
	}
	wantAddress, wantRule := 2, 2
	if lost == "rule" {
		wantAddress = 1
	}
	if lost == "address" {
		wantRule = 1
	}
	if p := pending(); len(p) != 1 || p[0].ID != foreign.ID || parseObject(finish)["cleanup_pending"] == true || addressDeletes != wantAddress || ruleDeletes != wantRule || vmDeletes != 1 {
		t.Fatalf("finish: %s receipts=%+v writes=%d/%d/%d", finish, p, vmDeletes, addressDeletes, ruleDeletes)
	}
	checkJournal(history.AlreadyDeleted)
}

func TestDestroyConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "vm", "delete", "destroy", "vm_id", "vms", parityVM)
}
