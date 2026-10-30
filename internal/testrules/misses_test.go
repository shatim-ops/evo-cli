// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	// missDeadline is the version by which a line of a list of misses is
	// gone: «Closes by 1.8» in the list's doc comment (every line), or in
	// the text of a line (that line).
	missDeadline = regexp.MustCompile(`Closes by (\d+)\.(\d+)`)
	// releasedVersion is a released section of CHANGELOG.md.
	releasedVersion = regexp.MustCompile(`(?m)^## \[(\d+)\.(\d+)\.\d+\]`)
)

// missList is a list of misses: a map of a test whose doc says it only
// shrinks.
type missList struct {
	where, name string
	deadline    [2]int // of the list; 0.0 — none
	lines       int
	byLine      map[[2]int]int // lines with a deadline of their own
}

// Release plan 1.6, «Одна декларация инструмента и храповики со сроком»
// (audits/process-test-strategy-1.6.md §5 п. 7; owner's decision 29.10,
// #578): the lists of misses (humanUnnamed, bodyUnnamed, idWithoutFormat
// and the others — a map of a test whose doc says it «only shrinks») shrink
// only when someone closes a line, and a line nobody closes lives forever.
// Each list says by which version its lines are gone («Closes by 1.N» in
// its doc comment; a line may name an earlier one in its text), at most two
// versions on from the one it is written in. Every PR: each list has a
// deadline, and the count of lines is logged. The weekly run
// (EVO_MISS_DEADLINE=1, weekly-misses.yml): a line whose deadline a
// released version (CHANGELOG.md) has reached fails — a red weekly run and
// an issue, not a red PR.
func TestMisses_HaveADeadline(t *testing.T) {
	root := filepath.Join("..", "..")
	lists := missLists(t, root)
	if len(lists) < 10 {
		t.Fatalf("%d lists of misses found: the scan sees nothing, update missLists", len(lists))
	}
	released := latestReleased(t, root)
	strict := os.Getenv("EVO_MISS_DEADLINE") == "1"
	total, overdue := 0, 0
	for _, l := range lists {
		total += l.lines
		if l.deadline == [2]int{} && l.lines > l.own() {
			t.Errorf("%s: list %s has no deadline — «Closes by 1.N» in its doc comment, at most two versions on", l.where, l.name)
			continue
		}
		due := map[[2]int]int{}
		for v, n := range l.byLine {
			due[v] += n
		}
		if rest := l.lines - l.own(); rest > 0 {
			due[l.deadline] += rest
		}
		for v, n := range due {
			if !versionReached(released, v) {
				continue
			}
			overdue += n
			msg := l.where + ": " + strconv.Itoa(n) + " line(s) of " + l.name + " were to be gone by " + versionText(v) + ", and " + versionText(released) + " is released"
			if strict {
				t.Error(msg)
			} else {
				t.Log(msg + " (fails the weekly run)")
			}
		}
		t.Logf("%-28s %3d line(s), closes by %s", l.name, l.lines, versionText(l.deadline))
	}
	t.Logf("lists of misses: %d, lines: %d, overdue: %d (released %s)", len(lists), total, overdue, versionText(released))
}

// own is the count of the lines with a deadline of their own.
func (l missList) own() int {
	n := 0
	for _, c := range l.byLine {
		n += c
	}
	return n
}

// missLists finds the lists of misses in the tests of internal/.
func missLists(t *testing.T, root string) []missList {
	t.Helper()
	var out []missList
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.VAR {
				continue
			}
			for _, sp := range g.Specs {
				vs := sp.(*ast.ValueSpec)
				doc := vs.Doc
				if doc == nil {
					doc = g.Doc
				}
				if doc == nil || !strings.Contains(doc.Text(), "only shrinks") || len(vs.Values) != 1 {
					continue
				}
				cl, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					continue
				}
				if _, ok := cl.Type.(*ast.MapType); !ok {
					continue
				}
				l := missList{where: filepath.ToSlash(rel), name: vs.Names[0].Name, byLine: map[[2]int]int{}}
				l.deadline = parseDeadline(doc.Text())
				for _, e := range cl.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					l.lines++
					text := string(src[fset.Position(kv.Value.Pos()).Offset:fset.Position(kv.Value.End()).Offset])
					if v := parseDeadline(text); v != [2]int{} {
						l.byLine[v]++
					}
				}
				out = append(out, l)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].where+out[i].name < out[j].where+out[j].name })
	return out
}

func parseDeadline(s string) [2]int {
	m := missDeadline.FindStringSubmatch(strings.Join(strings.Fields(s), " "))
	if m == nil {
		return [2]int{}
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return [2]int{a, b}
}

// latestReleased is the newest released version CHANGELOG.md has a section
// of.
func latestReleased(t *testing.T, root string) [2]int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := releasedVersion.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("CHANGELOG.md: no released section «## [x.y.z]»")
	}
	a, _ := strconv.Atoi(m[1])
	c, _ := strconv.Atoi(m[2])
	return [2]int{a, c}
}

// versionReached: released is v or later.
func versionReached(released, v [2]int) bool {
	return released[0] > v[0] || released[0] == v[0] && released[1] >= v[1]
}

func versionText(v [2]int) string {
	if v == [2]int{} {
		return "—"
	}
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1])
}

// The parts of the deadline check: a deadline in a doc or a line, a
// version the release has reached, and the newest released section.
func TestMisses_DeadlineParts(t *testing.T) {
	if got := parseDeadline("only shrinks.\nCloses by 1.8 (release plan 1.6)."); got != [2]int{1, 8} {
		t.Errorf("parseDeadline: %v", got)
	}
	if got := parseDeadline(`"release plan 1.6, «Линия CLI»: «разработка»"`); got != [2]int{} {
		t.Errorf("a work item is not a deadline: %v", got)
	}
	for _, c := range []struct {
		released, v [2]int
		want        bool
	}{{[2]int{1, 8}, [2]int{1, 8}, true}, {[2]int{1, 7}, [2]int{1, 8}, false}, {[2]int{2, 0}, [2]int{1, 9}, true}} {
		if versionReached(c.released, c.v) != c.want {
			t.Errorf("versionReached(%v, %v) != %v", c.released, c.v, c.want)
		}
	}
}
