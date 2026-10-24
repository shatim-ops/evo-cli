// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gateFile is the one place that reads confirmed: the gate of the
// registry, planner → gate → execution (release plan 1.3, W4).
const gateFile = "internal/tools/gate.go"

// inlineGates are the handlers that still read confirmed themselves,
// "file: function" → the work item of the release plan that moves the
// handler onto the gate. The list only shrinks: a read not here fails the
// test, so does a line with no read, and inlineGatesLeft is lowered with
// each line removed (G13-3, class K1).
var inlineGates = map[string]string{
	"internal/tools/tools.go: destroyTargeted":                  "W5",
	"internal/tools/tools.go: handleDeploy":                     "W4",
	"internal/tools/tools.go: handleDestroy":                    "W5",
	"internal/tools/tools.go: handleProvision":                  "W5",
	"internal/tools/tools_ar.go: handleArCreate":                "W4",
	"internal/tools/tools_ar.go: handleArDelete":                "W4",
	"internal/tools/tools_ca.go: handleCaDelete":                "W4",
	"internal/tools/tools_ca.go: handleCaDeploy":                "W6",
	"internal/tools/tools_ca.go: handleCaStartStop":             "W4",
	"internal/tools/tools_certmanager.go: handleCertDelete":     "W4",
	"internal/tools/tools_certmanager.go: handleCertImport":     "W4",
	"internal/tools/tools_certmanager.go: handleCertIssueLE":    "W4",
	"internal/tools/tools_certmanager.go: handleCertPrivateKey": "W4",
	"internal/tools/tools_certmanager.go: handleCertRenew":      "W4",
	"internal/tools/tools_deploy.go: handleDeployApply":         "W4",
	"internal/tools/tools_deploy.go: handleOpenPorts":           "W5",
	"internal/tools/tools_deploy.go: handleReleaseFloatingIP":   "W4",
	"internal/tools/tools_disk.go: handleDiskAttach":            "W4",
	"internal/tools/tools_disk.go: handleDiskCreate":            "W4",
	"internal/tools/tools_disk.go: handleDiskDelete":            "W4",
	"internal/tools/tools_disk.go: handleDiskDetach":            "W4",
	"internal/tools/tools_disk.go: handleDiskResize":            "W5",
	"internal/tools/tools_dns.go: handleDnsRecordCreate":        "W4",
	"internal/tools/tools_dns.go: handleDnsRecordDelete":        "W4",
	"internal/tools/tools_dns.go: handleDnsZoneCreate":          "W4",
	"internal/tools/tools_dns.go: handleDnsZoneDelete":          "W4",
	"internal/tools/tools_image.go: handleFipAttach":            "W4",
	"internal/tools/tools_image.go: handleImageCreate":          "W4",
	"internal/tools/tools_image.go: handleImageDelete":          "W4",
	"internal/tools/tools_k8s.go: handleK8sAddNodePool":         "W4",
	"internal/tools/tools_k8s.go: handleK8sCreate":              "W4",
	"internal/tools/tools_k8s.go: handleK8sDelete":              "W4",
	"internal/tools/tools_k8s_addons.go: handleK8sAddAddon":     "W4",
	"internal/tools/tools_kafka.go: handleKafkaCreate":          "W6",
	"internal/tools/tools_kafka.go: handleKafkaDelete":          "W4",
	"internal/tools/tools_kube.go: handleK8sApply":              "W4",
	"internal/tools/tools_kube.go: handleK8sHelm":               "W4",
	"internal/tools/tools_kube.go: handleK8sKubectl":            "W4",
	"internal/tools/tools_nlb.go: handleLbCreate":               "W4",
	"internal/tools/tools_nlb.go: handleLbDelete":               "W4",
	"internal/tools/tools_nlb.go: handleTgCreate":               "W4",
	"internal/tools/tools_nlb.go: handleTgDelete":               "W4",
	"internal/tools/tools_pg.go: handlePgBackupCreate":          "W4",
	"internal/tools/tools_pg.go: handlePgCreate":                "W6",
	"internal/tools/tools_pg.go: handlePgDbCreate":              "W4",
	"internal/tools/tools_pg.go: handlePgDbDelete":              "W4",
	"internal/tools/tools_pg.go: handlePgDelete":                "W4",
	"internal/tools/tools_pg.go: handlePgUserCreate":            "W6",
	"internal/tools/tools_pg.go: handlePgUserDelete":            "W4",
	"internal/tools/tools_redis.go: handleRedisCreate":          "W6",
	"internal/tools/tools_redis.go: handleRedisDelete":          "W4",
	"internal/tools/tools_s3.go: handleS3BucketCreate":          "W4",
	"internal/tools/tools_s3.go: handleS3BucketDelete":          "W6",
	"internal/tools/tools_s3.go: handleS3Put":                   "W4",
	"internal/tools/tools_s3.go: handleS3Rm":                    "W4",
	"internal/tools/tools_secret.go: handleSecretAddVersion":    "W4",
	"internal/tools/tools_secret.go: handleSecretCreate":        "W4",
	"internal/tools/tools_secret.go: handleSecretDelete":        "W4",
	"internal/tools/tools_secret.go: handleSecretValue":         "W4",
	"internal/tools/tools_snat.go: handleSnatCreate":            "W4",
	"internal/tools/tools_snat.go: handleSnatDelete":            "W4",
	"internal/tools/tools_stack.go: handleStackApply":           "W4",
	"internal/tools/tools_stack.go: handleStackDestroy":         "W4",
	"internal/tools/tools_vpc.go: handleVpcCreate":              "W4",
	"internal/tools/tools_vpc.go: handleVpcDelete":              "W4",
	"internal/tools/tools_vpc.go: handleVpcRouteCreate":         "W4",
	"internal/tools/tools_vpc.go: handleVpcRouteDelete":         "W4",
}

// inlineGatesLeft is len(inlineGates): a line added back fails the test
// even with the read it names.
const inlineGatesLeft = 67

// notGates read the key "confirmed" without deciding on consent.
var notGates = map[string]string{
	"internal/cli/tools.go: buildToolArgs":                 "refuses --arg confirmed: the CLI confirms with --yes or the prompt",
	"internal/cli/tools.go: takesConfirmed":                "the schema of the tool: does it take confirmed",
	"internal/tools/descriptions_ru.go: argDescriptionsRU": "the Russian description of the argument",
	"internal/tools/registry.go: takesConsent":             "the schema of the tool: tools.NeedsConsent",
}

// CLAUDE.md §4: a call bound to fail is refused before the question of
// consent. Where each handler reads confirmed itself, the checks it makes
// after that read are made after the question (G13-3): the gate is one
// function of the registry, and production code reads confirmed — an index
// "confirmed" or "confirmed" passed to a call — only in gateFile, the
// handlers of inlineGates and the reads of notGates.
func TestConfirmedReadOnlyInTheGate(t *testing.T) {
	if len(inlineGates) != inlineGatesLeft {
		t.Errorf("inlineGates has %d lines, inlineGatesLeft says %d: the list only shrinks — lower inlineGatesLeft with each line removed, never add one",
			len(inlineGates), inlineGatesLeft)
	}
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if rel == gateFile {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				key := rel + ": " + declName(decl)
				for _, pos := range confirmedReads(decl) {
					seen[key] = true
					if inlineGates[key] == "" && notGates[key] == "" {
						t.Errorf("%s (%s): reads confirmed outside the gate (%s); move the check onto the gate",
							key, fset.Position(pos), gateFile)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, list := range []map[string]string{inlineGates, notGates} {
		for key := range list {
			if !seen[key] {
				t.Errorf("%s no longer reads confirmed: remove its line (and lower inlineGatesLeft)", key)
			}
		}
	}
}

// confirmedReads are the reads of confirmed in decl: x["confirmed"] other
// than the left side of an assignment (a call of a tool sets it), and
// "confirmed" passed to a call.
func confirmedReads(decl ast.Decl) []token.Pos {
	set := map[ast.Node]bool{}
	var out []token.Pos
	ast.Inspect(decl, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				set[l] = true
			}
		case *ast.IndexExpr:
			if !set[x] && isConfirmed(x.Index) {
				out = append(out, x.Pos())
			}
		case *ast.CallExpr:
			for _, a := range x.Args {
				if isConfirmed(a) {
					out = append(out, a.Pos())
				}
			}
		}
		return true
	})
	return out
}

func isConfirmed(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == "confirmed"
}

// The rule sees a read however it is written, and not a write.
func TestConfirmedReadForms(t *testing.T) {
	for src, want := range map[string]int{
		`func f(a map[string]any) { c, _ := a["confirmed"].(bool); _ = c }`:    1,
		`func f(a map[string]any) bool { _, ok := a["confirmed"]; return ok }`: 1,
		`func f(a map[string]any) { g(a, "confirmed") }`:                       1,
		`func f(a map[string]any) { a["confirmed"] = true }`:                   0,
		`func f() map[string]any { return map[string]any{"confirmed": true} }`: 0,
		`func f(a map[string]any) { _ = a["name"] }`:                           0,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n"+src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(confirmedReads(f.Decls[0])); got != want {
			t.Errorf("%s: %d reads, want %d", src, got, want)
		}
	}
}
