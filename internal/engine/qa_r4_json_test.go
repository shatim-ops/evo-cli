// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// README и docs/exit-codes.md: «evo deploy --yes --json > deploy.json».
// Успешный deploy в режиме JSON должен напечатать объект с результатом.
func TestDeploy_JSONPrintsResultObject(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModeJSON, false)

	composePath := writeComposeFile(t, tmpDir)
	err := eng.Deploy(context.Background(), DeployOptions{ComposeFile: composePath, Flavor: "small",
		Region: "az-1", NonInteractive: true, Yes: true})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(out.String(), `"vm_id"`) || !strings.Contains(out.String(), `"status": "deployed"`) {
		t.Fatalf("deploy --json: stdout без результата развёртывания; получено %q", out.String())
	}

	// Повтор без изменений compose — {"status":"unchanged"}, а не пусто.
	out.Reset()
	if err := eng.Deploy(context.Background(), DeployOptions{ComposeFile: composePath, Flavor: "small",
		Region: "az-1", NonInteractive: true, Yes: true}); err != nil {
		t.Fatalf("Deploy (повтор): %v", err)
	}
	if !strings.Contains(out.String(), `"status": "unchanged"`) {
		t.Fatalf("повторный deploy --json: %q", out.String())
	}
}
