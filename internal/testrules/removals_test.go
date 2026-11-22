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

// removals are the functions of production code that remove a file by
// its path (os.Remove, os.RemoveAll): "file<TAB>function" → how many such
// calls and where the path comes from. A path built from what evo read —
// the content of a file of the project, which comes with git clone — can
// name any file: an intent with id "../state" removed .evo/state.json
// (full audit 1.8, FB180-1). The path of each removal is evo's own (made
// here, a random name, a temporary file) or checked against the form evo
// makes before it is joined. Not a list of misses: each line is a removal
// whose path is evo's own, declared with why; a new one is a new line.
var removals = map[string]directWrite{
	"internal/tools/preview_args.go\tRegistry.planLbCreate": {1, "the declared state directory, fixed lb-previews directory, and SHA-256 of the current caller/session and optional stack job scope plus .json; ReadFileNoFollow reads this same file and the stored offer supplies no path"},
	"internal/tools/tools_k8s_upgrade.go\treserveUpgrade":   {1, "the local preview slot derived from a SHA-256 of the transport session; no path read from a file"},
	"internal/intent/manual.go\tManualSnapshot.Apply":       {1, "the private staging directory os.MkdirTemp made in this call under the verified decision directory"},
	"internal/auth/cache.go\tFileCache.Delete":              {1, "the token cache's own path, set by evo from the configuration directory"},
	"internal/cli/login.go\twritableConfigDir":              {1, "the probe file os.CreateTemp made in this call"},
	"internal/config/profile.go\tMigrateLegacyDir":          {2, "the migration's own temporary directory: the configuration base and the pid, no name read"},
	"internal/config/profile.go\tDeleteProfile":             {1, "the token cache of a profile whose name, read from the file, is checked by ValidateProfileName before the path"},
	"internal/config/profile.go\twriteFileAtomic":           {1, "the temporary file os.CreateTemp made in this call"},
	"internal/tools/made_repeat.go\tRegistry.repeatOfMade":  {1, "the file madeKey names: a hash evo makes of the call, no name read"},
	"internal/tools/made_repeat.go\tforgetMadeOf":           {1, "a file of made/ whose name has the form madeKey makes (madeName), not a symlink: no path read from a file"},
	"internal/deployplan/store.go\tDelete":                  {1, "the plan id is checked by validPlanID (the form newPlanID makes) before the join"},
	"internal/intent/intent.go\tremove":                     {1, "Close and Settle: the id is a record file's name, checked against begin's form (recordPath); never the content's"},
	"internal/history/prune.go\tPrune":                      {2, "a file of a day of the journal: a name of the directory's listing that matches evo's form (fileForm, a date), never a line's content; the mark of a pruning put off (deferredName), a constant"},
	"internal/kubetools/kubetools.go\trun":                  {1, "the temporary file os.CreateTemp made in this call"},
	"internal/state/state.go\tWrite":                        {2, "the temporary file os.CreateTemp made in this call"},
	"internal/tools/tools_kube.go\tRegistry.planK8sHelm":    {1, "the temporary values file os.CreateTemp made in this call"},
}

// Full audit 1.8, FB180-1: the path a removal is joined from is not read
// from a file. Each os.Remove and os.RemoveAll of production code is
// declared in removals with where its path comes from; a new one fails
// the build, and so does a line whose place is gone.
func TestRemovals_PathIsNotReadFromAFile(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string][]string{}
	for _, dir := range []string{"cmd", "internal"} {
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
			local := importNames(f)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + "\t" + funcName(fd)
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					id, ok := sel.X.(*ast.Ident)
					if ok && local[id.Name] == "os" && (sel.Sel.Name == "Remove" || sel.Sel.Name == "RemoveAll") {
						found[key] = append(found[key], fset.Position(call.Pos()).String())
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
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch r := removals[k]; {
		case r.why == "":
			t.Errorf("%s (%s): a removal by path — its path must not come from what evo read (FB180-1): build it from evo's own name or check it against the form evo makes, then declare it in removals with where the path comes from", strings.Join(found[k], "; "), k)
		case len(found[k]) != r.n:
			t.Errorf("%s: %d removals by path, removals declares %d: %s", k, len(found[k]), r.n, strings.Join(found[k], "; "))
		}
	}
	for k := range removals {
		if found[k] == nil {
			t.Errorf("removals names %q, which removes by path no more: remove the line", k)
		}
	}
}
