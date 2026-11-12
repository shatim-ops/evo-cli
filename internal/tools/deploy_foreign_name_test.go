// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// FB170-2 (Fable's audit of 1.7): deploy names its VM evo-<project_name>.
// A VM of the project with that name, one this folder did not make, was
// not looked for: deploy asked for consent, Cloud.ru answered 409, and the
// hint said to delete that VM — someone else's, maybe. Now deploy refuses
// it before the question, as provision and deploy_plan do (D73): USAGE
// with existing_id, nothing created, and the way out is another name.
func TestDeploy_ANameTakenBySomeoneElseIsRefusedBeforeTheQuestion(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	foreign, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-web", ProjectID: faultProject})
	if err != nil {
		t.Fatal(err)
	}
	compose := writeTestCompose(t)
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage, ProjectName: "web",
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModePlain, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   faultToolsTimings(),
		}, nil
	})
	r.SetStateDir(dir)
	for _, args := range []map[string]any{{}, {"confirmed": true}} {
		text, isErr := r.handlers["deploy"](context.Background(), args)
		label := "deploy " + jsonArgs(args)
		ownRefusal(t, label, text, isErr, foreign.ID)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if m["status"] == "confirmation_required" {
			t.Errorf("%s: asks for consent to a VM name another VM has: %s", label, text)
		}
		for _, word := range []string{"vm_delete", "vm delete", "destroy"} {
			if strings.Contains(fmtAll(m["hint"], m["error"]), word) {
				t.Errorf("%s: the answer points at a delete (%s) of a VM that may be someone else's: %s", label, word, text)
			}
		}
		if hint, _ := m["hint"].(string); !strings.Contains(hint, "project_name") {
			t.Errorf("%s: the hint does not say where deploy's VM name comes from (project_name): %s", label, text)
		}
	}
	if vms := cloud.liveVMs(); len(vms) != 1 {
		t.Errorf("%d VMs after the refusals, want only the foreign one", len(vms))
	}
	if fips := cloud.liveFIPs(); len(fips) != 0 {
		t.Errorf("floating IPs after the refusals: %v, want none", fips)
	}
}

func jsonArgs(args map[string]any) string {
	b, _ := json.Marshal(args)
	return string(b)
}

func fmtAll(vs ...any) string {
	var b strings.Builder
	for _, v := range vs {
		if s, ok := v.(string); ok {
			b.WriteString(s)
			b.WriteByte(' ')
		}
	}
	return b.String()
}
