// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The user documentation is checked against the code: docs/commands.md is
// generated from the command tree, and docs/auth.md has to name every
// environment variable the code reads.

var updateDocs = flag.Bool("update-docs", false, "rewrite docs/commands.md from the command tree")

const (
	repoRoot        = "../.."
	commandsDocPath = repoRoot + "/docs/commands.md"
	authDocPath     = repoRoot + "/docs/auth.md"
	regenerateHint  = "go test ./internal/cli -run TestCommandsDoc -update-docs"
)

const commandsDocHeader = `# Справка по командам

<!-- Файл сгенерирован из дерева команд (cobra/doc). Руками не править:
     ` + regenerateHint + `
     CI сверяет файл с бинарём (TestCommandsDoc). -->

То же самое печатает ` + "`evo <команда> --help`" + `; с ` + "`EVO_LANG=en`" + ` справка
английская (решение D6, пересмотр). Заголовки разделов (Synopsis, Options)
ставит генератор cobra/doc. В деревьях ` + "`vm`" + ` и ` + "`disk`" + ` описания команд и флаги
русские; в остальных деревьях они пока на английском, как в MCP. Подробнее — в
[README](../README.md) и соседних файлах ` + "`docs/`" + `.

`

// genCommandsDoc renders the help of every visible command into one
// Markdown file, in the order of the command tree. The file is the Russian
// help, whatever language the tests run in.
func genCommandsDoc() ([]byte, error) {
	return genCommandsDocIn(i18n.RU)
}

// genCommandsDocIn renders the command tree in the given language.
func genCommandsDocIn(lang string) ([]byte, error) {
	prev := i18n.Lang()
	i18n.SetLang(lang)
	defer i18n.SetLang(prev)
	root := newRootCmd()
	// cobra adds these two on Execute; the binary has them, so the docs too.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	var cmds []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c != root && (!c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand()) {
			return
		}
		cmds = append(cmds, c)
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)

	var buf bytes.Buffer
	buf.WriteString(commandsDocHeader)
	buf.WriteString("## Содержание\n\n")
	for _, c := range cmds {
		depth := strings.Count(c.CommandPath(), " ")
		buf.WriteString(strings.Repeat("  ", depth) + "- [" + c.CommandPath() + "](#" + anchor(c.CommandPath()) + ")\n")
	}
	buf.WriteString("\n")

	// Links between commands point at headings of this same file.
	link := func(name string) string {
		return "#" + anchor(strings.ReplaceAll(strings.TrimSuffix(name, ".md"), "_", " "))
	}
	for _, c := range cmds {
		c.DisableAutoGenTag = true
		if c.Long != "" {
			// Help text is laid out for a terminal: keep it as is.
			c.Long = "```text\n" + strings.TrimRight(c.Long, "\n") + "\n```"
		}
		if err := doc.GenMarkdownCustom(c, &buf, link); err != nil {
			return nil, err
		}
	}
	// cobra/doc prints the flags without the help template: translate the
	// pflag defaults the same way --help does.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if lang == i18n.RU {
		out = []byte(ruDefaults(string(out)))
	}
	return out, nil
}

// anchor is the GitHub heading anchor of "## <path>".
func anchor(path string) string {
	return strings.ReplaceAll(strings.ToLower(path), " ", "-")
}

func TestCommandsDoc(t *testing.T) {
	got, err := genCommandsDoc()
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateDocs {
		if err := os.WriteFile(commandsDocPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(commandsDocPath)
	if err != nil {
		t.Fatalf("%v; generate it: %s", err, regenerateHint)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("docs/commands.md differs from the command tree (first difference at line %d); regenerate: %s",
			firstDiffLine(got, want), regenerateHint)
	}
}

func firstDiffLine(a, b []byte) int {
	la, lb := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return i + 1
		}
	}
	return min(len(la), len(lb)) + 1
}

func TestCommandsDoc_CoversEveryCommand(t *testing.T) {
	data, err := genCommandsDoc()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"evo configure", "evo vm create", "evo tools call", "evo mcp", "evo completion bash"} {
		if !bytes.Contains(data, []byte("\n## "+path+"\n")) {
			t.Errorf("no section for %q", path)
		}
	}
	if bytes.Contains(data, []byte("## evo mcp healthcheck")) {
		t.Error("hidden command mcp healthcheck is documented")
	}
}

// codeEnvVars lists the environment variables read by the non-test Go code:
// arguments of os.Getenv / os.LookupEnv (string literals or string
// constants), `env:"NAME"` struct tags and the entries of envMapping.
func codeEnvVars(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .git, .claude (worktrees of other checkouts) and the like.
			if path != repoRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			switch d.Name() {
			case "vendor", "testdata", "dist", "bin", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// String constants by name, across packages: os.Getenv(EnvConfigDir).
	consts := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := stringLit(vs.Values[i]); ok {
							consts[name.Name] = s
						}
					}
				}
			}
		}
	}

	found := map[string][]string{}
	add := func(name string, pos token.Pos) {
		found[name] = append(found[name], fset.Position(pos).String())
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) == 0 {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" ||
					(sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
					return true
				}
				switch arg := n.Args[0].(type) {
				case *ast.BasicLit:
					if s, ok := stringLit(arg); ok {
						add(s, arg.Pos())
					}
				case *ast.Ident:
					if s, ok := consts[arg.Name]; ok {
						add(s, arg.Pos())
					} else {
						t.Errorf("%s: os.%s(%s): cannot resolve the variable name", fset.Position(arg.Pos()), sel.Sel.Name, arg.Name)
					}
				case *ast.SelectorExpr:
					if arg.Sel.Name == "envVar" {
						// os.Getenv(m.envVar) in the loop over envMapping,
						// whose entries are collected below.
						return true
					}
					if s, ok := consts[arg.Sel.Name]; ok {
						add(s, arg.Pos())
					} else {
						t.Errorf("%s: os.%s(…%s): cannot resolve the variable name", fset.Position(arg.Pos()), sel.Sel.Name, arg.Sel.Name)
					}
				default:
					t.Errorf("%s: os.%s with a computed name: list it in docs/auth.md and teach this test", fset.Position(n.Pos()), sel.Sel.Name)
				}
			case *ast.Field:
				if n.Tag != nil {
					tag, _ := strconv.Unquote(n.Tag.Value)
					if name, _, _ := strings.Cut(reflect.StructTag(tag).Get("env"), ","); name != "" {
						add(name, n.Tag.Pos())
					}
				}
			case *ast.ValueSpec:
				for i, name := range n.Names {
					if name.Name != "envMapping" || i >= len(n.Values) {
						continue
					}
					ast.Inspect(n.Values[i], func(m ast.Node) bool {
						if cl, ok := m.(*ast.CompositeLit); ok && len(cl.Elts) > 0 {
							if s, ok := stringLit(cl.Elts[0]); ok {
								add(s, cl.Pos())
							}
						}
						return true
					})
				}
			}
			return true
		})
	}
	return found
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func TestAuthDoc_ListsEveryEnvVar(t *testing.T) {
	vars := codeEnvVars(t)
	// The scanner must keep finding what we know is there; an empty result
	// would make the check below pass for nothing.
	for _, known := range []string{"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_API_KEY", "CLOUDRU_PROFILE",
		"CLOUDRU_CONFIG_DIR", "CLOUDRU_PROJECT_ID", "CLOUDRU_S3_ENDPOINT", "CLOUDRU_AGREEMENT_ID", "MCP_BEARER_TOKEN"} {
		if _, ok := vars[known]; !ok {
			t.Errorf("scanner did not find %s in the code", known)
		}
	}

	data, err := os.ReadFile(authDocPath)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		re := regexp.MustCompile(`(^|[^A-Z0-9_])` + regexp.QuoteMeta(name) + `($|[^A-Z0-9_])`)
		if !re.Match(data) {
			t.Errorf("docs/auth.md does not mention %s (read at %s)", name, strings.Join(vars[name], ", "))
		}
	}
}
