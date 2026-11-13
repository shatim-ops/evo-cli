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
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
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
			t.Errorf("cloud-live.sh:%d: a request to the cloud without --connect-timeout and --max-time: %s", i+1, strings.TrimSpace(l))
		}
	}
	if checked < 6 {
		t.Fatalf("%d requests to the cloud read: the scan sees too few (the token, the pages of the registry, the DNS operation and its delete, api_once with and without a body)", checked)
	}
}
