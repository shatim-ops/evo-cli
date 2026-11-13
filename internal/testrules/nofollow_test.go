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
	"slices"
	"sort"
	"strings"
	"testing"
)

// directWrites are the places of production code that open a file for
// writing by its path — os.WriteFile, os.Create, os.OpenFile with a flag of
// writing: "file<TAB>function" → how many such calls it has and why it may. Such a call follows a symlink
// at the path and writes to its target. A write to evo's own files goes
// through the helper that refuses a link (O_NOFOLLOW) or through
// config.WriteFileAtomic (rename replaces a link). The places of R-67 (а)
// are here until «разработка» moves them to the helper: the list only
// shrinks.
type directWrite struct {
	n   int
	why string
}

var directWrites = map[string]directWrite{
	"internal/config/nofollow.go\tOpenNoFollow": {1, "the helper itself: O_NOFOLLOW and a regular file only (ledger 1.8, № 29)"},
	"internal/cli/main.go\tignoreInGit":         {1, "O_CREATE|O_EXCL: an existing file or symlink fails the open, nothing is written through it"},
	// httpretry cannot import config (config → provision → httpretry).
	"internal/httpretry/record.go\trecorded": {1, "the file EVO_RECORD_ANSWERS names (cloud-e2e), not one of a project or a clone; a link in its place is refused by Lstat before the open"},
}

// writeFlags are the flags of os.OpenFile that make it a write.
var writeFlags = map[string]bool{"O_WRONLY": true, "O_RDWR": true, "O_CREATE": true, "O_APPEND": true, "O_TRUNC": true}

// R-67 («Риск», conclusions 60–61; release plan 1.8, «R-67: запись evo
// через symlink из клонированного репозитория», (б)): evo's data directory
// lives in the project, and a symlink committed to a repository comes with
// git clone — deploy wrote known_hosts through it to its target. A write by
// path in production code is declared in directWrites with its reason; a
// new one fails the build, and so does a line whose place is gone.
func TestWrites_NoNewWriteThroughALink(t *testing.T) {
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
					if !ok {
						return true
					}
					pkg, name := local[id.Name], sel.Sel.Name
					write := false
					switch {
					case pkg == "os" && (name == "WriteFile" || name == "Create"):
						write = true
					case pkg == "io/ioutil" && name == "WriteFile":
						write = true
					case pkg == "os" && name == "OpenFile" && len(call.Args) > 1:
						write = hasWriteFlag(call.Args[1])
					}
					if write {
						found[key] = append(found[key], fset.Position(call.Pos()).String()+": "+pkg+"."+name)
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
		switch w := directWrites[k]; {
		case w.why == "":
			t.Errorf("%s: a write by path follows a symlink to its target (R-67) — write through the helper with O_NOFOLLOW or config.WriteFileAtomic, or declare it in directWrites with the reason", strings.Join(found[k], "; "))
		case len(found[k]) != w.n:
			t.Errorf("%s: %d writes by path, directWrites declares %d — a new one goes through the helper: %s", k, len(found[k]), w.n, strings.Join(found[k], "; "))
		}
	}
	for k := range directWrites {
		if found[k] == nil {
			t.Errorf("directWrites names %q, which writes by path no more: remove the line", k)
		}
	}
	if len(found) == 0 {
		t.Fatal("no write by path found: the scan sees nothing")
	}
}

// hasWriteFlag tells a flag expression of os.OpenFile that names a flag of
// writing (os.O_CREATE|os.O_RDWR…), or one the scan cannot read — a
// variable, which is taken for a write.
func hasWriteFlag(e ast.Expr) bool {
	found, unknown := false, false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if writeFlags[x.Sel.Name] {
				found = true
			}
			return false
		case *ast.Ident:
			unknown = true
		}
		return true
	})
	return found || unknown
}

// readsBeforeWrite are the functions of production code that read a file
// by its path — os.ReadFile, os.Open, os.OpenFile without a flag of writing
// — in a package where a function that leads to it also leads to a write
// (calls within the package, at any depth: SaveProfile reads through
// readSettingsFile and readYAML, then writes). "file<TAB>function" (the one
// that reads) → how many such reads and why they may. A read by path
// follows a symlink, so what is written back as evo's own file may be the
// target's content — a private key behind .evo/known_hosts (#1179). Such a
// read goes through config.ReadFileNoFollow or config.OpenNoFollow; a read
// of another file than the one written, or one after Lstat «a regular
// file», is declared here with its reason.
var readsBeforeWrite = map[string]directWrite{
	"internal/auth/cache.go\tFileCache.Load":            {1, "the token cache: parsed as JSON with this key's key_id, else a miss; what Store writes is a new token, not what was read"},
	"internal/cli/agent.go\treadUserFile":               {1, "a client's file in the home directory: Lstat refuses a symlink and a non-regular file before the read"},
	"internal/cli/tools.go\tbuildToolArgs":              {1, "--args-file the person names: input, parsed as JSON arguments, not a file of evo written back"},
	"internal/cli/trees.go\treadArgFile":                {1, "a file argument the person names: input of the command, not a file of evo written back"},
	"internal/cli/trees.go\tsecretRef.readRaw":          {1, "a secret file the person names (--password-file and the like): input, not a file of evo written back"},
	"internal/config/profile.go\treadYAML":              {1, "settings and credentials in the configuration directory of the home, not of a project: parsed into the profile structure, a target that is not one is an error"},
	"internal/state/state.go\tRead":                     {1, "checkPlace refuses a symlink and a non-regular file by Lstat before the read; parsed as JSON of the state, else STATE_CORRUPTED"},
	"internal/cli/main.go\treadKeyFile":                 {1, "the SSH key: Lstat «a regular file», then os.SameFile of the file opened — OpenNoFollow's check, with the error of the key"},
	"internal/config/profile.go\tMigrateLegacyDir":      {1, "the legacy directory in the home directory: Lstat refuses a non-regular file; copied to another file of the new directory"},
	"internal/tools/completion.go\treadCompletionCache": {1, "parsed as JSON of the cache, else a miss; what writeCompletionCache writes is new items from the platform, not what was read"},
}

// fileOp is what a function of production code does with files by path:
// its reads (positions), whether it writes, and the functions of its own
// package it calls, by name.
type fileOp struct {
	file  string
	reads []string
	write bool
	calls []string
}

// R-67, read by a link («Риск», conclusion 66, recommendation (а); release
// plan 1.8, «R-67, чтение по ссылке»): pinHostKey read .evo/known_hosts with
// os.ReadFile and wrote it back with WriteFileAtomic — a symlink to a
// private key committed to a repository made the key a file of the
// project. A read by path that a function of its package reaches together
// with a write (readUserFile and agentChange.write; readYAML and
// SaveProfile) goes through the helper or is declared with its reason; a
// new one fails the build, and so does a line whose place is gone. A read
// and a write in different packages are not seen.
func TestWrites_NoReadThroughALinkBeforeAWrite(t *testing.T) {
	root := filepath.Join("..", "..")
	// package directory → function name → what it does
	pkgs := map[string]map[string]*fileOp{}
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
			fns := pkgs[filepath.Dir(rel)]
			if fns == nil {
				fns = map[string]*fileOp{}
				pkgs[filepath.Dir(rel)] = fns
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				op := &fileOp{file: rel}
				fns[funcName(fd)] = op
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					pkg, name, own := calleeOf(call, local, rel)
					switch {
					case own:
						op.calls = append(op.calls, name)
					case pkg == "os" && (name == "ReadFile" || name == "Open"),
						pkg == "io/ioutil" && name == "ReadFile",
						pkg == "os" && name == "OpenFile" && len(call.Args) > 1 && !hasWriteFlag(call.Args[1]):
						op.reads = append(op.reads, fset.Position(call.Pos()).String()+": "+pkg+"."+name)
					}
					if pkg == configPkg && (name == "WriteFileAtomic" || name == "WriteFilePrivate") ||
						pkg == configPkg && name == "OpenNoFollow" && len(call.Args) > 1 && hasWriteFlag(call.Args[1]) ||
						pkg == "os" && (name == "WriteFile" || name == "Create") ||
						pkg == "io/ioutil" && name == "WriteFile" ||
						pkg == "os" && name == "OpenFile" && len(call.Args) > 1 && hasWriteFlag(call.Args[1]) {
						op.write = true
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
	found := map[string][]string{}
	writers := 0
	for _, fns := range pkgs {
		// resolve is the functions of the package a call names: a method
		// call (*.Name) is every method of that name.
		resolve := func(c string) []string {
			if m, ok := strings.CutPrefix(c, "*."); ok {
				var out []string
				for k := range fns {
					if strings.HasSuffix(k, "."+m) {
						out = append(out, k)
					}
				}
				return out
			}
			return []string{c}
		}
		// reach is every function of the package a function calls, at any
		// depth, itself included: SaveProfile reads settings through
		// readSettingsFile and readYAML.
		reach := func(name string) []string {
			seen := map[string]bool{name: true}
			out, todo := []string{name}, []string{name}
			for len(todo) > 0 {
				op := fns[todo[0]]
				todo = todo[1:]
				if op == nil {
					continue
				}
				for _, c := range op.calls {
					for _, k := range resolve(c) {
						if !seen[k] {
							seen[k] = true
							out = append(out, k)
							todo = append(todo, k)
						}
					}
				}
			}
			return out
		}
		for name, op := range fns {
			if !op.write {
				continue
			}
			writers++
			// The function that writes, and every one that leads to it in
			// its package, is a place where a read is followed by a write.
			for caller := range fns {
				r := reach(caller)
				if !slices.Contains(r, name) {
					continue
				}
				for _, k := range r {
					if o := fns[k]; o != nil && len(o.reads) > 0 {
						found[o.file+"\t"+k] = o.reads
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch r := readsBeforeWrite[k]; {
		case r.why == "":
			t.Errorf("%s (%s): a read by path where a file is written after — a symlink's target becomes what is written back (R-67, conclusion 66): read through config.ReadFileNoFollow or config.OpenNoFollow, or declare it in readsBeforeWrite with the reason", strings.Join(found[k], "; "), k)
		case len(found[k]) != r.n:
			t.Errorf("%s: %d reads by path, readsBeforeWrite declares %d — a new one goes through the helper: %s", k, len(found[k]), r.n, strings.Join(found[k], "; "))
		}
	}
	for k := range readsBeforeWrite {
		if found[k] == nil {
			t.Errorf("readsBeforeWrite names %q, which no more reads by path before a write: remove the line", k)
		}
	}
	if writers == 0 {
		t.Fatal("no function that writes a file found: the scan sees nothing")
	}
}

const configPkg = "github.com/shatim-ops/evo-cli/internal/config"

// calleeOf names the package and function of a call through an import, or
// — own — a function or method of the call's own package by its name
// (a method as Type.Name when the receiver is not a package: matched by
// name alone, so x.write() is any write method of the package). Inside
// config, WriteFileAtomic and OpenNoFollow are config's.
func calleeOf(call *ast.CallExpr, local map[string]string, rel string) (pkg, name string, own bool) {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok && local[id.Name] != "" {
			return local[id.Name], fn.Sel.Name, false
		}
		return "", "*." + fn.Sel.Name, true
	case *ast.Ident:
		if strings.HasPrefix(rel, "internal/config/") && (fn.Name == "WriteFileAtomic" || fn.Name == "WriteFilePrivate" || fn.Name == "OpenNoFollow") {
			return configPkg, fn.Name, false
		}
		return "", fn.Name, true
	}
	return "", "", false
}
