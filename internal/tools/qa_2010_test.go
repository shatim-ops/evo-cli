// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// QA 20.10: проверки аргументов до превью и до облака, причины ответов
// isError=true. Облако — contractRT (500 на всё) или фейковый CloudAPI.

const (
	qaID1 = "00000000-0000-4000-8000-000000000001"
	qaID2 = "00000000-0000-4000-8000-000000000002"
)

func qaDecode(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	return m
}

// usageCase: the call is USAGE, nothing reached the transport, and the
// message names want.
type usageCase struct {
	tool string
	args map[string]any
	want string
}

func runUsageCases(t *testing.T, cases []usageCase) {
	t.Helper()
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	for _, c := range cases {
		tl, ok := reg.Lookup(c.tool)
		if !ok {
			t.Fatalf("no tool %s", c.tool)
		}
		rt.take()
		text, isErr, pan := contractCall(tl, c.args, 300*time.Millisecond)
		reqs := rt.take()
		m := qaDecode(t, text)
		msg, _ := m["error"].(string)
		if pan != "" || !isErr || m["reason"] != clerrors.CodeUsage || len(reqs) != 0 || !strings.Contains(msg, c.want) {
			t.Errorf("%s %v: err=%v reason=%v reqs=%v want %q in error; text=%.300s", c.tool, c.args, isErr, m["reason"], reqs, c.want, text)
		}
	}
}

// QA-1: id и cluster_id расходятся — USAGE до облака; `pg delete A --id B`
// удалял B.
func TestQA1_PgIDAliasDisagree(t *testing.T) {
	both := map[string]any{"id": qaID1, "cluster_id": qaID2}
	runUsageCases(t, []usageCase{
		{"pg_get", both, "cluster_id"},
		{"pg_delete", both, "cluster_id"},
		{"pg_delete", map[string]any{"id": qaID1, "cluster_id": qaID2, "confirmed": true}, "cluster_id"},
	})
	// The same value twice, or either alone, goes on to the cloud.
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	for _, args := range []map[string]any{{"id": qaID1, "cluster_id": qaID1}, {"cluster_id": qaID1}, {"id": qaID1}} {
		tl, _ := reg.Lookup("pg_get")
		rt.take()
		text, _, _ := contractCall(tl, args, 300*time.Millisecond)
		if len(rt.take()) == 0 {
			t.Errorf("pg_get %v did not reach the API: %s", args, text)
		}
	}
}

// QA-3 (2), (3): порт — целое 1–65535, иначе USAGE с элементом до превью;
// так же у deploy_plan.
func TestQA3_PortsChecked(t *testing.T) {
	var cases []usageCase
	for _, tc := range []struct {
		ports any
		want  string
	}{
		{[]any{"8000-8010"}, `"8000-8010"`},
		{"80,abc", `"abc"`},
		{[]any{float64(80.5)}, "80.5"},
		{[]any{float64(0)}, "port 0"},
		{[]any{float64(-80)}, "port -80"},
		{[]any{float64(70000)}, "port 70000"},
		{"443,65536", "port 65536"},
	} {
		cases = append(cases,
			usageCase{"open_ports", map[string]any{"security_group": "web", "ports": tc.ports}, tc.want},
			usageCase{"open_ports", map[string]any{"security_group": "web", "ports": tc.ports, "confirmed": true}, tc.want},
			usageCase{"deploy_plan", map[string]any{"ports": tc.ports}, tc.want})
	}
	runUsageCases(t, cases)
	for _, ok := range []any{[]any{float64(80), "443"}, "80, 443", []any{float64(1), float64(65535)}} {
		got, err := portsArg("ports", ok)
		if err != nil || len(got) != 2 {
			t.Errorf("%v: %v %v", ok, got, err)
		}
	}
}

// failRulesCloud refuses the rules of the ports in fail.
type failRulesCloud struct {
	sgCloud
	fail  map[string]bool
	added []string
}

func (c *failRulesCloud) AddSecurityGroupRule(_ context.Context, _, _ string, r provision.SGRule) (*provision.SGRule, error) {
	if c.fail[r.PortRange] {
		return nil, &provision.StatusError{Status: 422, Body: `{"code":"bad_rule","message":"rule refused for ` + r.PortRange + `"}`}
	}
	c.added = append(c.added, r.PortRange)
	return &provision.SGRule{ID: "r-" + r.PortRange}, nil
}

func failRulesRegistry(t *testing.T, cloud provision.CloudAPI) *Registry {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config: config.Config{ProjectID: qaID1},
			Auth:   staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir,
		}, nil
	})
}

// QA-3 (1): ни одно правило не создалось — ошибка с причиной первого
// отказа; часть — status partial, isError, причины по портам.
func TestQA3_OpenPortsFailures(t *testing.T) {
	args := map[string]any{"security_group": "sg-1", "ports": []any{float64(80), float64(81)}, "confirmed": true}

	cloud := &failRulesCloud{fail: map[string]bool{"80:80": true, "81:81": true}}
	m, text, isErr := callNamed(t, failRulesRegistry(t, cloud), "open_ports", args)
	failures, _ := m["failures"].(map[string]any)
	if !isErr || m["status"] != "error" || m["reason"] != clerrors.CodeAPIError || !strings.Contains(text, "rule refused for 80:80") || len(failures) != 2 || m["opened"] != nil {
		t.Errorf("all failed: %s", text)
	}

	cloud = &failRulesCloud{fail: map[string]bool{"81:81": true}}
	m, text, isErr = callNamed(t, failRulesRegistry(t, cloud), "open_ports", args)
	failures, _ = m["failures"].(map[string]any)
	opened, _ := m["opened"].([]any)
	if !isErr || m["status"] != "partial" || m["reason"] != clerrors.CodeAPIError || len(opened) != 1 || len(failures) != 1 || failures["81"] == nil {
		t.Errorf("partial: %s", text)
	}

	cloud = &failRulesCloud{}
	m, text, isErr = callNamed(t, failRulesRegistry(t, cloud), "open_ports", args)
	if isErr || m["status"] != "ok" || m["failures"] != nil || len(cloud.added) != 2 {
		t.Errorf("all opened: %s", text)
	}
}

// QA-4: snat_create без name, snat_delete без id — USAGE (их required
// теперь в схеме).
func TestQA4_RequiredInSchema(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	for name, want := range map[string]string{"snat_create": "name", "snat_delete": "id", "open_ports": "ports"} {
		tl, _ := reg.Lookup(name)
		found := false
		for _, r := range schemaRequired(tl) {
			found = found || r == want
		}
		if !found {
			t.Errorf("%s: required %v has no %s", name, schemaRequired(tl), want)
		}
	}
}

// QA-5, QA-14: даты YYYY-MM-DD, end не раньше start, days ≥ 1 — USAGE до
// облака у billing_consumption и billing_finops.
func TestQA5_BillingDates(t *testing.T) {
	var cases []usageCase
	for _, bad := range []string{"2026-13-45", "yesterday", "2026-07-01T00:00:00Z", "2026-7-1"} {
		cases = append(cases,
			usageCase{"billing_consumption", map[string]any{"start": bad}, bad},
			usageCase{"billing_consumption", map[string]any{"start": "2026-07-01", "end": bad}, bad},
			usageCase{"billing_finops", map[string]any{"start": bad, "end": "2026-07-10"}, bad},
			usageCase{"billing_finops", map[string]any{"end": bad}, bad})
	}
	cases = append(cases,
		usageCase{"billing_consumption", map[string]any{"start": "2026-07-10", "end": "2026-07-01"}, "2026-07-01"},
		usageCase{"billing_finops", map[string]any{"start": "2026-07-10", "end": "2026-07-01"}, "2026-07-01"},
		usageCase{"billing_finops", map[string]any{"days": float64(-1)}, "days"},
		usageCase{"billing_finops", map[string]any{"days": float64(0)}, "days"})
	runUsageCases(t, cases)
}

// QA-7, QA-8, QA-14, QA-15: границы и закрытые списки — USAGE до превью
// (без confirmed) и до облака.
func TestQA7_BoundsBeforePreview(t *testing.T) {
	np := func(k string, v any) map[string]any { return map[string]any{"cluster_id": qaID1, k: v} }
	lb := func(k string, v any) map[string]any {
		m := map[string]any{"name": "lb", "target_group_id": qaID1, "port": float64(80)}
		m[k] = v
		return m
	}
	pg := func(k string, v any) map[string]any { return map[string]any{"name": "db", k: v} }
	img := func(k string, v any) map[string]any { return map[string]any{"name": "img", "disk_id": qaID1, k: v} }
	runUsageCases(t, []usageCase{
		{"k8s_add_nodepool", np("count", float64(0)), "count"},
		{"k8s_add_nodepool", np("count", float64(-2)), "count"},
		{"k8s_add_nodepool", np("disk_gb", float64(5)), "disk_gb"},
		{"k8s_add_nodepool", np("cpu", float64(0)), "cpu"},
		{"k8s_add_nodepool", np("ram_gb", float64(-4)), "ram_gb"},
		{"lb_create", lb("port", float64(70000)), "port 70000"},
		{"lb_create", lb("port", float64(-80)), "port -80"},
		{"lb_create", lb("target_port", float64(0)), "target_port 0"},
		{"lb_create", lb("hc_port", float64(65536)), "hc_port 65536"},
		{"pg_create", pg("storage_gb", float64(0)), "storage_gb"},
		{"pg_create", pg("storage_gb", float64(-5)), "storage_gb"},
		{"pg_create", pg("backup_retention_days", float64(0)), "backup_retention_days 0"},
		{"pg_create", pg("backup_retention_days", float64(31)), "backup_retention_days 31"},
		{"image_create", img("min_disk", float64(-1)), "min_disk"},
		{"image_create", img("min_ram", float64(-1)), "min_ram"},
		{"image_create", img("min_cpu", float64(-1)), "min_cpu"},
		// QA-8
		{"release_floating_ip", map[string]any{"id": qaID1, "ip_address": "192.0.2.10"}, "not both"},
		{"release_floating_ip", map[string]any{"id": qaID1, "ip_address": "192.0.2.10", "confirmed": true}, "not both"},
		// QA-14
		{"provision", map[string]any{"name": "web", "disk_size": float64(-5)}, "disk_size"},
		{"deploy", map[string]any{"disk_size": float64(-5)}, "disk_size"},
		// QA-15
		{"ar_create", map[string]any{"name": "reg", "registry_type": "MAVEN"}, "MAVEN"},
		{"ar_create", map[string]any{"name": "reg", "tariff": "GOLD"}, "GOLD"},
		{"k8s_kubeconfig", map[string]any{"cluster_id": qaID1, "network": "internet"}, "internet"},
		{"vpc_create", map[string]any{"name": " net"}, "space"},
		{"vpc_create", map[string]any{"name": "net\x01"}, "control"},
		{"vpc_create", map[string]any{"name": strings.Repeat("n", 256)}, "255"},
		{"pg_create", map[string]any{"name": "db "}, "space"},
		{"secret_create", map[string]any{"path": "folder/", "value": "v"}, "empty"},
		{"secret_create", map[string]any{"path": "a\nb", "value": "v"}, "control"},
		{"s3_bucket_create", map[string]any{"bucket": "My_Bucket"}, "My_Bucket"},
		{"s3_bucket_create", map[string]any{"bucket": "ab"}, `"ab"`},
		{"k8s_plan", map[string]any{"name": "k1"}, `"k1"`},
		{"k8s_create", map[string]any{"name": "my cluster"}, "my cluster"},
		{"k8s_plan", map[string]any{"product": "web app"}, "k8s-web app"},
	})
}

// The enum values are canonicalized by the schema (lowercase in, the
// spelling of the API out) and good names pass.
func TestQA15_GoodValuesPass(t *testing.T) {
	for _, n := range []string{"web", "k8s-prod_1", "кластер-1"} {
		if err := k8sClusterName(n); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	for _, n := range []string{"my-bucket.1", "abc"} {
		if err := bucketName(n); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	for _, n := range []string{"net-1", "Сеть проекта", "a"} {
		if err := plainName("VPC", "name", n); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
}

// QA-6: у ответов isError=true путей refused, not_deletable_yet есть
// reason из таблицы кодов; у org_quotas reason — код, фраза — в note.
func TestQA6_ReasonsOfRefusals(t *testing.T) {
	// release_floating_ip: the address of a SNAT gateway, and one in use.
	cloud := &natFIPCloud{}
	r, _ := targetRegistry(t, cloud)
	for _, args := range []map[string]any{{"id": "0000f1f1-0000-4000-8000-0000000000aa", "confirmed": true}, {"ip_address": "203.0.113.9", "confirmed": true}} {
		m, text, isErr := callNamed(t, r, "release_floating_ip", args)
		if !isErr || m["reason"] != clerrors.CodeUsage || m["error"] == nil || m["status"] != "refused" || cloud.deleted != "" {
			t.Errorf("release %v: %s", args, text)
		}
	}
	used := &inUseFIPCloud{}
	r, _ = targetRegistry(t, used)
	m, text, isErr := callNamed(t, r, "release_floating_ip", map[string]any{"id": "0000f1f1-0000-4000-8000-000000000001", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeUsage || m["status"] != "refused" || !strings.Contains(text, "in use") || used.deleted {
		t.Errorf("in use: %s", text)
	}
	// destroy of a VM still being created.
	tc := &targetCloud{deleteErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
		&provision.StatusError{Status: 409, Body: "creating"})}
	r, _ = targetRegistry(t, tc)
	m, text, isErr = callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if !isErr || m["status"] != "not_deletable_yet" || m["reason"] != clerrors.CodeAPIError || m["error"] == nil || m["hint"] == nil {
		t.Errorf("not_deletable_yet: %s", text)
	}
	if !clerrors.IsCode(m["reason"].(string)) {
		t.Errorf("reason %v is not a code", m["reason"])
	}
	// org_quotas without the organization scope: not an error, reason a code.
	rt := &contractRT{kind: "403"}
	reg := contractReg(t, rt)
	tl, _ := reg.Lookup("org_quotas")
	text, isErr, _ = contractCall(tl, nil, 400*time.Millisecond)
	m = qaDecode(t, text)
	if isErr || m["status"] != "unavailable" || m["reason"] != clerrors.CodeAPIError || m["note"] == nil {
		t.Errorf("org_quotas 403: %s", text)
	}
}

// inUseFIPCloud: a floating IP attached to a VM.
type inUseFIPCloud struct {
	provision.CloudAPI
	deleted bool
}

func (*inUseFIPCloud) SetToken(string) {}
func (*inUseFIPCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	var f provision.FloatingIP
	_ = json.Unmarshal([]byte(`{"id":"0000f1f1-0000-4000-8000-000000000001","ip_address":"192.0.2.11","state":"in_use"}`), &f)
	return &f, nil
}
func (c *inUseFIPCloud) DeleteFloatingIP(context.Context, string, string) error {
	c.deleted = true
	return nil
}

// QA-13: сообщение проверки аргументов не начинается с имени инструмента —
// CLI добавляет его сам («dns_record_create: dns_record_create: …»).
func TestQA13_NoToolNamePrefix(t *testing.T) {
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"dns_record_create", map[string]any{"bogus": "x"}},
		{"dns_record_create", map[string]any{"ttl": "x"}},
		{"dns_zone_create", map[string]any{"domain": "x.ru.", "type": "internal"}},
		{"vm_get", nil},
		{"disk_create", map[string]any{"name": "d", "size_gb": float64(0)}},
	} {
		tl, _ := reg.Lookup(c.tool)
		text, _, _ := contractCall(tl, c.args, 300*time.Millisecond)
		m := qaDecode(t, text)
		msg, _ := m["error"].(string)
		if m["reason"] != clerrors.CodeUsage || strings.HasPrefix(msg, c.tool) {
			t.Errorf("%s %v: %s", c.tool, c.args, text)
		}
	}
	// disk create d --size-gb 0 names size_gb, not the name that was given.
	tl, _ := reg.Lookup("disk_create")
	text, _, _ := contractCall(tl, map[string]any{"name": "d", "size_gb": float64(0)}, 300*time.Millisecond)
	if msg, _ := qaDecode(t, text)["error"].(string); strings.Contains(msg, "'name'") || !strings.Contains(msg, "size_gb") {
		t.Errorf("disk_create size_gb 0: %s", msg)
	}
}
