// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// The password evo generated reaches the answer whenever the VM is left
// coming up without the call: on a timeout, and on a call cancelled
// mid-wait (Ctrl-C, an MCP client gone). Before, the cancelled answer
// lost it (Fable audit 1.2.1, FB121-1).
func TestProvision_PasswordInLeftAnswers(t *testing.T) {
	for name, run := range map[string]func(*progressCloud) (string, bool){
		"cancel": func(cloud *progressCloud) (string, bool) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cloud.onPoll = func(n int) {
				if n == 2 {
					cancel()
				}
			}
			text, isErr, _, _ := provisionRun(t, ctx, cloud, false, output.ModeJSON, time.Minute, nil)
			return text, isErr
		},
		"timeout": func(cloud *progressCloud) (string, bool) {
			text, isErr, _, _ := provisionRun(t, context.Background(), cloud, false, output.ModeJSON, 20*time.Millisecond, nil)
			return text, isErr
		},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &progressCloud{states: []string{"creating", "creating", "creating"}, fields: keyImageFields}
			text, isErr := run(cloud)
			if !isErr {
				t.Fatalf("want an error answer, got %s", text)
			}
			pw := cloud.createReq.ImageMetadata["linux_password"]
			if pw == "" {
				t.Fatal("the VM got no generated password")
			}
			var res map[string]any
			if err := json.Unmarshal([]byte(text), &res); err != nil {
				t.Fatal(err)
			}
			if res["vm_id"] != "vm-made" || res["password"] != pw {
				t.Errorf("vm_id %v, password %v (the VM got %q):\n%s", res["vm_id"], res["password"], pw, text)
			}
		})
	}
}
