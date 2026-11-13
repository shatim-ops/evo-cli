// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// A refusal of the platform for a quota, read by a script from --json at
// the path docs/exit-codes.md names: the fields of the tool's error answer
// lie in error.details, and the tool's own «details» with them —
// error.details.details.cause (FB19I-27: the fragment and the guide named
// details.cause «in --json», where nothing is). The reason and the exit
// code are the refusal's.
func TestQuotaCause_InJSONAtThePathTheDocsName(t *testing.T) {
	// The docs a script is written from name the path (read before the
	// world moves the directory).
	for _, doc := range []string{"docs/exit-codes.md", "docs/troubleshooting.md", "changelog.d/quota-cause.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", doc))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "error.details.details.cause") {
			t.Errorf("%s does not name the path of the cause in --json: error.details.details.cause", doc)
		}
	}
	w, _ := withJSONWorld(t)
	world := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Host == "compute.api.cloud.ru" && r.URL.Path == "/api/v1/disks" {
			return contractRT{status: http.StatusUnprocessableEntity,
				body: `[{"message":"Organization quota for this type of object exceeded","code":"organization_quota_exceeded"}]`}.RoundTrip(r)
		}
		return world(r)
	}
	t.Cleanup(func() { w.rt.extra = world })
	cmd := commandsByTool()["disk_create"]
	tl, _ := newRegistry().Lookup("disk_create")
	line := parityCommandOf(t, cmd.group, cmd.cmd, tl, copyArgs(parityArgs["disk_create"])).tree
	newProcess()
	code, _, stderr := runCLI(t, "", append([]string{"--json", "--yes"}, line...)...)
	env, ok := errorEnvelope(stderr)
	if !ok {
		t.Fatalf("no error envelope in stderr (exit %d): %s", code, stderr)
	}
	inner, _ := env.Details["details"].(map[string]any)
	if inner["cause"] != "quota" || inner["platform_code"] != "organization_quota_exceeded" {
		t.Errorf("want error.details.details.cause «quota» with its platform_code: %s", stderr)
	}
	if env.Reason != clerrors.CodeAPIError || code != clerrors.ExitCode(clerrors.New(clerrors.CodeAPIError, "", "")) {
		t.Errorf("want API_ERROR and its exit code, got %s, exit %d: %s", env.Reason, code, stderr)
	}
}
