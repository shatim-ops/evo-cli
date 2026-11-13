// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A request of cloud-live.sh to an API of Cloud.ru — the token, a probe,
// a sweep — has a limit of time for the connection and for the answer. A
// request that hangs would keep the paid resources of the run until the
// limit of the job cuts it, with no sweep after (risk note 185, the debt
// named with it; R-89).
func TestScripts_CloudRequestsHaveTimeLimits(t *testing.T) {
	// The sweep of the stand (e2e_stand.sh) sends the same token for up to
	// half an hour: its requests had no limit (review of #1658).
	for _, f := range []struct {
		name string
		min  int
	}{{"cloud-live.sh", 6}, {"e2e_stand.sh", 3}} {
		cloudRequestsHaveTimeLimits(t, f.name, f.min)
	}
}

func cloudRequestsHaveTimeLimits(t *testing.T, name string, min int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") || !strings.Contains(l, "curl ") {
			continue
		}
		// A command goes on to the next line: the header with the token
		// is often there.
		for j := i; strings.HasSuffix(strings.TrimSpace(lines[j]), "\\") && j+1 < len(lines); j++ {
			l += " " + lines[j+1]
		}
		// The local evo mcp of the run is not the cloud (its request has a
		// limit of its own).
		if strings.Contains(l, "http://127.0.0.1") || (!strings.Contains(l, "Authorization: Bearer") && !strings.Contains(l, "api.cloud.ru")) {
			continue
		}
		checked++
		if !strings.Contains(l, "--connect-timeout ") || !strings.Contains(l, "--max-time ") {
			t.Errorf("%s:%d: a request to the cloud without --connect-timeout and --max-time: %s", name, i+1, strings.TrimSpace(l))
		}
	}
	if checked < min {
		t.Fatalf("%s: %d requests to the cloud read, want at least %d: the scan sees too few", name, checked, min)
	}
}
