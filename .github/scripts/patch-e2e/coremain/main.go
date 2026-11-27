// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

// This process-only entry point runs the exact candidate CLI and MCP with a
// transport that persists evidence before allowing its caller to use a result.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/shatim-ops/evo-cli/internal/cli"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const candidate = "d7645da5efea04122f984cfd8cb9a28408d08177"
const maxBody = 8 << 20

var holdRule = errors.New("P-90: injected local interruption before removal of this run’s rule")

var denied = errors.New("P-90: request or ownership evidence outside the approved cycle")
var idFormat = regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$")
var markerFormat = regexp.MustCompile("^evo-fip-[A-Z2-7a-z0-9]{20,64}$")

type object = map[string]any
type ledger map[string]object
type guard struct {
	abort            func()
	cut              func()
	validateCutState func(cycle) bool
	holdRule         bool
	planApply        bool
	localPublicKey   func() ([]byte, error)
	diskStep         string
	runnerCIDR       string
	base             http.RoundTripper
	prefix           string
	readOnly         bool
	load             func() (ledger, error)
	save             func(kind, key string, fields object) error
	lock             func() (func(), error)
	mu               sync.Mutex
}

func str(m object, k string) string { s, _ := m[k].(string); return s }
func integer(m object, k string) int {
	n, ok := m[k].(float64)
	if !ok || n != float64(int(n)) {
		return 0
	}
	return int(n)
}
func bit(m object, k string) bool { yes, _ := m[k].(bool); return yes }
func keys(m object, allowed ...string) bool {
	for k := range m {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return m != nil
}
func ref(v any) string {
	switch a := v.(type) {
	case string:
		return a
	case map[string]any:
		return str(a, "id")
	}
	return ""
}
func firstObject(v any) (object, bool) {
	m, ok := v.(map[string]any)
	if ok {
		return m, true
	}
	rows, ok := v.([]any)
	if !ok || len(rows) != 1 {
		return nil, false
	}
	m, ok = rows[0].(map[string]any)
	return m, ok
}
func jsonBody(b []byte) (any, error) { var v any; e := json.Unmarshal(b, &v); return v, e }
func body(r io.ReadCloser) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, maxBody+1))
	if len(b) > maxBody || e != nil {
		return nil, denied
	}
	return b, nil
}
func putBack(b []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(b)) }
func id(m object) string             { return str(m, "id") }
func owned(m object, s string) bool {
	return idFormat.MatchString(s) && id(m) == s && !bit(m, "confirmed_absent")
}
func address(s string) bool {
	ip, e := netip.ParseAddr(s)
	return e == nil && ip.Is4() && ip.IsGlobalUnicast() && !ip.IsPrivate()
}

type cycle struct {
	g    *guard
	rows ledger
}

func (c cycle) row(k string) object { return c.rows[c.g.prefix+k] }
func (c cycle) record(kind, key string, f object) error {
	if e := c.g.save(kind, c.g.prefix+key, f); e != nil {
		return denied
	}
	key = c.g.prefix + key
	if c.rows[key] == nil {
		c.rows[key] = object{}
	}
	for k, v := range f {
		c.rows[key][k] = v
	}
	return nil
}
func (c cycle) witness(kind, key string, f object) error {
	old := c.row(key)
	for k, v := range f {
		if p, ok := old[k]; ok && p != v {
			return denied
		}
	}
	for k := range f {
		if _, ok := old[k]; !ok {
			return c.record(kind, key, f)
		}
	}
	return nil
}

// Validate the independently configured public runner address and pin it to the
// receipt. IsGlobalUnicast alone includes private and protocol-reserved space.
func publicRunner(s string) bool {
	p, err := netip.ParsePrefix(s)
	if err != nil || p.Bits() != 32 || p.String() != s || !address(p.Addr().String()) {
		return false
	}
	a := p.Addr()
	for _, reserved := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
		if netip.MustParsePrefix(reserved).Contains(a) {
			return false
		}
	}
	return true
}

func (c cycle) scopeOK() bool {
	s := c.row("scope")
	for _, k := range []string{"project_id", "zone_id", "sg_id", "subnet_id", "image_id", "flavor_id"} {
		if !idFormat.MatchString(str(s, k)) {
			return false
		}
	}
	for _, k := range []string{"vm_name", "image_name", "flavor_name", "disk_type_name"} {
		if str(s, k) == "" {
			return false
		}
	}
	group := c.row("group")
	if !owned(group, str(s, "sg_id")) || !bit(group, "post_sent") {
		return false
	}
	p := integer(s, "port")
	return (p == 18085 || p == 18086) && integer(s, "disk_cap_gb") > 0 && str(s, "cidr") == c.g.runnerCIDR && publicRunner(c.g.runnerCIDR)
}

var planID = regexp.MustCompile("^plan-[a-f0-9]{12}$")

func (c cycle) planMode() bool {
	return c.g.prefix == "core.2." && str(c.row("scope"), "creation_mode") == "plan" &&
		planID.MatchString(str(c.row("scope"), "plan_id")) &&
		str(c.row("scope"), "boot_name") == provision.FitName(str(c.row("scope"), "vm_name"), "-disk")
}

func (c cycle) bootName() string {
	if c.planMode() {
		return str(c.row("scope"), "boot_name")
	}
	return provision.FitName(str(c.row("scope"), "vm_name"), "-boot")
}

// SSH trust comes from the public half made in this cycle, never from the VM
// response or a catalog. The fingerprint in an approved saved plan is public.
func (c cycle) trustedMeta(m object) bool {
	if !keys(m, "public_key", "name", "hostname", "fqdn") || c.g.localPublicKey == nil {
		return false
	}
	raw, err := c.g.localPublicKey()
	if err != nil || len(raw) > 4096 {
		return false
	}
	parsed := strings.Fields(strings.TrimSpace(string(raw)))
	wire := strings.Fields(str(m, "public_key"))
	if len(parsed) < 2 || len(wire) < 2 || parsed[0] != "ssh-ed25519" || parsed[0] != wire[0] || parsed[1] != wire[1] {
		return false
	}
	if c.g.planApply {
		sum := sha256.Sum256([]byte(wire[0] + " " + wire[1]))
		return c.planMode() && str(c.row("scope"), "ssh_key_fingerprint") == hex.EncodeToString(sum[:])
	}
	return true
}

type route struct {
	kind, key, mode string
	record          object
}

func (c cycle) route(r *http.Request, p []string, q url.Values, b []byte) (route, error) {
	var z route
	s := c.row("scope")
	proj := str(s, "project_id")
	vm := c.row("vm")
	fip := c.row("fip")
	sg := str(s, "sg_id")
	if vals, ok := q["project_id"]; ok && (len(vals) != 1 || vals[0] != proj) {
		return z, denied
	}
	if len(p) < 3 || p[0] != "api" {
		return z, denied
	}
	if r.Method == http.MethodGet {
		if len(b) != 0 || p[1] != "v1" {
			return z, denied
		}
		for k, v := range q {
			if len(v) != 1 {
				return z, denied
			}
			switch k {
			case "project_id", "offset", "limit", "name", "availability_zone_id", "sort", "sort_by", "type":
			default:
				return z, denied
			}
		}
		if len(p) == 3 {
			switch p[2] {
			case "floating-ips", "vms", "disks", "interfaces", "flavors", "images", "availability-zones", "disk-types", "subnets", "security-groups":
				return route{mode: "list:" + p[2]}, nil
			}
		}
		if len(p) == 4 {
			switch p[2] {
			case "floating-ips":
				if owned(fip, p[3]) {
					return route{mode: "card:fip"}, nil
				}
			case "vms":
				if owned(vm, p[3]) {
					return route{mode: "card:vm"}, nil
				}
			case "images":
				if p[3] == str(s, "image_id") || owned(c.row("new-image"), p[3]) {
					return z, nil
				}
			case "security-groups":
				if p[3] == sg {
					return z, nil
				}
			case "disks":
				if owned(c.row("data-disk"), p[3]) {
					return route{mode: "card:data"}, nil
				}
				if owned(c.row("bootdisk"), p[3]) {
					return route{mode: "card:disk"}, nil
				}
			}
		}
		if len(p) == 5 && p[2] == "security-groups" && p[3] == sg && p[4] == "rules" {
			return z, nil
		}
		return z, denied
	}
	// The candidate's price POST only evaluates a quote.
	if r.Method == "POST" && len(p) == 5 && p[1] == "v1" && p[2] == "projects" && p[3] == proj && p[4] == "price-calculation" && len(q) == 0 {
		v, e := jsonBody(b)
		m, _ := v.(map[string]any)
		if e != nil || !keys(m, "total_count", "flavor_id", "image_id", "disks", "floating_ips") || integer(m, "total_count") != 1 {
			return z, denied
		}
		return z, nil
	}
	if c.g.readOnly {
		return z, denied
	}
	for k := range q {
		if k != "project_id" {
			return z, denied
		}
	}
	if c.g.planApply && (!c.planMode() || c.g.diskStep != "") {
		return z, denied
	}
	if c.g.diskStep != "" {
		return c.diskAction(r, p, q, b)
	}
	if r.Method == "DELETE" {
		if len(b) != 0 || p[1] != "v1" {
			return z, denied
		}
		if len(p) == 4 {
			switch p[2] {
			case "floating-ips":
				if owned(fip, p[3]) {
					return route{kind: "fip", key: "fip", mode: "delete"}, nil
				}
			case "vms":
				if owned(vm, p[3]) {
					return route{kind: "vm", key: "vm", mode: "delete"}, nil
				}
			}
		}
		if len(p) == 6 && p[2] == "security-groups" && p[3] == sg && p[4] == "rules" && owned(c.row("app-rule"), p[5]) {
			return route{kind: "rule", key: "app-rule", mode: "delete"}, nil
		}
		return z, denied
	}
	v, e := jsonBody(b)
	if e != nil {
		return z, denied
	}
	m, _ := v.(map[string]any)
	if r.Method == "PUT" && len(p) == 4 && p[1] == "v1" && p[2] == "floating-ips" && owned(fip, p[3]) && owned(c.row("iface"), str(m, "interface_id")) && str(m, "name") == str(fip, "name") && keys(m, "name", "interface_id") {
		return route{mode: "attach"}, nil
	}
	if r.Method != "POST" {
		return z, denied
	}
	switch {
	case len(p) == 3 && p[1] == "v1" && p[2] == "floating-ips":
		n := str(m, "name")
		marker := str(m, "description")
		plan := c.g.planApply && c.planMode()
		goodCreate := keys(m, "name", "description", "project_id", "availability_zone_id") && provision.IsBootFIPOf(n, str(s, "vm_name")) && !c.planMode()
		if plan {
			face := c.row("iface")
			goodCreate = keys(m, "name", "description", "project_id", "availability_zone_id", "interface_id") &&
				n == provision.FitName(str(s, "vm_name"), "-fip") && owned(vm, str(face, "vm_id")) &&
				owned(face, str(m, "interface_id")) && bit(face, "plan_unbound_scope_verified")
		}
		if !goodCreate || !markerFormat.MatchString(marker) || str(m, "project_id") != proj || str(m, "availability_zone_id") != str(s, "zone_id") {
			return z, denied
		}
		z = route{kind: "fip", key: "fip", mode: "create", record: object{"name": n, "marker": marker}}
	case len(p) == 3 && p[1] == "v1.1" && p[2] == "vms":
		a, ok := v.([]any)
		if !ok || len(a) != 1 {
			return z, denied
		}
		m, _ = a[0].(map[string]any)
		if !keys(m, "project_id", "name", "flavor_name", "availability_zone_id", "image_name", "image_metadata", "disks", "interfaces") || str(m, "project_id") != proj || str(m, "name") != str(s, "vm_name") || str(m, "image_name") != str(s, "image_name") || str(m, "flavor_name") != str(s, "flavor_name") || str(m, "availability_zone_id") != str(s, "zone_id") {
			return z, denied
		}
		meta, _ := m["image_metadata"].(map[string]any)
		if !c.trustedMeta(meta) {
			return z, denied
		}
		for key, value := range meta {
			t, ok := value.(string)
			if !ok || t == "" || len(t) > 4096 || strings.ContainsAny(t, "\n\r\x00") {
				return z, denied
			}
			if (key == "hostname" || key == "fqdn") && t != str(s, "vm_name") {
				return z, denied
			}
		}
		if !strings.HasPrefix(str(meta, "public_key"), "ssh-ed25519 ") {
			return z, denied
		}
		ds, dok := m["disks"].([]any)
		ns, nok := m["interfaces"].([]any)
		if !dok || len(ds) != 1 || !nok || len(ns) != 1 {
			return z, denied
		}
		d, _ := ds[0].(map[string]any)
		n, _ := ns[0].(map[string]any)
		groups, _ := n["security_groups"].([]any)
		if !keys(d, "name", "size", "disk_type_name") || str(d, "name") != c.bootName() || str(d, "disk_type_name") != str(s, "disk_type_name") || integer(d, "size") < 1 || integer(d, "size") > integer(s, "disk_cap_gb") {
			return z, denied
		}
		validBind := owned(fip, str(n, "attach_external_ip_id")) && !c.planMode()
		if c.g.planApply {
			_, has := n["attach_external_ip_id"]
			validBind = c.planMode() && !has && !bit(fip, "post_sent")
		}
		if !keys(n, "type", "subnet_id", "interface_security_enabled", "security_groups", "attach_external_ip_id") || str(n, "type") != "regular" || str(n, "subnet_id") != str(s, "subnet_id") || !bit(n, "interface_security_enabled") || len(groups) != 1 || groups[0] != sg || !validBind {
			return z, denied
		}
		z = route{kind: "vm", key: "vm", mode: "create", record: object{}}
	case len(p) == 5 && p[1] == "v1" && p[2] == "security-groups" && p[3] == sg && p[4] == "rules":
		pa := strconv.Itoa(integer(s, "port"))
		lbl := sgrules.RuleLabel(pa, id(vm))
		if !owned(vm, id(vm)) || !keys(m, "direction", "ether_type", "ip_protocol", "port_range", "remote_ip_prefix", "description") || str(m, "direction") != "ingress" || str(m, "ether_type") != "IPv4" || str(m, "ip_protocol") != "tcp" || str(m, "port_range") != pa+":"+pa || str(m, "remote_ip_prefix") != str(s, "cidr") || str(m, "description") != lbl {
			return z, denied
		}
		z = route{kind: "rule", key: "app-rule", mode: "create", record: object{"marker": lbl}}
	default:
		return z, denied
	}
	if old := c.row(z.key); old != nil && (bit(old, "post_sent") || id(old) != "") {
		return route{}, denied
	}
	return z, nil
}

func validPage(v any) ([]any, error) {
	if list, ok := v.([]any); ok {
		return list, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, denied
	}
	list, ok := m["items"].([]any)
	if !ok {
		return nil, denied
	}
	return list, nil
}

func (c cycle) disk(m object) error {
	if str(m, "name") != c.bootName() {
		return nil
	}
	vms, ok := m["vms"].([]any)
	if !ok || len(vms) != 1 {
		return nil
	}
	v, _ := vms[0].(map[string]any)
	vm := c.row("vm")
	if !owned(vm, id(v)) || !bit(v, "primary") {
		return nil
	}
	if !idFormat.MatchString(id(m)) {
		return denied
	}
	return c.witness("disk", "bootdisk", object{"id": id(m), "vm_id": id(vm)})
}
func (c cycle) markerEvidence(m object, stage string) error {
	f := c.row("fip")
	a, ok := m["description"].(string)
	fields := object{stage + "_marker_present": ok && a != "", stage + "_marker_exact": ok && a != "" && a == str(f, "marker")}
	if prior, ok := f[stage+"_marker_exact"]; ok && prior != fields[stage+"_marker_exact"] {
		// Once an inconsistency has occurred the evidence cannot become reliable by a retry.
		return denied
	}
	return c.witness("fip", "fip", fields)
}
func (c cycle) fipCard(m object) error {
	f := c.row("fip")
	if !owned(f, id(m)) {
		return denied
	}
	if n := str(m, "name"); n != "" && n != str(f, "name") {
		return denied
	}
	if z := ref(m["availability_zone"]); z != "" && z != str(c.row("scope"), "zone_id") {
		return denied
	}
	if mk := str(m, "description"); mk != "" && mk != str(f, "marker") {
		return denied
	}
	if v := ref(m["vm"]); v != "" && !owned(c.row("vm"), v) {
		return denied
	}
	if v := ref(m["interface"]); v != "" && id(c.row("iface")) != "" && !owned(c.row("iface"), v) {
		return denied
	}
	if ref(m["nat_gateway"]) != "" {
		return denied
	}
	if ip := str(m, "ip_address"); ip != "" {
		if !address(ip) {
			return denied
		}
		if e := c.witness("fip", "fip", object{"ip_address": ip}); e != nil {
			return e
		}
	}
	return nil
}
func (c cycle) vmCard(m object) error {
	vm, f, s := c.row("vm"), c.row("fip"), c.row("scope")
	if !owned(vm, id(m)) {
		return denied
	}
	if n := str(m, "name"); n != "" && n != str(s, "vm_name") {
		return denied
	}
	for _, k := range []string{"floating_ip", "public_ip"} {
		if ip := str(m, k); ip != "" && ip != str(f, "ip_address") {
			return denied
		}
	}
	active := str(m, "state") == "active" || str(m, "state") == "running" || str(m, "status") == "active" || str(m, "status") == "running"
	matched := false
	ifaces, _ := m["interfaces"].([]any)
	if len(ifaces) > 1 {
		return denied
	}
	for _, raw := range ifaces {
		i, _ := raw.(map[string]any)
		if i == nil || str(i, "type") == "direct_ip" {
			return denied
		}
		if g, ok := i["security_groups"].([]any); ok && (len(g) != 1 || ref(g[0]) != str(s, "sg_id")) {
			return denied
		}
		if sn := ref(i["subnet"]); sn != "" && sn != str(s, "subnet_id") {
			return denied
		}
		if bit(i, "primary") && idFormat.MatchString(id(i)) {
			if err := c.witness("interface", "iface", object{"id": id(i), "vm_id": id(vm)}); err != nil {
				return err
			}
		}
		if bound, ok := i["floating_ip"].(map[string]any); ok {
			ip := str(bound, "ip_address")
			if !owned(f, id(bound)) || !address(ip) || !bit(i, "primary") {
				return denied
			}
			if err := c.witness("fip", "fip", object{"ip_address": ip, "bound_vm_id": id(vm)}); err != nil {
				return err
			}
			matched = true
		}
	}
	// An active VM card without the run's FIP/primary interface cannot vouch
	// for a direct SSH connection. Delay accepting it until Cloud confirms it.
	if active && !matched {
		if !c.g.planApply || !c.planMode() || bit(f, "post_sent") || len(ifaces) != 1 {
			return denied
		}
		i, _ := ifaces[0].(map[string]any)
		groups, _ := i["security_groups"].([]any)
		if str(i, "type") != "regular" || !bit(i, "primary") || !owned(c.row("iface"), id(i)) ||
			ref(i["subnet"]) != str(s, "subnet_id") || len(groups) != 1 || ref(groups[0]) != str(s, "sg_id") || i["floating_ip"] != nil || str(m, "public_ip") != "" || str(m, "floating_ip") != "" {
			return denied
		}
		if e := c.witness("interface", "iface", object{"plan_unbound_scope_verified": true}); e != nil {
			return e
		}
	}
	// Some API revisions return the disks nested on the VM. Only record a
	// primary boot disk on the just-proven VM; list/card is also checked.
	ds, _ := m["disks"].([]any)
	for _, v := range ds {
		d, _ := v.(map[string]any)
		if len(ds) == 1 && bit(d, "primary") && str(d, "name") == c.bootName() && idFormat.MatchString(id(d)) {
			if err := c.witness("disk", "bootdisk", object{"id": id(d), "vm_id": id(vm)}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c cycle) inspect(rt route, v any) error {
	s, vm, f := c.row("scope"), c.row("vm"), c.row("fip")
	if rt.mode == "create" {
		m, ok := firstObject(v)
		if !ok || !idFormat.MatchString(id(m)) {
			return denied
		}
		for _, field := range []string{"operation_id", "operation_type", "done", "error", "action_type", "response"} {
			if _, ok := m[field]; ok {
				return denied
			}
		}
		if rt.kind == "disk" || rt.kind == "image" {
			// Do not interpret an asynchronous operation ID as the resource ID.
			if str(m, "name") != str(c.row(rt.key), "name") {
				return denied
			}
			if p := str(m, "project_id"); p != "" && p != str(s, "project_id") {
				return denied
			}
		}
		// Only the response to the corresponding request can introduce its ID.
		if e := c.witness(rt.kind, rt.key, object{"id": id(m)}); e != nil {
			return e
		}
		switch rt.kind {
		case "vm":
			return c.vmCard(m)
		case "fip":
			if e := c.markerEvidence(m, "post"); e != nil {
				return e
			}
			return c.fipCard(m)
		case "rule", "disk", "image":
			return nil
		}
	}
	m, _ := v.(map[string]any)
	switch rt.mode {
	case "card:vm":
		return c.vmCard(m)
	case "card:fip":
		if e := c.markerEvidence(m, "card"); e != nil {
			return e
		}
		return c.fipCard(m)
	case "card:data":
		if !owned(c.row("data-disk"), id(m)) {
			return denied
		}
		return nil
	case "card:disk":
		if !owned(c.row("bootdisk"), id(m)) {
			return denied
		}
		return c.disk(m)
	case "list:vms", "list:floating-ips", "list:disks", "list:images":
		rows, err := validPage(v)
		if err != nil {
			return err
		}
		for _, raw := range rows {
			m, ok := raw.(map[string]any)
			if !ok {
				return denied
			}
			switch rt.mode {
			case "list:vms":
				if str(m, "name") == str(s, "vm_name") && !owned(vm, id(m)) {
					// An exact name in a list cannot prove which caller created a VM.
					return denied
				}
			case "list:floating-ips":
				if (provision.IsBootFIPOf(str(m, "name"), str(s, "vm_name")) || c.planMode() && str(m, "name") == provision.FitName(str(s, "vm_name"), "-fip")) && !owned(f, id(m)) {
					return denied
				}
				if id(f) != "" && id(m) == id(f) {
					if e := c.markerEvidence(m, "list"); e != nil {
						return e
					}
					if e := c.fipCard(m); e != nil {
						return e
					}
				}
			case "list:images":
				if rec := c.row("new-image"); bit(rec, "post_sent") && str(rec, "name") == str(m, "name") && !owned(rec, id(m)) {
					return denied
				}
			case "list:disks":
				if rec := c.row("data-disk"); bit(rec, "post_sent") && str(rec, "name") == str(m, "name") && !owned(rec, id(m)) {
					return denied
				}
				if e := c.disk(m); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func (g *guard) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := g.roundTrip(r)
	// The candidate treats some lookup errors as advisory and could continue
	// to raw SSH. A harness refusal must terminate the command before then.
	if errors.Is(err, denied) && g.abort != nil {
		g.abort()
	}
	return resp, err
}
func (g *guard) roundTrip(r *http.Request) (*http.Response, error) {
	// Non-cloud I/O is either the explicitly verified test web server, or
	// outside these endpoints. Redirects re-enter this check.
	u := r.URL
	if u == nil || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || r.Host != "" && r.Host != u.Host {
		return nil, denied
	}
	if u.Scheme == "https" && u.Host == "iam.api.cloud.ru" && u.Path == "/api/v1/auth/token" && u.RawQuery == "" && r.Method == "POST" {
		return g.base.RoundTrip(r)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if (u.Scheme == "http" || u.Scheme == "https") && u.Port() != "" {
		rows, e := g.load()
		if e != nil {
			return nil, denied
		}
		c := cycle{g, rows}
		f, vm, s := c.row("fip"), c.row("vm"), c.row("scope")
		if c.scopeOK() && address(str(f, "ip_address")) && u.Hostname() == str(f, "ip_address") && u.Port() == strconv.Itoa(integer(s, "port")) && owned(vm, str(f, "bound_vm_id")) && !bit(f, "confirmed_absent") && r.Header.Get("Authorization") == "" && r.Header.Get("Cookie") == "" && r.Header.Get("Proxy-Authorization") == "" && r.Method == "GET" && u.Path == "/" && u.RawQuery == "" {
			return g.base.RoundTrip(r)
		}
		return nil, denied
	}
	if u.Scheme != "https" || u.Host != "compute.api.cloud.ru" || !strings.HasPrefix(u.Path, "/api/") || strings.Contains(u.Path, "//") {
		return nil, denied
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, denied
	}
	// Serialize read/claim across CLI and MCP processes in the same runner.
	// The receipt book separately enforces durable compare-and-swap writes.
	unlock, err := g.lock()
	if err != nil {
		return nil, denied
	}
	defer unlock()
	rows, err := g.load()
	if err != nil {
		return nil, denied
	}
	c := cycle{g, rows}
	if !c.scopeOK() {
		return nil, denied
	}
	b, err := body(r.Body)
	if err != nil {
		return nil, err
	}
	if r.Body != nil {
		r.Body = putBack(b)
	}
	p := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	rt, err := c.route(r, p, q, b)
	if err != nil {
		return nil, err
	}
	if rt.mode == "create" || rt.mode == "single-change" {
		rt.record["post_sent"] = true
		if err = c.record(rt.kind, rt.key, rt.record); err != nil {
			return nil, err
		}
	}
	if rt.mode == "delete" {
		if bit(c.row(rt.key), "delete_sent") {
			return nil, denied
		}
		if rt.key == "app-rule" && g.holdRule {
			if !bit(c.row("rule-cut"), "cut") {
				if err := c.record("checkpoint", "rule-cut", object{"cut": true, "rule_id": id(c.row("app-rule")), "group_id": str(c.row("scope"), "sg_id")}); err != nil {
					return nil, err
				}
			}
			return nil, holdRule
		}
		if rt.key == "vm" && g.cut != nil {
			if bit(c.row("destroy-cut"), "cut") || !g.validateCutState(c) {
				return nil, denied
			}
			if err := c.record("checkpoint", "destroy-cut", object{"cut": true, "state_matches": true, "vm_id": id(c.row("vm")), "fip_id": id(c.row("fip"))}); err != nil {
				return nil, err
			}
			g.cut()
			return nil, denied
		}
		if err := c.record(rt.kind, rt.key, object{"delete_sent": true}); err != nil {
			return nil, err
		}
	}
	resp, err := g.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		resp.Body.Close()
		return nil, denied
	}
	if rt.mode == "" || rt.mode == "delete" || resp.StatusCode/100 != 2 {
		return resp, nil
	}
	if rt.mode == "attach" {
		b, e := body(resp.Body)
		if e != nil {
			return nil, denied
		}
		resp.Body = putBack(b)
		if len(bytes.TrimSpace(b)) > 0 {
			v, e := jsonBody(b)
			m, ok := v.(map[string]any)
			if e != nil || !ok || c.fipCard(m) != nil {
				return nil, denied
			}
		}
		return resp, nil
	}
	if !strings.HasPrefix(rt.mode, "card:") && !strings.HasPrefix(rt.mode, "list:") && rt.mode != "create" {
		return resp, nil
	}
	switch rt.mode {
	case "list:subnets", "list:flavors", "list:interfaces", "list:disk-types", "list:availability-zones", "list:security-groups":
		return resp, nil
	}
	b, err = body(resp.Body)
	if err != nil {
		return nil, denied
	}
	resp.Body = putBack(b)
	v, err := jsonBody(b)
	if err != nil {
		return nil, denied
	}
	if err = c.inspect(rt, v); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}
func newGuard(cycleID string, ro bool, driver, tmp string) (*guard, error) {
	if cycleID != "1" && cycleID != "2" || !filepath.IsAbs(driver) || !filepath.IsAbs(tmp) {
		return nil, denied
	}
	g := &guard{prefix: "core." + cycleID + ".", readOnly: ro, runnerCIDR: os.Getenv("P90_RUNNER_CIDR"), base: httpretry.DefaultBase}
	g.load = func() (ledger, error) {
		cmd := exec.Command("python3", driver, "ledger")
		b, e := cmd.Output()
		if e != nil {
			return nil, denied
		}
		var rows ledger
		e = json.Unmarshal(b, &rows)
		return rows, e
	}
	g.save = func(kind, key string, f object) error {
		b, e := json.Marshal(object{"kind": kind, "key": key, "fields": f})
		if e != nil {
			return denied
		}
		cmd := exec.Command("python3", driver, "checkpoint")
		cmd.Stdin = bytes.NewReader(b)
		return cmd.Run()
	}
	g.lock = func() (func(), error) {
		f, e := os.OpenFile(filepath.Join(tmp, "p90core-"+cycleID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
			f.Close()
			return nil, e
		}
		return func() { f.Close() }, nil
	}
	return g, nil
}

// verify's HTTPS fallback clones http.DefaultTransport instead of DefaultBase.
// Give that clone an address-only dialer; the Compute/IAM base was captured first.
func restrictFallback(g *guard) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	dial := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, destination string) (net.Conn, error) {
		rows, e := g.load()
		if e != nil {
			return nil, denied
		}
		c := cycle{g, rows}
		f, vm := c.row("fip"), c.row("vm")
		want := net.JoinHostPort(str(f, "ip_address"), strconv.Itoa(integer(c.row("scope"), "port")))
		if !c.scopeOK() || !address(str(f, "ip_address")) || destination != want || !owned(vm, str(f, "bound_vm_id")) || bit(f, "confirmed_absent") {
			return nil, denied
		}
		return dial(ctx, network, destination)
	}
	http.DefaultTransport = tr
}
func main() {
	g, e := newGuard(os.Getenv("P90_CORE_CYCLE"), os.Getenv("P90_CORE_READONLY") == "1", os.Getenv("P90_DRIVER"), os.Getenv("RUNNER_TEMP"))
	if e != nil {
		fmt.Fprintln(os.Stderr, "P-90: invalid guard settings")
		os.Exit(1)
	}
	g.diskStep = os.Getenv("P90_DISK_STEP")
	g.planApply = os.Getenv("P90_CORE_PLAN_APPLY") == "1"
	if g.planApply && os.Getenv("P90_CORE_CYCLE") != "2" {
		os.Exit(87)
	}
	g.localPublicKey = func() ([]byte, error) { return os.ReadFile(filepath.Join(".evo", "id_ed25519.pub")) }
	if os.Getenv("P90_CORE_HOLD_RULE") == "1" {
		if os.Getenv("P90_CORE_CYCLE") != "1" {
			os.Exit(87)
		}
		g.holdRule = true
	}
	g.abort = func() {
		fmt.Fprintln(os.Stderr, "P-90: outside the permitted resource or evidence incomplete")
		os.Exit(87)
	}
	if os.Getenv("P90_CORE_INTERRUPT_DELETE") == "1" {
		if os.Getenv("P90_CORE_CYCLE") != "1" {
			os.Exit(87)
		}
		g.cut = func() { os.Exit(91) }
		g.validateCutState = func(c cycle) bool {
			file, e := state.Read(state.DefaultPath("."))
			if e != nil {
				return false
			}
			recs := file.ActiveDeployments()
			if len(recs) != 1 {
				return false
			}
			r := recs[0]
			return r.ProjectID == str(c.row("scope"), "project_id") && owned(c.row("vm"), r.VMID) && owned(c.row("fip"), r.FloatingIPID)
		}
	}
	restrictFallback(g)
	httpretry.DefaultBase = g
	cli.Main("1.9.1-candidate", candidate, "")
}
