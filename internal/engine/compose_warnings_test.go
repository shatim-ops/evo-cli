// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// localOnlyCompose uses what deploy does not upload (guide §5).
const localOnlyCompose = `services:
  web:
    build: .
    env_file: .env
    volumes:
      - ./data:/data
    ports:
      - "8080:80"
`

// --dry-run shows the compose warnings before the preview, for a new VM and
// for an update of the existing one (QA r4 Q2).
func TestDeploy_DryRunShowsComposeWarnings(t *testing.T) {
	for _, existing := range []bool{false, true} {
		eng, cloud, opts, stdout, stderr := newDeployForConfirm(t)
		if err := os.WriteFile(opts.ComposeFile, []byte(localOnlyCompose), 0o644); err != nil {
			t.Fatal(err)
		}
		if existing {
			sf := &state.File{Version: 1}
			sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", Status: "running",
				ComposeHash: "old", CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
				t.Fatal(err)
			}
		}
		opts.DryRun = true

		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("existing=%v: dry run: %v", existing, err)
		}
		if cloud.createCalled {
			t.Fatalf("existing=%v: --dry-run created a VM", existing)
		}
		for _, want := range []string{"Warning: service \"web\": build:", "env_file: .env", "volumes: ./data:/data"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("existing=%v: warnings lack %q:\nstderr:\n%s\nstdout:\n%s", existing, want, stderr.String(), stdout.String())
			}
		}
	}
}
