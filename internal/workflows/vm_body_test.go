// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// jsonTags are the names of the JSON fields of a struct.
func jsonTags(v any) map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

// vmBodyKey is a key of a jq or JSON object: `key:` or `"key":`.
var vmBodyKey = regexp.MustCompile(`"?([a-z_]+)"?\s*:`)

// Run 181 (10.11): the probe cmp7 created its VM by a direct POST
// /api/v1.1/vms with availability_zone_name and subnet_name — fields the VM
// API does not take — and got 422 with no text; P41 met the same in rc 1.6.
// A direct POST of a VM in cloud-live.sh goes through vm_post, and every
// field it sends is a field of the body evo itself sends
// (provision.CreateVMRequest, its disks and interfaces).
func TestScripts_DirectVMBodyIsEvos(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	allowed := jsonTags(provision.CreateVMRequest{})
	for k := range jsonTags(provision.DiskReq{}) {
		allowed[k] = true
	}
	for k := range jsonTags(provision.InterfaceReq{}) {
		allowed[k] = true
	}
	if n := strings.Count(s, "POST https://compute.api.cloud.ru/api/v1.1/vms"); n != 1 {
		t.Errorf("cloud-live.sh: %d direct POSTs of a VM, want one, in vm_post", n)
	}
	start := strings.Index(s, "\nvm_post() {")
	if start < 0 {
		t.Fatal("cloud-live.sh: no vm_post")
	}
	body := s[start:]
	body = body[:strings.Index(body, "\n}\n")]
	// The fields are those of the jq filter, not the URL before it.
	if i := strings.Index(body, "'[{"); i >= 0 {
		body = body[i:]
	}
	// The probes' own interfaces (P41's direct_ip) are literals at the call.
	var lits []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "vm_post ") || strings.HasPrefix(strings.TrimSpace(l), "regular_if()") {
			lits = append(lits, l)
		}
	}
	keys := vmBodyKey.FindAllStringSubmatch(body+"\n"+strings.Join(lits, "\n"), -1)
	if len(keys) < 8 {
		t.Fatalf("%d fields of the VM body read: the scan sees nothing", len(keys))
	}
	for _, k := range keys {
		if !allowed[k[1]] {
			t.Errorf("cloud-live.sh sends %q in the body of a VM: not a field evo sends (provision.CreateVMRequest)", k[1])
		}
	}
}
