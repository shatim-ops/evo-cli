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

	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// jsonTags are the names of the JSON fields of a struct; required are
// those evo always sends (no omitempty).
func jsonTags(v any) (all, required map[string]bool) {
	all, required = map[string]bool{}, map[string]bool{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			all[name] = true
			if !strings.Contains(opts, "omitempty") {
				required[name] = true
			}
		}
	}
	return all, required
}

// vmBodyProbed — the fields of the direct POST of a VM that evo does not
// send: the probe that sends each exists to learn what the platform does
// with it.
var vmBodyProbed = map[string]string{
	"tag_ids": "Г-17: the tag of a VM in the request that creates it (plan 1.9, wave В6)",
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
	// The fields of the VM and of its disk: the interfaces are the
	// probe's (regular_if, P41's direct_ip).
	allowed, required := jsonTags(provision.CreateVMRequest{})
	disk, diskReq := jsonTags(provision.DiskReq{})
	iface, _ := jsonTags(provision.InterfaceReq{})
	for _, m := range []map[string]bool{disk, iface} {
		for k := range m {
			allowed[k] = true
		}
	}
	for k := range diskReq {
		required[k] = true
	}
	// omitempty in evo's body, but the images of the probes declare
	// mandatory fields: without it the API answers 422
	// image_metadata_fields_missing (run 183, cmp7).
	required["image_metadata"] = true
	// A field a probe is about is not evo's yet: it is taken from the
	// published spec of Compute (PublicVmCreateRequest) and named here with
	// its probe. Closed list.
	for k := range vmBodyProbed {
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
	if !strings.Contains(body, "POST https://compute.api.cloud.ru/api/v1.1/vms") {
		t.Error("cloud-live.sh: the direct POST of a VM is not in vm_post")
	}
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
	sent := map[string]bool{}
	for _, k := range vmBodyKey.FindAllStringSubmatch(body, -1) {
		sent[k[1]] = true
	}
	for k := range required {
		if !sent[k] {
			t.Errorf("vm_post does not send %q, which evo always sends (provision.CreateVMRequest)", k)
		}
	}
	for _, k := range keys {
		if !allowed[k[1]] {
			t.Errorf("cloud-live.sh sends %q in the body of a VM: not a field evo sends (provision.CreateVMRequest)", k[1])
		}
	}
}

// vmMetaField is a field of an image_get field that vm_meta reads: .name,
// .role, .default_value.
var vmMetaField = regexp.MustCompile(`\.(name|role|filled_by|default[a-z_]*)\b`)

// Run 184 (10.11): vm_meta read .default, which image_get does not answer
// (images.FieldInfo has default_value), so a field the image fills by its
// default went out empty. Every field of a field of image_get that vm_meta
// reads is a JSON field of images.FieldInfo.
func TestScripts_VMMetaReadsImageGetFields(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "\nvm_meta() {")
	if i < 0 {
		t.Fatal("cloud-live.sh: no vm_meta")
	}
	body := s[i:]
	body = body[:strings.Index(body, "\n}\n")]
	tags, _ := jsonTags(images.FieldInfo{})
	seen := 0
	for _, m := range vmMetaField.FindAllStringSubmatch(body, -1) {
		seen++
		if !tags[m[1]] {
			t.Errorf("vm_meta reads .%s of a field of image_get: not a field of images.FieldInfo", m[1])
		}
	}
	if seen < 3 {
		t.Fatalf("%d fields of image_get read in vm_meta: the scan sees nothing", seen)
	}
}
