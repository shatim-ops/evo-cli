// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const (
	project    = "aaaaaaaa-aaaa-4aaa-8aaa-000000000001"
	zone       = "aaaaaaaa-aaaa-4aaa-8aaa-000000000002"
	sg         = "aaaaaaaa-aaaa-4aaa-8aaa-000000000003"
	sn         = "aaaaaaaa-aaaa-4aaa-8aaa-000000000004"
	imageID    = "aaaaaaaa-aaaa-4aaa-8aaa-000000000005"
	flavorID   = "aaaaaaaa-aaaa-4aaa-8aaa-000000000006"
	fipID      = "aaaaaaaa-aaaa-4aaa-8aaa-000000000007"
	vmID       = "aaaaaaaa-aaaa-4aaa-8aaa-000000000008"
	ifaceID    = "aaaaaaaa-aaaa-4aaa-8aaa-000000000009"
	diskID     = "aaaaaaaa-aaaa-4aaa-8aaa-000000000010"
	ruleID     = "aaaaaaaa-aaaa-4aaa-8aaa-000000000011"
	other      = "aaaaaaaa-aaaa-4aaa-8aaa-000000000099"
	fipName    = "fip-evo-run-test-1pq"
	marker     = "evo-fip-aaaaaaaaaaaaaaaaaaaaaaaaaa"
	webIP      = "192.0.2.65"
	api        = "https://compute.api.cloud.ru"
	vmName     = "evo-run-test"
	runnerCIDR = "8.8.8.8/32"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type world struct {
	g        *guard
	rows     ledger
	calls    map[string]int
	next     func(*http.Request) (*http.Response, error)
	saveFail string
	aborted  int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{rows: ledger{"core.1.scope": {"project_id": project, "zone_id": zone, "sg_id": sg, "subnet_id": sn, "image_id": imageID, "flavor_id": flavorID, "vm_name": vmName, "image_name": "ubuntu-22.04", "flavor_name": "lowcost10-2-4", "disk_type_name": "SSD", "disk_cap_gb": 20.0, "cidr": runnerCIDR, "port": 18085.0}}, calls: map[string]int{}}
	w.rows["core.1.group"] = object{"id": sg, "post_sent": true}
	w.g = &guard{prefix: "core.1.", localPublicKey: func() ([]byte, error) { return []byte("ssh-ed25519 synthetic test"), nil }, runnerCIDR: runnerCIDR, load: func() (ledger, error) { return w.rows, nil }, save: func(kind, k string, f object) error {
		if w.saveFail == k {
			return errors.New("store unavailable")
		}
		if w.rows[k] == nil {
			w.rows[k] = object{"kind": kind}
		}
		for a, v := range f {
			w.rows[k][a] = v
		}
		return nil
	}, lock: func() (func(), error) { return func() {}, nil }, abort: func() { w.aborted++ }}
	w.g.base = transportFunc(func(r *http.Request) (*http.Response, error) {
		w.calls[r.Method+" "+r.URL.Path]++
		if w.next != nil {
			return w.next(r)
		}
		return response(200, object{}), nil
	})
	return w
}
func response(code int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(b))}
}
func send(t *testing.T, w *world, method, target string, v any) (*http.Response, error) {
	t.Helper()
	var reader io.Reader
	if v != nil {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		reader = bytes.NewReader(raw)
	}
	r, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := w.g.RoundTrip(r)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return resp, err
}
func fipReq() object {
	return object{"name": fipName, "description": marker, "project_id": project, "availability_zone_id": zone}
}
func vmReq() []any {
	return []any{object{"name": vmName, "project_id": project, "availability_zone_id": zone, "image_name": "ubuntu-22.04", "flavor_name": "lowcost10-2-4", "image_metadata": object{"name": "test-user", "hostname": vmName, "public_key": "ssh-ed25519 synthetic"}, "disks": []any{object{"name": vmName + "-boot", "size": 20, "disk_type_name": "SSD"}}, "interfaces": []any{object{"type": "regular", "subnet_id": sn, "security_groups": []any{sg}, "attach_external_ip_id": fipID, "interface_security_enabled": true}}}}
}
func vmDetail() object {
	return object{"name": vmName, "id": vmID, "state": "running", "interfaces": []any{object{"id": ifaceID, "primary": true, "type": "regular", "subnet": object{"id": sn}, "security_groups": []any{object{"id": sg}}, "floating_ip": object{"id": fipID, "ip_address": webIP}}}}
}
func ruleReq() object {
	return object{"direction": "ingress", "ether_type": "IPv4", "ip_protocol": "tcp", "port_range": "18085:18085", "description": "evo: port 18085 (vm " + vmID + ")", "remote_ip_prefix": runnerCIDR}
}
func doOK(t *testing.T, w *world, m, path string, b, reply any) {
	t.Helper()
	w.next = func(r *http.Request) (*http.Response, error) { return response(200, reply), nil }
	if _, e := send(t, w, m, api+path, b); e != nil {
		t.Fatalf("%s %s: %v", m, path, e)
	}
}
func seed(w *world) {
	w.rows["core.1.fip"] = object{"id": fipID, "post_sent": true, "name": fipName, "marker": marker, "ip_address": webIP}
	w.rows["core.1.vm"] = object{"id": vmID, "post_sent": true}
}
func TestOwnedCycleDirectReceiptsAndReadback(t *testing.T) {
	w := newWorld(t)
	f := object{"name": fipName, "id": fipID, "description": marker, "ip_address": webIP}
	doOK(t, w, "POST", "/api/v1/floating-ips", fipReq(), f)
	doOK(t, w, "POST", "/api/v1.1/vms", vmReq(), []any{object{"id": vmID, "state": "creating"}})
	doOK(t, w, "GET", "/api/v1/vms/"+vmID, nil, vmDetail())
	doOK(t, w, "GET", "/api/v1/floating-ips/"+fipID, nil, f)
	doOK(t, w, "GET", "/api/v1/floating-ips?project_id="+project, nil, object{"items": []any{f}, "total": 1})
	doOK(t, w, "GET", "/api/v1/disks?project_id="+project, nil, object{"items": []any{object{"id": diskID, "name": vmName + "-boot", "vms": []any{object{"id": vmID, "primary": true}}}}})
	doOK(t, w, "POST", "/api/v1/security-groups/"+sg+"/rules?project_id="+project, ruleReq(), object{"id": ruleID})
	for _, d := range []string{"/api/v1/security-groups/" + sg + "/rules/" + ruleID, "/api/v1/vms/" + vmID + "?project_id=" + project, "/api/v1/floating-ips/" + fipID + "?project_id=" + project} {
		doOK(t, w, "DELETE", d, nil, nil)
	}
	for _, field := range []string{"post_sent", "delete_sent", "post_marker_present", "post_marker_exact", "card_marker_present", "card_marker_exact", "list_marker_present", "list_marker_exact"} {
		if !bit(w.rows["core.1.fip"], field) {
			t.Fatal("receipt lacks", field)
		}
	}
	for k, v := range map[string]string{"vm": vmID, "fip": fipID, "app-rule": ruleID, "bootdisk": diskID, "iface": ifaceID} {
		if id(w.rows["core.1."+k]) != v {
			t.Fatal("wrong receipt", k)
		}
	}
	if str(w.rows["core.1.fip"], "bound_vm_id") != vmID {
		t.Fatal("missing SSH proof")
	}
}
func TestNoSecondPostAfterUnknownOrUnpersistedReply(t *testing.T) {
	for _, bad := range []string{"before", "network", "missing_id", "multiple", "incomplete_body", "partial_status"} {
		t.Run(bad, func(t *testing.T) {
			w := newWorld(t)
			seed(w)
			delete(w.rows, "core.1.vm")
			if bad == "before" {
				w.saveFail = "core.1.vm"
			}
			w.next = func(*http.Request) (*http.Response, error) {
				if !bit(w.rows["core.1.vm"], "post_sent") {
					t.Fatal("outbound without durable receipt")
				}
				switch bad {
				case "network":
					return nil, errors.New("lost")
				case "missing_id":
					return response(200, object{}), nil
				case "multiple":
					return response(200, []any{object{"id": vmID}, object{"id": other}}), nil
				case "incomplete_body":
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("[{"))}, nil
				default:
					return response(206, object{"id": vmID}), nil
				}
			}
			if _, e := send(t, w, "POST", api+"/api/v1.1/vms", vmReq()); e == nil {
				t.Fatal("uncertainty reported as success")
			}
			if _, e := send(t, w, "POST", api+"/api/v1.1/vms", vmReq()); e == nil {
				t.Fatal("retried")
			}
			want := 1
			if bad == "before" {
				want = 0
			}
			if w.calls["POST /api/v1.1/vms"] != want {
				t.Fatal("outbound count", w.calls)
			}
			if id(w.rows["core.1.vm"]) != "" {
				t.Fatal("ID appeared without direct receipt")
			}
			if bad != "before" {
				w.next = func(*http.Request) (*http.Response, error) {
					return response(200, object{"items": []any{object{"id": other, "name": vmName}}}), nil
				}
				if _, e := send(t, w, "GET", api+"/api/v1/vms?project_id="+project, nil); e == nil {
					t.Fatal("name-time fallback yielded unrelated VM")
				}
			}
		})
	}
}
func TestForeignMutationsAndDestinationsNeverForwarded(t *testing.T) {
	w := newWorld(t)
	seed(w)
	w.rows["core.1.iface"] = object{"id": ifaceID}
	w.rows["core.1.app-rule"] = object{"id": ruleID}
	badRule := ruleReq()
	badRule["port_range"] = "22:22"
	cases := []struct {
		m, url string
		req    any
	}{
		{"DELETE", api + "/api/v1/vms/" + other, nil}, {"DELETE", api + "/api/v1/floating-ips/" + other, nil},
		{"DELETE", api + "/api/v1/vms/" + vmID + "?project_id=" + other, nil}, {"DELETE", api + "/api/v1/disks/" + other, nil},
		{"DELETE", api + "/api/v1/security-groups/" + sg + "/rules/" + other, nil}, {"DELETE", api + "/api/v1/security-groups/" + other + "/rules/" + ruleID, nil},
		{"POST", api + "/api/v1/security-groups/" + sg + "/rules", badRule}, {"PUT", api + "/api/v1/floating-ips/" + fipID, object{"name": fipName, "interface_id": other}},
		{"POST", "https://kafka.api.cloud.ru/v1/clusters", object{}}, {"GET", "https://redis.api.cloud.ru/v1/clusters", nil},
		{"GET", "http://" + webIP + ":18085/", nil}, {"GET", "https://compute.api.cloud.ru:443/api/v1/vms", nil},
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if _, e := send(t, w, c.m, c.url, c.req); e == nil {
				t.Fatal("forwarded")
			}
		})
	}
	if len(w.calls) != 0 {
		t.Fatalf("forbidden requests reached upstream %v", w.calls)
	}
}
func TestNoSSHOnForeignCardAndNoNameOnlyFIPOrDisk(t *testing.T) {
	w := newWorld(t)
	seed(w)
	for _, mutate := range []func(object){
		func(m object) { m["id"] = other }, func(m object) { m["public_ip"] = "192.0.2.207" },
		func(m object) { delete(m, "interfaces") }, func(m object) { m["interfaces"].([]any)[0].(object)["type"] = "direct_ip" },
		func(m object) {
			m["interfaces"].([]any)[0].(object)["floating_ip"] = object{"id": other, "ip_address": webIP}
		},
	} {
		m := vmDetail()
		mutate(m)
		w.next = func(*http.Request) (*http.Response, error) { return response(200, m), nil }
		if _, e := send(t, w, "GET", api+"/api/v1/vms/"+vmID, nil); e == nil {
			t.Fatal("unsafe VM card")
		}
	}
	if w.aborted != 5 {
		t.Fatalf("did not stop the caller: %d", w.aborted)
	}
	w.next = func(*http.Request) (*http.Response, error) {
		return response(200, object{"items": []any{object{"id": other, "name": fipName, "state": "available"}}}), nil
	}
	if _, e := send(t, w, "GET", api+"/api/v1/floating-ips?project_id="+project, nil); e == nil {
		t.Fatal("unproven address became visible to reuse")
	}
	doOK(t, w, "GET", "/api/v1/disks?project_id="+project, nil, object{"items": []any{object{"id": diskID, "name": vmName + "-boot", "vms": []any{object{"id": other, "primary": true}}}}})
	if id(w.rows["core.1.bootdisk"]) != "" {
		t.Fatal("foreign disk adopted")
	}
}
func TestReadOnlyAndInterruptBeforeVMDelete(t *testing.T) {
	w := newWorld(t)
	seed(w)
	w.g.readOnly = true
	if _, e := send(t, w, "DELETE", api+"/api/v1/vms/"+vmID, nil); e == nil {
		t.Fatal("readonly mutates")
	}
	doOK(t, w, "POST", "/api/v1/projects/"+project+"/price-calculation", object{"total_count": 1}, object{})
	w.g.readOnly = false
	cut := 0
	w.g.cut = func() { cut++ }
	w.g.validateCutState = func(c cycle) bool { return false }
	send(t, w, "DELETE", api+"/api/v1/vms/"+vmID, nil)
	if cut != 0 {
		t.Fatal("interruption reported without state evidence")
	}
	w.g.validateCutState = func(c cycle) bool { return true }
	send(t, w, "DELETE", api+"/api/v1/vms/"+vmID, nil)
	if cut != 1 || !bit(w.rows["core.1.destroy-cut"], "state_matches") || w.calls["DELETE /api/v1/vms/"+vmID] != 0 {
		t.Fatal("unexpected cut or outbound DELETE")
	}
	w.g.cut = nil
	doOK(t, w, "DELETE", "/api/v1/vms/"+vmID, nil, nil)
	if w.calls["DELETE /api/v1/vms/"+vmID] != 1 {
		t.Fatal("resume did not delete exact VM")
	}
}

func TestDeliberateRuleHoldDoesNotEmitDeleteAndCanResume(t *testing.T) {
	w := newWorld(t)
	seed(w)
	w.rows["core.1.app-rule"] = object{"id": ruleID}
	path := "/api/v1/security-groups/" + sg + "/rules/" + ruleID
	w.g.holdRule = true
	for range 2 {
		if _, e := send(t, w, "DELETE", api+path, nil); !errors.Is(e, holdRule) {
			t.Fatal("missing controlled failure")
		}
	}
	if !bit(w.rows["core.1.rule-cut"], "cut") || str(w.rows["core.1.rule-cut"], "rule_id") != ruleID || w.aborted != 0 || w.calls["DELETE "+path] != 0 {
		t.Fatal("wrong pre-wire evidence")
	}
	w.g.holdRule = false
	doOK(t, w, "DELETE", path, nil, nil)
	if w.calls["DELETE "+path] != 1 {
		t.Fatal("did not resume")
	}
}
func TestDiskImageProductWritesRequireDirectReceiptCycleAndStage(t *testing.T) {
	w := newWorld(t)
	seed(w)
	w.g.prefix = "core.2."
	for k, v := range w.rows {
		if strings.HasPrefix(k, "core.1.") {
			w.rows[strings.Replace(k, "core.1.", "core.2.", 1)] = v
		}
	}
	w.rows["core.2.lifecycle"] = object{"application_verified": true}
	w.rows["core.2.bootdisk"] = object{"id": diskID, "vm_id": vmID}
	const did = "bbbbbbbb-bbbb-4bbb-8bbb-000000000001"
	const img = "bbbbbbbb-bbbb-4bbb-8bbb-000000000002"
	w.g.diskStep = "create-data"
	d := object{"project_id": project, "availability_zone_id": zone, "name": vmName + "-data", "size": 10, "disk_type_name": "SSD"}
	doOK(t, w, "POST", "/api/v1/disks?project_id="+project, d, object{"name": vmName + "-data", "id": did, "state": "creating"})
	if _, e := send(t, w, "POST", api+"/api/v1/disks?project_id="+project, d); e == nil {
		t.Fatal("second data disk")
	}
	w.g.diskStep = "attach-data"
	if _, e := send(t, w, "POST", api+"/api/v1/disks/"+did+"/attach", object{"vm_id": other}); e == nil {
		t.Fatal("foreign VM was allowed")
	}
	doOK(t, w, "POST", "/api/v1/disks/"+did+"/attach", object{"vm_id": vmID}, object{})
	w.g.diskStep = "resize-data"
	if _, e := send(t, w, "PUT", api+"/api/v1/disks/"+did, object{"size": 21}); e == nil {
		t.Fatal("larger disk allowed")
	}
	doOK(t, w, "PUT", "/api/v1/disks/"+did, object{"size": 20}, object{})
	w.g.diskStep = "detach-data"
	doOK(t, w, "POST", "/api/v1/disks/"+did+"/detach", object{"vm_id": vmID}, object{})
	w.g.diskStep = "delete-data"
	doOK(t, w, "DELETE", "/api/v1/disks/"+did, nil, nil)
	w.g.diskStep = "stop-for-image"
	doOK(t, w, "POST", "/api/v1/vms/"+vmID+"/set-power", object{"state": "power_off"}, object{})
	w.g.diskStep = "detach-boot"
	doOK(t, w, "POST", "/api/v1/disks/"+diskID+"/detach", object{"vm_id": vmID}, nil)
	i := object{"name": vmName + "-image", "disk_id": did, "description": vmName + "-image", "availability_zones": []any{object{"availability_zone_id": zone}}, "project_id": project}
	w.g.diskStep = "create-image"
	if _, e := send(t, w, "POST", api+"/api/v1/images", i); e == nil {
		t.Fatal("regular data disk accepted as boot source")
	}
	i["disk_id"] = diskID
	doOK(t, w, "POST", "/api/v1/images", i, object{"name": vmName + "-image", "id": img, "project_id": project})
	if str(w.rows["core.2.new-image"], "source_disk_id") != diskID {
		t.Fatal("source lost")
	}
	w.g.diskStep = "restore-boot"
	doOK(t, w, "POST", "/api/v1/disks/"+diskID+"/attach", object{"vm_id": vmID}, nil)
	w.g.diskStep = "delete-image"
	doOK(t, w, "DELETE", "/api/v1/images/"+img, nil, nil)
	if _, e := send(t, w, "DELETE", api+"/api/v1/images/"+imageID, nil); e == nil {
		t.Fatal("catalogue image deletion")
	}
}

func TestOnlyApprovedPublicRunnerMayReachAppPort(t *testing.T) {
	for _, cidr := range []string{"", "0.0.0.0/0", "8.8.8.8/31", "8.8.8.8", "9.9.9.9/32", "127.0.0.1/32", "10.0.0.1/32", "100.64.1.2/32", "169.254.1.1/32", "198.51.100.1/32", "224.0.0.1/32", "255.255.255.255/32", "::1/128"} {
		t.Run(cidr, func(t *testing.T) {
			w := newWorld(t)
			seed(w)
			w.g.runnerCIDR = cidr
			if cidr != "9.9.9.9/32" {
				w.rows["core.1.scope"]["cidr"] = cidr
			}
			req := ruleReq()
			req["remote_ip_prefix"] = cidr
			if _, err := send(t, w, "POST", api+"/api/v1/security-groups/"+sg+"/rules", req); err == nil || len(w.calls) != 0 {
				t.Fatal("unapproved scope reached upstream")
			}
		})
	}
	w := newWorld(t)
	seed(w)
	req := ruleReq()
	req["remote_ip_prefix"] = "0.0.0.0/0"
	if _, err := send(t, w, "POST", api+"/api/v1/security-groups/"+sg+"/rules", req); err == nil || len(w.calls) != 0 {
		t.Fatal("widened ingress")
	}
}

func TestPlanScopeOwnInterfaceBeforeFIPAndRejectForeignSSH(t *testing.T) {
	w := newWorld(t)
	w.g.prefix = "core.2."
	w.g.planApply = true
	scope := w.rows["core.1.scope"]
	w.rows["core.2.scope"] = scope
	w.rows["core.2.group"] = w.rows["core.1.group"]
	scope["creation_mode"] = "plan"
	scope["plan_id"] = "plan-123456789abc"
	scope["boot_name"] = vmName + "-disk"
	scope["ssh_key_fingerprint"] = fmt.Sprintf("%x", sha256.Sum256([]byte("ssh-ed25519 synthetic")))
	vr := vmReq()
	m := vr[0].(object)
	m["disks"].([]any)[0].(object)["name"] = vmName + "-disk"
	ni := m["interfaces"].([]any)[0].(object)
	delete(ni, "attach_external_ip_id")
	meta := m["image_metadata"].(object)
	meta["linux_password"] = "unexpected"
	if _, err := send(t, w, "POST", api+"/api/v1.1/vms", vr); err == nil || len(w.calls) > 0 {
		t.Fatal("unapproved password sent")
	}
	delete(meta, "linux_password")
	meta["public_key"] = "ssh-ed25519 foreign"
	if _, err := send(t, w, "POST", api+"/api/v1.1/vms", vr); err == nil || len(w.calls) > 0 {
		t.Fatal("foreign key sent")
	}
	meta["public_key"] = "ssh-ed25519 synthetic"
	ni["security_groups"] = []any{other}
	if _, err := send(t, w, "POST", api+"/api/v1.1/vms", vr); err == nil || len(w.calls) > 0 {
		t.Fatal("foreign network")
	}
	ni["security_groups"] = []any{sg}
	doOK(t, w, "POST", "/api/v1.1/vms", vr, []any{object{"id": vmID, "state": "creating"}})
	f := fipReq()
	f["name"] = vmName + "-fip"
	f["interface_id"] = ifaceID
	if _, err := send(t, w, "POST", api+"/api/v1/floating-ips", f); err == nil {
		t.Fatal("unproven interface")
	}
	v := vmDetail()
	iface := v["interfaces"].([]any)[0].(object)
	delete(iface, "floating_ip")
	v["public_ip"] = webIP
	w.next = func(*http.Request) (*http.Response, error) { return response(200, v), nil }
	if _, err := send(t, w, "GET", api+"/api/v1/vms/"+vmID, nil); err == nil {
		t.Fatal("unproven SSH")
	}
	delete(v, "public_ip")
	doOK(t, w, "GET", "/api/v1/vms/"+vmID, nil, v)
	f["interface_id"] = other
	if _, err := send(t, w, "POST", api+"/api/v1/floating-ips", f); err == nil {
		t.Fatal("foreign interface")
	}
	f["interface_id"] = ifaceID
	doOK(t, w, "POST", "/api/v1/floating-ips", f, object{"id": fipID, "name": vmName + "-fip", "description": marker, "ip_address": webIP, "interface": object{"id": ifaceID}})
	w.g.planApply = false
	w.next = func(*http.Request) (*http.Response, error) { return response(200, v), nil }
	if _, err := send(t, w, "GET", api+"/api/v1/vms/"+vmID, nil); err == nil {
		t.Fatal("unbound active VM after plan")
	}
	doOK(t, w, "GET", "/api/v1/vms/"+vmID, nil, vmDetail())
	if !bit(w.rows["core.2.iface"], "plan_unbound_scope_verified") || str(w.rows["core.2.fip"], "bound_vm_id") != vmID {
		t.Fatal("missing correlations")
	}
}

func TestLostDeleteResponseNeverSendsAnotherDelete(t *testing.T) {
	for k, path := range map[string]string{"vm": "/api/v1/vms/" + vmID, "fip": "/api/v1/floating-ips/" + fipID, "app-rule": "/api/v1/security-groups/" + sg + "/rules/" + ruleID} {
		t.Run(k, func(t *testing.T) {
			w := newWorld(t)
			seed(w)
			w.rows["core.1.app-rule"] = object{"id": ruleID}
			w.next = func(*http.Request) (*http.Response, error) { return nil, errors.New("response lost") }
			if _, e := send(t, w, "DELETE", api+path, nil); e == nil || !bit(w.rows["core.1."+k], "delete_sent") {
				t.Fatal("no durable physical-dispatch marker")
			}
			if _, e := send(t, w, "DELETE", api+path, nil); !errors.Is(e, denied) || w.calls["DELETE "+path] != 1 {
				t.Fatal("duplicate physical dispatch")
			}
		})
	}
}
