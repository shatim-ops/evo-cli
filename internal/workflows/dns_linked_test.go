// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// dnsLinkedText is the English text of the refusal D66 of dns_zone_delete
// in the source of the tool: the text --json answers in any language of
// the CLI (D6, clarified 14.11).
var dnsLinkedText = regexp.MustCompile(`i18n\.Tf\(\s*"(DNS zone %v is linked[^"]*)"`)

// dns_linked of cloud-live.sh knows the refusal D66 by the text evo
// answers with --json. Run 234 (19.11): the check looked for a Russian
// word, --json answered in English since #1965, the zone was not unlinked
// and stayed after the run and the sweep.
func TestScripts_DNSLinkedKnowsTheRefusalOfEvo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash script: CI runs it on Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	root := filepath.Join("..", "..")
	script, err := os.ReadFile(filepath.Join(root, ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var fn string
	for _, l := range strings.Split(string(script), "\n") {
		if strings.HasPrefix(l, "dns_linked() {") {
			fn = l
		}
	}
	if fn == "" {
		t.Fatal("cloud-live.sh: no dns_linked() on one line — update the test")
	}
	src, err := os.ReadFile(filepath.Join(root, "internal", "tools", "tools_dns.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := dnsLinkedText.FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("tools_dns.go: no English text of the refusal D66 — update the test")
	}
	refusal := fmt.Sprintf(m[1], "zone-1", `"vpc-a" (vpc-1)`)
	for _, c := range []struct {
		rc, reason, why string
		linked          bool
	}{
		{"1", "USAGE", refusal, true},
		{"1", "USAGE, retry: false", refusal, true},
		{"1", "USAGE", "missing required argument id", false},
		{"5", "API_ERROR", refusal, false},
		{"0", "", "", false},
	} {
		cmd := exec.Command("bash", "-c", fn+"\nrc=$1 reason=$2 why=$3\ndns_linked", "dns_linked", c.rc, c.reason, c.why)
		err := cmd.Run()
		if got := err == nil; got != c.linked {
			t.Errorf("dns_linked with rc %s, reason %q, why %q: %v, want %v", c.rc, c.reason, c.why, got, c.linked)
		}
	}
}
