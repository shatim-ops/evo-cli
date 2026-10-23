// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// fakeWait records what the wait for the VM was told.
type fakeWait struct {
	states []string
	done   bool
}

func (w *fakeWait) C() <-chan time.Time { return nil }
func (w *fakeWait) Show()               {}
func (w *fakeWait) State(s string)      { w.states = append(w.states, s) }
func (w *fakeWait) Done()               { w.done = true }

// deploy_apply shows its stages as it goes — the create, the wait for the
// VM, its address, the ports — to the person at the CLI and to an MCP
// client that asked (owner's decision 23.10: it was silent for minutes on
// both). Without a Progress it shows nothing, as before.
func TestApply_ShowsItsStages(t *testing.T) {
	lang := i18n.Lang()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang(lang) })
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	var buf bytes.Buffer
	wait := &fakeWait{}
	cloud := &portsCloud{}
	plan := &Plan{Product: "wiki", VM: VMSpec{Name: "wiki-1"}, Network: NetworkSpec{SecurityGroupID: "sg-1"}, Ports: []int{80}}
	var opened []string
	show := &Progress{P: output.NewWithWriter(&buf, output.ModePlain, false), Wait: wait,
		Ports: func(r sgrules.OpenResult, _ *state.DeploymentRecord) { opened = r.Opened }}
	if _, err := ApplyWithTimeout(context.Background(), cloud, plan, 0, show); err != nil {
		t.Fatal(err)
	}
	at, out := 0, buf.String()
	for _, want := range []string{"==> Creating VM wiki-1…", "Request accepted: VM wiki-1", "VM wiki-1 is running"} {
		i := strings.Index(out[at:], want)
		if i < 0 {
			t.Fatalf("no %q after position %d:\n%s", want, at, out)
		}
		at += i + len(want)
	}
	if len(opened) != 1 || opened[0] != "80" {
		t.Errorf("the ports shown: %v", opened)
	}
	if len(wait.states) == 0 || !wait.done {
		t.Errorf("the wait for the VM: states %v, done %v", wait.states, wait.done)
	}

	if _, err := ApplyWithTimeout(context.Background(), &portsCloud{}, plan, 0, nil); err != nil {
		t.Errorf("without a Progress: %v", err)
	}
}
