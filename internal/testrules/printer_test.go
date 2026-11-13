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
	"sort"
	"strings"
	"testing"
)

// bufferPrinterScope is the code that has no terminal of its own: what it
// prints goes into a tool's answer or its progress, never to a screen. The
// CLI's printers (internal/cli) are the ones that may be a terminal's.
var bufferPrinterScope = []string{"internal/tools", "internal/engine", "internal/deployplan"}

// textPrinterMisses are the places of bufferPrinterScope that make a printer
// in output.ModeText — one that redraws a line with "\r" as on a terminal —
// onto a buffer an answer carries: "file<TAB>function" → the answer. It only
// shrinks. Empty: its deadline 1.7 is met, and a new line comes with a
// deadline of its own and its plan line (release plan 1.7, F9: the printer
// into a buffer is ModePlain, one point).
var textPrinterMisses = map[string]string{}

// F9 (agent runs of Grok on 1.6.0, release plan 1.7): a printer evo makes
// outside the CLI writes to a buffer, so it is never in output.ModeText — a
// live printer's wait line is a carriage return and spaces the model reads
// in the answer (Printer.Live, Progress). The behaviour behind it:
// checkNoTerminalCodes in internal/tools reads every answer of the fault
// matrix and of the slow world.
func TestPrinters_ABufferIsNoTerminal(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string]bool{}
	for _, dir := range bufferPrinterScope {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "NewWithWriter" && sel.Sel.Name != "NewWithWriters") {
						return true
					}
					for _, a := range call.Args {
						if m, ok := a.(*ast.SelectorExpr); ok && m.Sel.Name == "ModeText" {
							found[rel+"\t"+funcName(fd)] = true
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
	for k := range found {
		if _, ok := textPrinterMisses[k]; !ok {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": a printer in output.ModeText onto a buffer — its wait line is \"\\r\" in the answer (F9); make it output.ModePlain")
		}
	}
	for k := range textPrinterMisses {
		if !found[k] {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in textPrinterMisses, but makes no printer in output.ModeText; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}
