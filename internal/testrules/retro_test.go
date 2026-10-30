// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"fmt"
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

// The retro of the roles (owner's decision 27.10, docs/internal/handoff/
// README.md, «Ретро ролей»): each role writes it in handoff/<version>-
// journal.md before the handoff, and each item of it ends in a result the
// next version can see. These checks hold what the template asks (R1–R6,
// release plan 1.4.0): a retro of at most retroMaxWords words a role (R1);
// an outcome from the closed list and a reference a reader can follow (R2);
// a stumble that comes back in the next retro without the owner's decision
// (R3); an example kept past its version (R4); the count in handoff/N.md
// as the journal gives it (R5); one template in the two files that show it
// (R6).

const retroMaxWords = 150

// retroOutcomes are the results an item may end in.
var retroOutcomes = []string{"правило", "проверка", "бэклог", "пример"}

// retroKnown are items written before the checks, each with what is to be
// done about it: "<version> «<role>»: <check>" → the work item. The list
// only shrinks: an item not here fails the test, and so does a line the
// journals no longer give.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var retroKnown = map[string]string{
	"1.3.1 «релиз»: R2 an item without a reference":                  "the first retro, written before R2; stays as it is",
	"1.3.1 «Процесс»: R2 an outcome not in the list":                 "the first retro, written before R2; stays as it is",
	"1.3.1 «разработка»: R2 an item without a reference":             "the first retro, written before R2; stays as it is",
	"1.3.1 «разработка»: R2 an outcome not in the list":              "the first retro, written before R2; stays as it is",
	"1.3.1 «разработка»: R2 a cost without a number":                 "the first retro, written before R2; stays as it is",
	"1.4.0 «разработка»: R2 a cost without a number":                 "written before R2: «десятки пробуждений» is no number; stays as it is",
	"1.4.0 «разработка»: R3 #396 again without the owner's decision": "two problems of one PR (a Qwen answer missed, the preview of stack_destroy), not a repeat",
}

var (
	retroHeading = regexp.MustCompile(`^### Ретро «([^»·]+?) · ([^»]+)» \(`)
	retroRef     = regexp.MustCompile(`#\d+|\brun \d+|\bTest[A-Z]\w*`)
	retroTest    = regexp.MustCompile(`\bTest[A-Z]\w*`)
	retroOutcome = regexp.MustCompile(`(?:Закрепить|Результат):\s*` + "`?" + `([\p{L}]+)`)
	retroExample = regexp.MustCompile(`пример, до (\d+\.\d+(?:\.\d+)?)`)
	retroCount   = regexp.MustCompile("Ретро \\(`handoff/[^`]+-journal\\.md`\\): пунктов (\\d+) — правил (\\d+)[^,]*, проверок (\\d+), бэклог (\\d+), примеров (\\d+), повторов (\\d+), текстом (\\d+)")
	retroNumber  = regexp.MustCompile(`\d`)
)

type retroItem struct {
	kind, text string
}

type retro struct {
	version, role string
	words         int
	items         []retroItem
}

// retros reads the section «Ретро» of each journal, in the order of its
// versions.
func retros(t *testing.T, dir string) map[string][]retro {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*-journal.md"))
	out := map[string][]retro{}
	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), "-journal.md")
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		_, sec, ok := strings.Cut(string(b), "\n## Ретро\n")
		if !ok {
			continue
		}
		sec, _, _ = strings.Cut(sec, "\n## ")
		var cur *retro
		for _, l := range strings.Split(sec, "\n") {
			if m := retroHeading.FindStringSubmatch(l); m != nil {
				out[version] = append(out[version], retro{version: version, role: strings.TrimSpace(m[1])})
				cur = &out[version][len(out[version])-1]
				continue
			}
			if cur == nil {
				continue
			}
			cur.words += len(strings.Fields(l))
			if item, ok := strings.CutPrefix(l, "- "); ok {
				kind, _, _ := strings.Cut(item, ":")
				cur.items = append(cur.items, retroItem{kind: kind, text: item})
			}
		}
	}
	return out
}

// outcome is the first word after «Закрепить:» or «Результат:», "" when
// the item gives none.
func (it retroItem) outcome() string {
	if m := retroOutcome.FindStringSubmatch(it.text); m != nil {
		return m[1]
	}
	return ""
}

func (it retroItem) repeat() bool { return strings.HasPrefix(it.kind, "Повтор") }

// none: «Повтор из прошлого ретро: нет» — no repeat, not an item.
func (it retroItem) none() bool {
	_, rest, _ := strings.Cut(it.text, ":")
	return it.repeat() && strings.HasPrefix(strings.TrimSpace(rest), "нет")
}

func TestRetro_Rules(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := filepath.Join(root, "docs", "internal", "handoff")
	all := retros(t, dir)
	tests := testNames(t, root)
	released := releasedVersions(t, root)
	found := map[string]bool{}
	flag := func(key, detail string) {
		found[key] = true
		if retroKnown[key] == "" {
			t.Errorf("%s: %s", key, detail)
		}
	}
	versions := make([]string, 0, len(all))
	for v := range all {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versionLess(versions[i], versions[j]) })

	for i, v := range versions {
		for _, r := range all[v] {
			who := fmt.Sprintf("%s «%s»", v, roleName(r.role))
			if r.words > retroMaxWords {
				flag(who+": R1 over the words", fmt.Sprintf("%d words, at most %d", r.words, retroMaxWords))
			}
			for _, it := range r.items {
				if it.repeat() {
					continue
				}
				switch o := it.outcome(); {
				case o == "":
					flag(who+": R2 an item without an outcome", it.text)
				case !contains(retroOutcomes, o):
					flag(who+": R2 an outcome not in the list", fmt.Sprintf("%q in %q, want one of %v", o, it.text, retroOutcomes))
				}
				if strings.HasPrefix(it.kind, "Дороже") {
					if !retroNumber.MatchString(it.text) {
						flag(who+": R2 a cost without a number", it.text)
					}
				} else if !retroRef.MatchString(it.text) {
					flag(who+": R2 an item without a reference", it.text)
				}
				for _, name := range retroTest.FindAllString(it.text, -1) {
					if !tests[name] {
						flag(who+": R2 a test that is not there", name)
					}
				}
				for _, m := range retroExample.FindAllStringSubmatch(it.text, -1) {
					if released[m[1]] {
						flag(who+": R4 «пример, до "+m[1]+"» after "+m[1]+" is out", it.text)
					}
				}
			}
			// R3: a stumble of the previous retro of the role whose
			// reference comes back without the owner's decision.
			if i == 0 {
				continue
			}
			for _, prev := range all[versions[i-1]] {
				if roleName(prev.role) != roleName(r.role) {
					continue
				}
				before := map[string]bool{}
				for _, it := range prev.items {
					if strings.HasPrefix(it.kind, "Споткнулись") {
						for _, ref := range retroRef.FindAllString(it.text, -1) {
							before[ref] = true
						}
					}
				}
				for _, it := range r.items {
					if !strings.HasPrefix(it.kind, "Споткнулись") || strings.Contains(it.text, "решени") {
						continue
					}
					for _, ref := range retroRef.FindAllString(it.text, -1) {
						if before[ref] {
							flag(who+": R3 "+ref+" again without the owner's decision", it.text)
						}
					}
				}
			}
		}
		// R5: the count of handoff/<version>.md.
		b, err := os.ReadFile(filepath.Join(dir, v+".md"))
		if err != nil {
			continue
		}
		m := retroCount.FindStringSubmatch(string(b))
		if m == nil {
			flag(v+": R5 no count in handoff/"+v+".md", "the line «Ретро (`handoff/"+v+"-journal.md`): пунктов N — …»")
			continue
		}
		want := retroTally(all[v])
		var got [7]int
		for k := range got {
			got[k], _ = strconv.Atoi(m[k+1])
		}
		if got != want {
			flag(v+": R5 the count in handoff/"+v+".md is not the journal's",
				fmt.Sprintf("пунктов, правил, проверок, бэклог, примеров, повторов, текстом: %v, the journal gives %v", got, want))
		}
	}
	for k := range retroKnown {
		if !found[k] {
			t.Errorf("retroKnown names %q, which the journals no longer give: remove the line", k)
		}
	}
}

// retroTally counts the items of a version: all, then by outcome —
// правило, проверка, бэклог, пример, a repeat, none of these.
func retroTally(rs []retro) (n [7]int) {
	for _, r := range rs {
		for _, it := range r.items {
			if it.none() {
				continue
			}
			n[0]++
			switch {
			case it.repeat():
				n[5]++
			case contains(retroOutcomes, it.outcome()):
				n[1+indexOf(retroOutcomes, it.outcome())]++
			default:
				n[6]++
			}
		}
	}
	return n
}

// R6: the template of the retro is the same in the two files that show it.
func TestRetro_OneTemplate(t *testing.T) {
	root := filepath.Join("..", "..")
	block := func(path string) string {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		_, rest, ok := strings.Cut(string(b), "### Ретро «<роль> · <версия>» (<дата>)")
		if !ok {
			t.Fatalf("%s: no template of the retro", path)
		}
		body, _, _ := strings.Cut(rest, "```")
		return strings.TrimSpace(body)
	}
	readme, handoff := block("docs/internal/handoff/README.md"), block("docs/internal/audits/process-handoff.md")
	if readme != handoff {
		t.Errorf("the template of the retro differs:\nhandoff/README.md:\n%s\nprocess-handoff.md:\n%s", readme, handoff)
	}
}

// roleName is the role of a heading: «Процесс · 1.3.1 и 1.4» is Процесс.
func roleName(r string) string {
	r, _, _ = strings.Cut(r, " ")
	return r
}

// testNames are the test functions of the repository, by the AST.
func testNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") {
				out[fd.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// releasedVersions are the versions CHANGELOG.md has a release section
// for, each also as its minor ("1.4" for 1.4.0).
func releasedVersions(t *testing.T, root string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^## \[(\d+)\.(\d+)\.(\d+)\]`).FindAllStringSubmatch(string(b), -1) {
		out[m[1]+"."+m[2]+"."+m[3]] = true
		if m[3] == "0" {
			out[m[1]+"."+m[2]] = true
		}
	}
	return out
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

func contains(list []string, s string) bool { return indexOf(list, s) >= 0 }

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
