// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// probeHosts — the hosts cloud-live.sh sends the IAM token of the workflow
// to. Closed list: a new host is a new surface of a workflow with a secret
// and comes with a note of the risk role (note 183 for resource-manager).
var probeHosts = map[string]string{
	"compute.api.cloud.ru":          "Compute: the probes and the sweeps",
	"iam.api.cloud.ru":              "IAM: the token itself",
	"ar.api.cloud.ru":               "Artifact Registry: the pages of the list",
	"dns.api.cloud.ru":              "DNS: the links of a zone",
	"secretmanager.api.cloud.ru":    "Secret Manager: a secret in a folder",
	"resource-manager.api.cloud.ru": "Resource Manager: the tags (Г-17, risk note 183)",
	"mk8s.api.cloud.ru":             "Managed Kubernetes: node pools and versions (Г-12, area_k8schange)",
	"$host.api.cloud.ru":            "DBaaS: postgresql, kafka or redis, chosen by a case of constants (area_dbchange)",
}

var probeHostRe = regexp.MustCompile(`https://([A-Za-z0-9.$_{}+-]+)`)

// The token of the workflow goes only to the hosts named above, each a
// constant of the script: not a value of the environment, of an input of
// the run or of an answer of the platform; and a request does not follow a
// redirect — the token would go where the answer points (risk note 183,
// У1).
func TestWorkflows_ProbeHostsAreDeclared(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	seen := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		for _, m := range probeHostRe.FindAllStringSubmatch(l, -1) {
			h := m[1]
			if !strings.HasSuffix(h, "api.cloud.ru") {
				// Not an API of Cloud.ru: the token must not be on the line.
				if strings.Contains(l, "$TOKEN") || strings.Contains(l, "Authorization") {
					t.Errorf("cloud-live.sh sends the token to %q: not an API host of Cloud.ru", h)
				}
				continue
			}
			seen[h] = true
			if _, ok := probeHosts[h]; !ok {
				t.Errorf("cloud-live.sh has a request to %q, which is not in probeHosts: a new host of a workflow with a secret needs a note of the risk role", h)
			}
		}
	}
	var gone []string
	for h := range probeHosts {
		if !seen[h] {
			gone = append(gone, h)
		}
	}
	sort.Strings(gone)
	for _, h := range gone {
		t.Errorf("probeHosts names %q, which cloud-live.sh no longer calls: remove it", h)
	}
	// The one variable host is chosen from constants.
	if !regexp.MustCompile(`pg\) g=Г-13 host=postgresql ;;\s*kafka\) g=Г-14 host=kafka ;;\s*redis\) g=Г-15 host=redis ;;`).MatchString(s) {
		t.Error("cloud-live.sh: $host of area_dbchange is not chosen by the case of three constants")
	}
	if !strings.Contains(s, "\nRM=https://resource-manager.api.cloud.ru/v1\n") {
		t.Error("cloud-live.sh: RM is not the constant of the published spec")
	}
	// No request follows a redirect.
	for i, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") || !strings.Contains(l, "curl ") {
			continue
		}
		if regexp.MustCompile(`curl [^|]*(\s-[A-Za-z]*L[A-Za-z]*\s|--location)`).MatchString(l) {
			t.Errorf("cloud-live.sh:%d: curl follows redirects: the token would go where the answer points", i+1)
		}
	}
}

// The sweep of the stand (e2e_stand.sh) sends the token of the workflow
// too: only to hosts of probeHosts, and no request of it follows a
// redirect. The tag of the stand (Вп-3) brought it the host of Resource
// Manager, the same as the probe Г-17's (risk note 183).
func TestWorkflows_StandHostsAreDeclared(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "e2e_stand.sh"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for i, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		for _, m := range probeHostRe.FindAllStringSubmatch(l, -1) {
			h := m[1]
			if !strings.HasSuffix(h, "api.cloud.ru") {
				if strings.Contains(l, "TOKEN") || strings.Contains(l, "Authorization") {
					t.Errorf("e2e_stand.sh:%d sends the token to %q: not an API host of Cloud.ru", i+1, h)
				}
				continue
			}
			n++
			if _, ok := probeHosts[h]; !ok {
				t.Errorf("e2e_stand.sh:%d has a request to %q, which is not in probeHosts", i+1, h)
			}
		}
		if strings.Contains(l, "curl ") && regexp.MustCompile(`curl [^|]*(\s-[A-Za-z]*L[A-Za-z]*\s|--location)`).MatchString(l) {
			t.Errorf("e2e_stand.sh:%d: curl follows redirects: the token would go where the answer points", i+1)
		}
	}
	if n < 3 {
		t.Fatalf("e2e_stand.sh: %d hosts of Cloud.ru found — the check sees nothing", n)
	}
}
