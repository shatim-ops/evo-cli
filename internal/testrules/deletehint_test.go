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
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// deleteTool is a word that may name a tool that deletes: *_delete,
// destroy, stack_destroy, release_floating_ip, s3_rm. deleteTools keeps the
// ones the registry has (can_delete is a field, not a tool).
var deleteTool = regexp.MustCompile(`\b(destroy|stack_destroy|release_floating_ip|s3_rm|[a-z0-9_]+_delete)\b`)

// deleteTools are the tools of the registry that delete: destructive, and
// named as one that removes (not s3_put, k8s_apply and the others that
// write over).
func deleteTools() map[string]bool {
	out := map[string]bool{}
	for _, tl := range tools.New(nil).Tools() {
		if tools.IsDestructive(tl) && deleteTool.FindString(tl.Name) == tl.Name {
			out[tl.Name] = true
		}
	}
	return out
}

// deleteFlows are the files that are the flow of a delete the person asked
// for, by the tools they carry on: their texts continue that delete.
var deleteFlows = map[string][]string{
	// The engine of destroy: it deletes the VM and releases its floating IP.
	"internal/engine/destroy.go": {"destroy", "release_floating_ip"},
}

// deleteHintDescribes are the texts that name a delete to say what it does
// or does not do, not to offer it: "file<TAB>function<TAB>tool" → what they
// say.
var deleteHintDescribes = map[string]string{
	"internal/engine/network.go\tShowOpenedPorts\tdestroy":            "destroy will not close ports opened without a rule id",
	"internal/engine/network.go\talreadyOpenText\tdestroy":            "destroy will not touch a rule evo did not create",
	"internal/engine/preview.go\tpendingRulesPreview\tdestroy":        "the rules an earlier destroy left (the preview of the call that removes them)",
	"internal/engine/preview.go\tredeployPreview\tdestroy":            "what an earlier destroy left",
	"internal/engine/preview.go\trulePreview\tdestroy":                "destroy releases the floating IP the deploy creates",
	"internal/tools/tools_stack.go\tnormalizeManifest\tstack_destroy": "a taken s3 key: the stack would overwrite the object and stack_destroy would delete it",
}

// humanDecision are the words by which a text that names a delete leaves
// it to the person (release plan 1.7, block D: «спросите человека, прежде
// чем удалять»), in English and in Russian.
var humanDecision = []string{"ask the person", "спросите человека"}

// deleteHintScope is the code whose text reaches an agent: the answers,
// hints and previews of the tools, the engine's output, the plan's.
var deleteHintScope = []string{"internal/tools", "internal/engine", "internal/deployplan"}

// deleteHintMisses are the texts of deleteHintScope that offer a delete as
// the next step and do not leave it to the person: "file<TAB>function<TAB>
// tool" → why it is a miss. It only shrinks. Closes by 1.7 (release plan
// 1.7, block D; owner's decision 03.11, #732).
var deleteHintMisses = map[string]string{
	"internal/deployplan/apply.go\tApplyWithTimeout\tdestroy":            "a VM of the name in error state: «destroy it first, then retry»",
	"internal/engine/deploy.go\tDeploy\tdestroy":                         "«Run 'evo destroy' to finish it», «Delete it with 'evo destroy'»",
	"internal/engine/deploy.go\tcreateBootFIP\trelease_floating_ip":      "quota of floating IPs: «Delete a floating IP you no longer need»",
	"internal/engine/deploy.go\thandleInterrupt\tdestroy":                "«Use 'evo destroy' to clean up if needed»",
	"internal/engine/deploy.go\tprovisionVM\tdestroy":                    "«or 'evo destroy' to delete it»",
	"internal/engine/deploy.go\tvmStillRunningHint\tdestroy":             "«Use 'evo destroy --yes' to delete it»",
	"internal/engine/status.go\tShowStatus\tdestroy":                     "a stale record: «'evo destroy' removes it and releases its floating IP»",
	"internal/tools/tools.go\tfipLeftAnswer\trelease_floating_ip":        "«release it with release_floating_ip»",
	"internal/tools/tools.go\tprovisionNoPublicIP\tdestroy":              "«or delete the VM with destroy»",
	"internal/tools/tools.go\tprovisionVM\tdestroy":                      "a taken name: «or delete it with destroy name=…» (the VM may be someone else's: P1)",
	"internal/tools/tools_deploy.go\tplanReleaseFloatingIP\tdestroy":     "a floating IP in use: «delete the VM (destroy)» — a larger delete than asked",
	"internal/tools/tools_deploy.go\tplanReleaseFloatingIP\tsnat_delete": "a floating IP of a SNAT gateway: «Delete the SNAT gateway» — a larger delete than asked",
	"internal/tools/tools_disk.go\tdiskAttachedError\tdestroy":           "a boot disk: «delete the VM with destroy» — a larger delete than asked",
	"internal/tools/tools_disk.go\thandleDiskCreate\tdisk_delete":        "«or delete it with disk_delete»",
	"internal/tools/tools_s3.go\tplanS3BucketDelete\ts3_rm":              "a bucket with objects: «delete them with s3_rm» — a larger delete than asked",
	"internal/tools/tools_snat.go\thandleSnatCreate\tsnat_delete":        "«Delete it with snat_delete when no longer needed»",
	"internal/tools/tools_stack.go\tcheckStackSecrets\tstack_destroy":    "a taken secret path: «or stack_destroy that job first»",
	"internal/tools/tools_stack.go\thandleStackPlan\tstack_destroy":      "earlier jobs: «Run stack_destroy on them first»",
	"internal/tools/tools_stack.go\tnormalizeManifest\ts3_rm":            "a taken s3 key: «or remove the object (s3_rm)»",
}

// Release plan 1.7, block D (owner's decision 03.11, #732: «убери за собой»
// is not in the core; the agent creates, the person decides what to
// delete): a text of evo that names a tool that deletes is either the
// tool's own — its preview or its answer, in a function named after it
// (planDiskDelete for disk_delete, destroyAnswer for destroy) or in a file
// of its flow (deleteFlows) — says what the delete does without offering
// it (deleteHintDescribes), or leaves the delete to the person
// (humanDecision). Anything else is a line of deleteHintMisses: the names
// of deletes in texts are a closed list. A text only for the agent's eye, not a sentence (a
// tool's name, a map key, a case), is not a hint: the scan reads the
// literals with a space in them, outside the declarations of the tools
// (registerTools: Name, Description) and outside comparisons and cases.
func TestHints_DeleteIsThePersonsDecision(t *testing.T) {
	root := filepath.Join("..", "..")
	dels := deleteTools()
	if len(dels) < 20 {
		t.Fatalf("%d tools that delete in the registry: the filter sees nothing", len(dels))
	}
	found := map[string][]string{}
	described := map[string]bool{}
	for _, dir := range deleteHintScope {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil || fd.Name.Name == "registerTools" {
					continue
				}
				own := strings.ToLower(strings.ReplaceAll(fd.Name.Name, "_", ""))
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CaseClause, *ast.BinaryExpr, *ast.IndexExpr:
						return false
					case *ast.BasicLit:
						if x.Kind != token.STRING {
							return true
						}
						s, err := strconv.Unquote(x.Value)
						if err != nil || !strings.Contains(s, " ") || leavesItToThePerson(s) {
							return true
						}
						for _, m := range deleteTool.FindAllString(s, -1) {
							if !dels[m] || strings.Contains(own, strings.ReplaceAll(m, "_", "")) || slices.Contains(deleteFlows[rel], m) {
								continue // not a tool, or the tool's own preview or answer
							}
							k := rel + "\t" + fd.Name.Name + "\t" + m
							if _, ok := deleteHintDescribes[k]; ok {
								described[k] = true
								continue
							}
							found[k] = append(found[k], fset.Position(x.Pos()).String()+": "+s)
						}
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var bad []string
	for k, texts := range found {
		if _, ok := deleteHintMisses[k]; !ok {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+" — a text offers the delete and does not leave it to the person (\""+humanDecision[0]+"\"):\n    "+strings.Join(texts, "\n    "))
		}
	}
	for k := range deleteHintDescribes {
		if !described[k] {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+": in deleteHintDescribes, but no text there names the delete; take the line out")
		}
	}
	for k := range deleteHintMisses {
		if _, ok := found[k]; !ok {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+": in deleteHintMisses, but no text there offers the delete; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("deleteHintMisses: %d lines (Closes by 1.7)", len(deleteHintMisses))
}

func leavesItToThePerson(s string) bool {
	low := strings.ToLower(s)
	for _, w := range humanDecision {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}
