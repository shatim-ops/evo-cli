// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// docs/exit-codes.md: with --json, stderr is one JSON object, so each line
// parses with jq. docs/install.md tells k8s kubectl to put evo flags before
// the cluster id, and a flag after it is USAGE. The refusal is still an
// error of a command whose argv contains --json, and jsonRequested sees
// that flag. run applied it only while commandStarted was false, so the
// refusal of the started kubectl command was plain text (break r3b, R3B-3).
func TestJSON_KubectlFlagAfterArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("EVO_LANG", "en")
	_, out, errOut := runCLI(t, "", "k8s", "kubectl", "00000000-0000-4000-8000-000000000001", "get", "pods", "--json")
	if why := jsonImpure(out, errOut); why != "" {
		t.Fatalf("docs/exit-codes.md: stderr must be JSON lines, got %s\n%s", why, errOut)
	}
	if !strings.Contains(errOut, `"reason"`) {
		t.Fatalf("want a reason in the JSON error, got %s", errOut)
	}
}
