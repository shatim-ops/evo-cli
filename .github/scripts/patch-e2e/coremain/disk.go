// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/http"
	"net/url"
	"strings"
)

// The second cycle deliberately uses its own VM as the disk attachment. One
// invocation of the normal CLI gets exactly one mutation, not the next step.
func (c cycle) diskAction(r *http.Request, p []string, q url.Values, b []byte) (route, error) {
	var z route
	s := c.row("scope")
	proj := str(s, "project_id")
	vm := c.row("vm")
	boot := c.row("bootdisk")
	if c.g.prefix != "core.2." || !c.scopeOK() || !owned(vm, id(vm)) || !bit(c.row("lifecycle"), "application_verified") || p[1] != "v1" {
		return z, denied
	}
	if len(q) > 1 || len(q) == 1 && (len(q["project_id"]) != 1 || q.Get("project_id") != proj) {
		return z, denied
	}
	data, img := c.row("data-disk"), c.row("new-image")
	name := str(s, "vm_name")
	step := c.g.diskStep
	if step == "delete-data" || step == "delete-image" {
		if r.Method != "DELETE" || len(b) != 0 || len(p) != 4 {
			return z, denied
		}
		if step == "delete-data" && p[2] == "disks" && owned(data, p[3]) {
			return route{kind: "disk", key: "data-disk", mode: "delete"}, nil
		}
		if step == "delete-image" && p[2] == "images" && owned(img, p[3]) {
			return route{kind: "image", key: "new-image", mode: "delete"}, nil
		}
		return z, denied
	}
	v, e := jsonBody(b)
	if e != nil {
		return z, denied
	}
	m, _ := v.(map[string]any)
	post := r.Method == http.MethodPost
	switch step {
	case "create-data":
		if !post || len(p) != 3 || p[2] != "disks" || !keys(m, "project_id", "availability_zone_id", "name", "size", "disk_type_name") || str(m, "project_id") != proj || str(m, "availability_zone_id") != str(s, "zone_id") || str(m, "name") != name+"-data" || integer(m, "size") != 10 || str(m, "disk_type_name") != str(s, "disk_type_name") {
			return z, denied
		}
		z = route{kind: "disk", key: "data-disk", mode: "create", record: object{"name": name + "-data", "vm_id": id(vm)}}
	case "resize-data":
		if r.Method != "PUT" || len(p) != 4 || p[2] != "disks" || !owned(data, p[3]) || !keys(m, "size") || integer(m, "size") != 20 {
			return z, denied
		}
	case "attach-data", "detach-data", "detach-boot", "restore-boot":
		if !post || len(p) != 5 || p[2] != "disks" || !keys(m, "vm_id") || !owned(vm, str(m, "vm_id")) {
			return z, denied
		}
		k, action := data, "attach"
		if strings.HasSuffix(step, "boot") {
			k = boot
			if str(boot, "vm_id") != id(vm) {
				return z, denied
			}
		}
		if strings.HasPrefix(step, "detach") {
			action = "detach"
		}
		if p[4] != action || !owned(k, p[3]) {
			return z, denied
		}
	case "stop-for-image":
		if !post || len(p) != 5 || p[2] != "vms" || !owned(vm, p[3]) || p[4] != "set-power" || !keys(m, "state") || str(m, "state") != "power_off" {
			return z, denied
		}
	case "create-image":
		if !post || len(p) != 3 || p[2] != "images" || !keys(m, "project_id", "name", "disk_id", "description", "availability_zones") || !owned(boot, str(m, "disk_id")) || str(boot, "vm_id") != id(vm) || str(m, "project_id") != proj || str(m, "name") != name+"-image" || str(m, "description") != name+"-image" {
			return z, denied
		}
		az, ok := m["availability_zones"].([]any)
		if !ok || len(az) != 1 {
			return z, denied
		}
		a, _ := az[0].(map[string]any)
		if !keys(a, "availability_zone_id") || str(a, "availability_zone_id") != str(s, "zone_id") {
			return z, denied
		}
		z = route{kind: "image", key: "new-image", mode: "create", record: object{"name": name + "-image", "source_disk_id": id(boot)}}
	default:
		return z, denied
	}
	if z.mode == "" {
		z = route{kind: "checkpoint", key: "disk-op-" + step, mode: "single-change", record: object{}}
	}
	if bit(c.row(z.key), "post_sent") || id(c.row(z.key)) != "" {
		return route{}, denied
	}
	return z, nil
}
