// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func TestContract_ArgForms(t *testing.T) {
	reg := tools.New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	open, ok := reg.Lookup("open_ports")
	if !ok {
		t.Fatal("open_ports")
	}
	disk, ok := reg.Lookup("disk_create")
	if !ok {
		t.Fatal("disk_create")
	}
	helm, ok := reg.Lookup("k8s_helm")
	if !ok {
		t.Fatal("k8s_helm")
	}
	// ports=80,443, ports=80 and a JSON array are the array form from the
	// tools call help: comma-separated or a JSON array. Each item stays the
	// integer the schema names.
	for _, tc := range []struct {
		pairs []string
		n     int
	}{
		{[]string{"security_group=web", "ports=80,443"}, 2},
		{[]string{"security_group=web", "ports=80"}, 1},
		{[]string{"security_group=web", "ports=[80,443]"}, 2},
	} {
		args, err := buildToolArgs(open, "", "", tc.pairs)
		if err != nil {
			t.Fatalf("%v: %v", tc.pairs, err)
		}
		ports, _ := args["ports"].([]any)
		if len(ports) != tc.n || ports[0] != float64(80) {
			t.Fatalf("%v: ports %#v", tc.pairs, args["ports"])
		}
	}
	if _, err := buildToolArgs(open, "", "", []string{"security_group=web", "ports=80a"}); err == nil || clerrors.ExitCode(err) != clerrors.ExitGeneral {
		t.Fatalf("ports=80a: %v", err)
	}
	// size_gb=10GB is not an integer. A silent 10 would be a finding.
	if _, err := buildToolArgs(disk, "", "", []string{"name=web", "size_gb=10GB"}); err == nil || clerrors.ExitCode(err) != clerrors.ExitGeneral {
		t.Fatalf("size_gb=10GB: %v", err)
	}
	// name= is an empty required argument: USAGE, not a call with "".
	if _, err := buildToolArgs(disk, "", "", []string{"name=", "size_gb=10"}); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("empty name: %v", err)
	}
	// The first '=' separates the name. A string keeps the rest, including '='.
	args, err := buildToolArgs(helm, "", "", []string{"cluster_id=00000000-0000-4000-8000-000000000001", "action=list", "values=a=b"})
	if err != nil || args["values"] != "a=b" {
		t.Fatalf("values=a=b: %v %#v", err, args)
	}
	if _, err := buildToolArgs(open, "", "", []string{"security_group=a=b"}); err != nil {
		t.Fatalf("value may contain '=': %v", err)
	}
	// A repeated --arg: the later one wins (tools call help).
	args, err = buildToolArgs(disk, "", "", []string{"name=one", "size_gb=10", "name=two"})
	if err != nil || args["name"] != "two" {
		t.Fatalf("repeat: %v %#v", err, args)
	}
	if _, err := buildToolArgs(disk, "", "", []string{"name"}); err == nil {
		t.Fatal("--arg without '=' was accepted")
	}
	if _, err := buildToolArgs(disk, "", `["not","object"]`, nil); err == nil {
		t.Fatal("--args-json array was accepted")
	}
	if _, err := buildToolArgs(disk, "", "null", nil); err == nil {
		t.Fatal("--args-json null was accepted")
	}
	// --args-json then --arg: the flag wins, as the help says.
	args, err = buildToolArgs(disk, "", `{"name":"from-json","size_gb":10}`, []string{"name=from-arg"})
	if err != nil || args["name"] != "from-arg" || args["size_gb"] != float64(10) {
		t.Fatalf("json+arg: %v %#v", err, args)
	}
}

func TestContract_ConfirmedIsNotAnArg(t *testing.T) {
	// docs/mcp.md «Подтверждения»: the CLI confirms with --yes, not confirmed=true.
	// tools call help: confirmed is not an argument of --arg / --args-json.
	for _, arg := range []string{"confirmed=false", "confirmed=true", "CONFIRMED=true", "confirmed"} {
		var code int
		if arg == "confirmed" {
			_, _, code = execCLI(t, "tools", "call", "disk_delete", "--arg", arg)
		} else {
			_, _, code = execCLI(t, "tools", "call", "disk_delete", "--arg", "disk_id=00000000-0000-4000-8000-000000000001", "--arg", arg)
		}
		if code != clerrors.ExitGeneral {
			t.Errorf("%s: exit %d, want 1", arg, code)
		}
	}
}

func TestContract_YesDryRunIsDryRun(t *testing.T) {
	// confirm.Ask checks DryRun before Yes. --yes --dry-run is a dry run, exit 0.
	out, errOut, code := execCLI(t, "disk", "delete", "00000000-0000-4000-8000-000000000001", "--yes", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	var m map[string]any
	if json.Unmarshal([]byte(out), &m) != nil || m["status"] != "dry_run" {
		t.Fatalf("stdout %s stderr %s", out, errOut)
	}
}

// Every tree command of vm, image, disk, vpc, k8s, pg, s3, secret with --json:
// stdout is empty or one JSON document, stderr is only JSON lines.
// Error path: no key, and any HTTP answer is an immediate 401 JSON body.
func TestContract_TreeJSONPurity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("EVO_LANG", "en")
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	installContractTransport(t, 401, `{"error":"refused"}`)
	var bad []string
	n := 0
	for _, g := range treeGroups {
		if !contractTree(g.Name) {
			continue
		}
		for _, c := range g.Cmds {
			n++
			args := []string{"--json", g.Name, c.Verb}
			for _, spec := range c.Args {
				if strings.HasSuffix(spec, "?") {
					continue
				}
				name := strings.TrimSuffix(strings.TrimSuffix(spec, "..."), "?")
				name = strings.Split(name, "|")[0]
				if strings.Contains(name, "name") || name == "bucket" || name == "key" || name == "path" || name == "action" || name == "query" {
					args = append(args, "web")
				} else {
					args = append(args, "00000000-0000-4000-8000-000000000001")
				}
			}
			_, out, errOut := runCLI(t, "", args...)
			if why := jsonImpure(out, errOut); why != "" {
				bad = append(bad, strings.Join(args, " ")+": "+why)
			}
		}
	}
	t.Logf("tree commands=%d", n)
	if len(bad) > 0 {
		t.Fatalf("%d commands:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if n < 40 {
		t.Fatalf("sweep saw %d commands", n)
	}
}

func contractTree(name string) bool {
	switch name {
	case "vm", "image", "disk", "vpc", "k8s", "pg", "s3", "secret":
		return true
	}
	return false
}

func jsonImpure(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	if stdout != "" {
		var v any
		dec := json.NewDecoder(strings.NewReader(stdout))
		if err := dec.Decode(&v); err != nil {
			return "stdout " + clipLine(stdout)
		}
		var extra any
		if dec.Decode(&extra) == nil {
			return "stdout has a second JSON value"
		}
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v any
		if json.Unmarshal([]byte(line), &v) != nil {
			return "stderr " + clipLine(line)
		}
	}
	return ""
}

func clipLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 180 {
		return s[:180]
	}
	return s
}

// Success path of the same trees: list commands whose answer is one JSON
// document. The token is injected, so IAM is not called; the transport
// answers every host with an empty list.
func TestContract_TreeJSONSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("CLOUDRU_PROJECT_ID", "00000000-0000-4000-8000-000000000001")
	t.Setenv("EVO_LANG", "en")
	installContractTransport(t, 200, `{"items":[],"total":0,"vms":[],"disks":[],"images":[],"subnets":[],"availability_zones":[],"disk_types":[]}`)
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth = previewToken{}
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })
	var bad []string
	for _, args := range [][]string{
		{"vm", "list", "--json"},
		{"vm", "zones", "--json"},
		{"disk", "list", "--json"},
		{"disk", "types", "--json"},
		{"vpc", "subnets", "--json"},
		{"image", "list", "--json"},
	} {
		code, out, errOut := runCLI(t, "", args...)
		why := jsonImpure(out, errOut)
		if why != "" || code != 0 || strings.TrimSpace(out) == "" {
			bad = append(bad, strings.Join(args, " ")+": exit "+strconv.Itoa(code)+" "+why+" out="+clipLine(out)+" err="+clipLine(errOut))
		}
	}
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

func installContractTransport(t *testing.T, status int, body string) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = contractRT{status: status, body: body}
	t.Cleanup(func() { http.DefaultTransport = old })
}

type contractRT struct {
	status int
	body   string
}

func (c contractRT) RoundTrip(r *http.Request) (*http.Response, error) {
	body := c.body
	// ListAvailabilityZones accepts a JSON array, and treats {"items":[]} as
	// a paginated page with nothing in it, then fails the array decode.
	if c.status == 200 && strings.Contains(r.URL.Path, "availability-zones") {
		body = `[]`
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Retry-After", "0")
	return &http.Response{
		StatusCode:    c.status,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}, nil
}
