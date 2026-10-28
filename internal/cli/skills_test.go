// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/tools"
	"gopkg.in/yaml.v3"
)

// The scenario skills of skills/ (docs/internal/skills/spec.md, §8): what an
// agent reads as the way to work with evo. The test lives here because it
// needs the registry, the command tree and the rules evo generates.

// skillNames is the closed list of the skills 1.3.0 ships (spec §3.1): a
// missing one or an extra one fails, so an empty skills/ does not pass.
var skillNames = []string{"evo-cleanup", "evo-deploy", "evo-postgres", "evo-scenarios", "evo-vm"}

// skillRouter is the skill that picks the others: its body has its own limit.
const skillRouter = "evo-scenarios"

// skillVersionWant is metadata.evo-version of every skill: the major.minor
// of evo they are written for. The release changes it with the minor
// (docs/RELEASING.md).
const skillVersionWant = "1.4"

// skillOneWordTools are tool names without "_" the text may name in
// backticks (spec §8.2): they are checked against the registry like the
// rest.
var skillOneWordTools = map[string]bool{"deploy": true, "destroy": true, "status": true, "logs": true,
	"list": true, "provision": true, "doctor": true, "whoami": true}

// skillUpperWords are capitalised words in backticks that are no code of
// evo (spec §8.3).
var skillUpperWords = map[string]bool{"MCP": true, "CLI": true, "JSON": true, "SSH": true, "UUID": true,
	"CIDR": true, "HTTP": true, "SSE": true}

const skillStopMarker = "Stop and ask:"

type scenarioSkill struct {
	name  string
	front struct {
		Name        string            `yaml:"name"`
		Description string            `yaml:"description"`
		License     string            `yaml:"license"`
		Metadata    map[string]string `yaml:"metadata"`
	}
	frontRaw string
	body     []string // lines after the front matter
}

func readSkills(t *testing.T) []scenarioSkill {
	t.Helper()
	root := filepath.Join("..", "..", "skills")
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var got []string
	var out []scenarioSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		got = append(got, e.Name())
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("skills/%s: no SKILL.md: %v", e.Name(), err)
			continue
		}
		s := scenarioSkill{name: e.Name()}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		rest, ok := strings.CutPrefix(text, "---\n")
		front, body, ok2 := strings.Cut(rest, "\n---\n")
		if !ok || !ok2 {
			t.Errorf("skills/%s/SKILL.md: no front matter between --- lines", e.Name())
			continue
		}
		s.frontRaw = front
		if err := yaml.Unmarshal([]byte(front), &s.front); err != nil {
			t.Errorf("skills/%s/SKILL.md: front matter: %v", e.Name(), err)
			continue
		}
		s.body = strings.Split(strings.TrimRight(body, "\n"), "\n")
		out = append(out, s)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(skillNames, ",") {
		t.Errorf("skills/ holds %v, 1.3.0 ships %v (spec §3.1)", got, skillNames)
	}
	return out
}

// spec §8.1, §8.7, §13.12: front matter, lengths, version, license.
func TestSkills_Structure(t *testing.T) {
	nameRE := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, s := range readSkills(t) {
		f := s.front
		if f.Name != s.name || !nameRE.MatchString(f.Name) || len(f.Name) > 64 {
			t.Errorf("skills/%s: name %q — the name of the directory, [a-z0-9-], up to 64", s.name, f.Name)
		}
		if d := f.Description; d == "" || len(d) > 1024 || strings.Contains(d, "\n") {
			t.Errorf("skills/%s: description — one line of 1 to 1024 characters, has %d", s.name, len(d))
		}
		if f.License != "Apache-2.0" {
			t.Errorf("skills/%s: license %q, LICENSE is Apache-2.0", s.name, f.License)
		}
		if v := f.Metadata["evo-version"]; v != skillVersionWant {
			t.Errorf("skills/%s: metadata.evo-version %q, want %q", s.name, v, skillVersionWant)
		}
		limit := 200
		if s.name == skillRouter {
			limit = 100
		}
		if len(s.body) > limit {
			t.Errorf("skills/%s: %d lines of body, up to %d", s.name, len(s.body), limit)
		}
	}
}

// A skill whose description claims what the built-in skill evo claims ("Use
// it for any task on Cloud.ru resources") is loaded next to it for every
// task, and the agent reads two sources of one rule (spec §3.1, variant
// (a)). The phrase is taken from skillFrontMatter, so a new wording of the
// built-in moves the check with it.
func TestSkills_DescriptionNotTheBuiltIn(t *testing.T) {
	var builtIn struct{ Description string }
	fm := strings.TrimPrefix(skillFrontMatter, "---\n")
	fm, _, _ = strings.Cut(fm, "\n---")
	if err := yaml.Unmarshal([]byte(fm), &builtIn); err != nil {
		t.Fatalf("front matter of the built-in skill: %v", err)
	}
	_, claim, ok := strings.Cut(builtIn.Description, "Use it for ")
	claim = strings.TrimSuffix(claim, ".")
	if !ok || claim == "" {
		t.Fatalf("no \"Use it for …\" in the description of the built-in skill: %q", builtIn.Description)
	}
	for _, s := range readSkills(t) {
		if strings.Contains(strings.ToLower(s.front.Description), strings.ToLower(claim)) {
			t.Errorf("skills/%s: description claims %q, as the built-in skill evo does: name the scenario instead", s.name, claim)
		}
	}
}

// skillSpan is a text in backticks, or a line of a fenced block, and its
// line.
type skillSpan struct {
	line int
	text string
}

// skillSpans are the texts in backticks, a span wrapped over lines read as
// one line, and, inside a fenced block, each line itself.
func skillSpans(lines []string) []skillSpan {
	var out []skillSpan
	prose := make([]string, len(lines))
	fence := false
	for i, l := range lines {
		switch {
		case strings.HasPrefix(strings.TrimSpace(l), "```"):
			fence = !fence
		case fence:
			out = append(out, skillSpan{i + 1, strings.TrimSpace(l)})
		default:
			prose[i] = l
		}
	}
	text := strings.Join(prose, "\n")
	for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatchIndex(text, -1) {
		line := strings.Count(text[:m[0]], "\n") + 1
		out = append(out, skillSpan{line, strings.Join(strings.Fields(text[m[2]:m[3]]), " ")})
	}
	return out
}

// skillProductWords are the names an answer of evo can carry: the string
// literals and JSON keys of the code of evo, and the words with "_" of the
// tool descriptions (a field passed through from the API, like
// connection_string of pg_get). A skill names a field or a state of the
// answer by one of them; a typo or an invented field is none.
func skillProductWords(t *testing.T, reg *tools.Registry) map[string]bool {
	t.Helper()
	words := map[string]bool{}
	lit := regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)[",]`)
	under := regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	for _, dir := range []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range lit.FindAllStringSubmatch(string(b), -1) {
				words[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tl := range reg.Tools() {
		for _, w := range under.FindAllString(tl.Description, -1) {
			words[w] = true
		}
	}
	return words
}

// skillCommand checks a CLI command line: the words up to the first flag or
// argument name a command of the tree, every --flag is one of its flags.
func skillCommand(root *cobra.Command, line string) string {
	words := strings.Fields(line)
	if len(words) < 2 || words[0] != "evo" {
		return ""
	}
	cmd := root
	i := 1
	for ; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "-") {
			break
		}
		var next *cobra.Command
		for _, c := range cmd.Commands() {
			if c.Name() == w || c.HasAlias(w) {
				next = c
				break
			}
		}
		if next == nil {
			break
		}
		cmd = next
	}
	if cmd == root {
		return "no command " + strings.Join(words[:2], " ")
	}
	// A bare command path names the command, as in a table of scenarios.
	if i == len(words) {
		return ""
	}
	// The words before the first flag are the command's arguments, checked
	// by the command itself: a group rejects a word that is none of its
	// commands (evo vm explode).
	var args []string
	for _, w := range words[i:] {
		if strings.HasPrefix(w, "-") {
			break
		}
		args = append(args, w)
	}
	if err := cmd.ValidateArgs(args); err != nil {
		return cmd.CommandPath() + " " + strings.Join(args, " ") + ": " + err.Error()
	}
	for _, w := range words[i:] {
		if !strings.HasPrefix(w, "--") || w == "--" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(w, "--"), "=")
		if cmd.Flag(name) == nil {
			return "no flag --" + name + " of " + cmd.CommandPath()
		}
	}
	return ""
}

// spec §8.2, §8.3: every tool, argument, command, flag and code a skill
// names is one of evo.
func TestSkills_NamesExist(t *testing.T) {
	reg := newRegistry()
	known := map[string]bool{}
	for _, tl := range reg.Tools() {
		known[tl.Name] = true
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k := range props {
			known[k] = true
		}
	}
	product := skillProductWords(t, reg)
	for w := range product {
		known[w] = true
	}
	codes := map[string]bool{}
	b, err := os.ReadFile(filepath.Join("..", "errors", "codes.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`= "([A-Z][A-Z_]+)"`).FindAllStringSubmatch(string(b), -1) {
		codes[m[1]] = true
	}
	ident := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	upper := regexp.MustCompile(`^[A-Z][A-Z_]+$`)
	root := newRootCmd()
	for _, s := range readSkills(t) {
		for _, sp := range skillSpans(s.body) {
			line, text := sp.line, sp.text
			switch {
			case ident.MatchString(text) && (strings.Contains(text, "_") || skillOneWordTools[text]):
				if !known[text] {
					t.Errorf("skills/%s:%d: `%s` is no tool, argument or answer field of evo", s.name, line, text)
				}
			case upper.MatchString(text) && !skillUpperWords[text]:
				if !codes[text] && !product[text] {
					t.Errorf("skills/%s:%d: `%s` is no reason (internal/errors) or state evo knows", s.name, line, text)
				}
			case strings.HasPrefix(text, "evo "):
				if msg := skillCommand(root, text); msg != "" {
					t.Errorf("skills/%s:%d: `%s`: %s", s.name, line, text, msg)
				}
			}
		}
	}
}

// spec §8.4: a scenario passes confirmed=true or --yes only after it
// stopped to show the preview to the person.
func TestSkills_ConsentAfterThePreview(t *testing.T) {
	consent := regexp.MustCompile(`confirmed\s*[=:]\s*true|--yes\b`)
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)confirmed\s*[=:]\s*true.*\b(always|immediately|without asking)\b|\b(always|immediately|without asking)\b.*confirmed\s*[=:]\s*true`),
		regexp.MustCompile(`(?i)--yes\b.*\balways\b|\balways\b.*--yes\b`),
	}
	for _, s := range readSkills(t) {
		if s.name == skillRouter {
			continue
		}
		stop := -1
		for i, l := range s.body {
			if stop < 0 && strings.Contains(l, skillStopMarker) {
				stop = i
			}
			if consent.MatchString(l) && (stop < 0 || i < stop) {
				t.Errorf("skills/%s:%d: consent before %q: %s", s.name, i+1, skillStopMarker, strings.TrimSpace(l))
			}
			for _, re := range forbidden {
				if re.MatchString(l) {
					t.Errorf("skills/%s:%d: consent without the person: %s", s.name, i+1, strings.TrimSpace(l))
				}
			}
		}
		if stop < 0 {
			t.Errorf("skills/%s: no %q", s.name, skillStopMarker)
		}
	}
}

// skillBatchYes is the skill where one yes may cover a table of deletes
// (owner's decision 24.10, spec §13.5), and skillBatchYesTerms are the
// terms that bound it: the rows of the table only, while the preview of
// each matches its row; anything else stops for a new question.
const skillBatchYes = "evo-cleanup"

var skillBatchYesTerms = []*regexp.Regexp{
	regexp.MustCompile(`(?i)only while the preview of each matches its row`),
	regexp.MustCompile(`(?i)a resource not in the table`),
	regexp.MustCompile(`(?i)an error: stop and ask again`),
}

// spec §8.4, §13.5: one yes for many changes only in evo-cleanup, and there
// only with the terms that bound it.
func TestSkills_OneYesForATable(t *testing.T) {
	oneYes := regexp.MustCompile(`(?i)\bone yes\b`)
	seen := false
	for _, s := range readSkills(t) {
		text := strings.Join(strings.Fields(strings.Join(s.body, " ")), " ")
		if !oneYes.MatchString(text) {
			continue
		}
		if s.name != skillBatchYes {
			t.Errorf("skills/%s: one yes for many changes is %s's only (spec §13.5): ask before each", s.name, skillBatchYes)
			continue
		}
		seen = true
		for _, re := range skillBatchYesTerms {
			if !re.MatchString(text) {
				t.Errorf("skills/%s: one yes for the table without the term %q (spec §13.5)", s.name, re)
			}
		}
	}
	if !seen {
		t.Errorf("skills/%s: no \"one yes\" for the table of deletes, the scenario of spec §6.4", skillBatchYes)
	}
}

// spec §8.5: a scenario does not repeat the rules evo generates
// (docs/agent-skill.md): it would drift from them.
func TestSkills_NoBuiltInRuleRepeated(t *testing.T) {
	var starts []string
	inRules := false
	sc := bufio.NewScanner(strings.NewReader(agentSkillBody(newRootCmd(), false)))
	ruleRE := regexp.MustCompile(`^\d+\.\s+(.*)`)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "## Rules"):
			inRules = true
		case strings.HasPrefix(l, "## "):
			inRules = false
		case inRules:
			if m := ruleRE.FindStringSubmatch(l); m != nil {
				if w := strings.Fields(m[1]); len(w) >= 5 {
					starts = append(starts, strings.ToLower(strings.Join(w[:5], " ")))
				}
			}
		}
	}
	if len(starts) < 9 {
		t.Fatalf("read %d rules of evo, want 9: has agentSkillBody changed its form?", len(starts))
	}
	lead := regexp.MustCompile(`^(\s*([-*]|\d+\.)\s+)?`)
	for _, s := range readSkills(t) {
		for i, l := range s.body {
			text := strings.ToLower(strings.Join(strings.Fields(lead.ReplaceAllString(l, "")), " "))
			for _, st := range starts {
				if strings.HasPrefix(text, st) {
					t.Errorf("skills/%s:%d: repeats a rule of evo (%q…): link docs/agent-skill.md instead", s.name, i+1, st)
				}
			}
		}
	}
}

// spec §8.6, §3.2: no install of packages, no real id or address, no secret.
func TestSkills_NothingForbidden(t *testing.T) {
	install := regexp.MustCompile(`\b(npm install|pip install|brew install|apt install|apt-get)\b|curl[^|]*\|\s*(ba)?sh\b`)
	uuid := regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	ipv4 := regexp.MustCompile(`\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)
	secret := regexp.MustCompile(`CLOUDRU_SECRET=[^\s<$]`)
	// Documentation ranges (RFC 5737) and 0.0.0.0 (/0, any source).
	doc := []string{"203.0.113.", "198.51.100.", "192.0.2.", "0.0.0.0"}
	for _, s := range readSkills(t) {
		lines := append(strings.Split(s.frontRaw, "\n"), s.body...)
		front := len(lines) - len(s.body)
		for i, l := range lines {
			where := s.name + ":front matter"
			if i >= front {
				where = s.name + ":" + strconv.Itoa(i-front+1)
			}
			if install.MatchString(l) {
				t.Errorf("skills/%s: installs a package: %s", where, strings.TrimSpace(l))
			}
			if uuid.MatchString(l) {
				t.Errorf("skills/%s: an id: %s", where, strings.TrimSpace(l))
			}
			for _, ip := range ipv4.FindAllString(l, -1) {
				ok := false
				for _, d := range doc {
					ok = ok || strings.HasPrefix(ip, d) || ip == d
				}
				if !ok {
					t.Errorf("skills/%s: address %s outside the documentation ranges", where, ip)
				}
			}
			if secret.MatchString(l) {
				t.Errorf("skills/%s: a value of CLOUDRU_SECRET", where)
			}
		}
	}
}

// What the live run of Grok on 1.3.0 found the skills to say wrong, against
// the code (G130-01, -09, -10, -16, -25, -27, -29): the wrong claim may not
// come back, and the right one stays.
func TestSkills_SayWhatTheCodeDoes(t *testing.T) {
	bodies := map[string]string{}
	for _, s := range readSkills(t) {
		bodies[s.name] = strings.Join(strings.Fields(strings.Join(s.body, " ")), " ")
	}
	tests := testFuncs(t)
	gaps := 0
	for _, c := range []struct {
		skill, finding string
		must, mustNot  []string
		// proof are the tests that the code does what must says (С9,
		// audit blind-spots-1.5 FB15-48: a skill promises nothing the code
		// does not do); noProof says why there is none — a line to the
		// agent, not a claim of the code, or «пробел:» with the test that
		// is missing (the list of gaps only shrinks).
		proof   []string
		noProof string
	}{
		// The inventory is the prefix; the rest of the project is one line,
		// never a row of the table to delete; the lists need every group.
		{"evo-cleanup", "G130-01",
			[]string{"`--toolsets all`", "never a row of the table"},
			[]string{"or every group of the resources concerned"},
			nil, "указание агенту, а не утверждение о коде"},
		// The VM of a deployment is in the inventory by the prefix too:
		// deploy names it evo-<project_name> (M2 HTTP T4), and a deployment
		// of another project in a working directory is never a row (review
		// of the release on #445: production VMs in the table).
		{"evo-cleanup", "M2 T4",
			[]string{"`vm_name` starts with `evo-<prefix>`"},
			[]string{"the VMs of the deployments `status` and `list` found"},
			[]string{"TestDeploy_VMNameFromDirectoryIsAHostname"}, ""},
		// deploy has no default image (engine.BootImage: CONFIG_INVALID);
		// the VM is named evo-<project_name> (vmNameFor); the compose file's
		// ports open from 0.0.0.0/0 in the zone's shared group.
		{"evo-deploy", "G130-09, -10, -29",
			[]string{"has no default image", "`evo-<project_name>`", "`0.0.0.0/0`"},
			[]string{"`--image` if needed)"},
			// No image: the world «config without CLOUDRU_IMAGE_ID» refuses
			// before the question, no image is picked for the person.
			[]string{"TestDeploy_VMNameFromDirectoryIsAHostname", "TestConsent_RefusalWithoutWriteComesBeforeTheQuestion",
				"TestOpen_PortOpensForAnyAddressInTheGivenGroup"}, ""},
		// pg_create's default subnet is the zone's (pickDBaaSSubnet), and
		// evo gives no price for PostgreSQL.
		{"evo-postgres", "G130-25, -27",
			[]string{"the main subnet of the profile's zone", "`tariff_url`"},
			[]string{"the first subnet of the project", "low-cost", "the cheapest"},
			[]string{"TestDbaasSubnet_MainSubnetOfTheProfilesZone", "TestPgCreate_PreviewGivesTheTariffNotAPrice"}, ""},
		// With --allow-project a resource that is gone answers NOT_FOUND
		// (H9 b), not already_deleted.
		{"evo-scenarios", "G130-16",
			[]string{"a server without `--allow-project`"},
			nil,
			[]string{"TestAlreadyDeleted_BoundServerKeepsNotFound"}, ""},
	} {
		b := bodies[c.skill]
		for _, w := range c.must {
			if !strings.Contains(b, w) {
				t.Errorf("skills/%s (%s): must say %q", c.skill, c.finding, w)
			}
		}
		for _, w := range c.mustNot {
			if strings.Contains(b, w) {
				t.Errorf("skills/%s (%s): says %q, which the code does not do", c.skill, c.finding, w)
			}
		}
		for _, p := range c.proof {
			if !tests[p] {
				t.Errorf("skills/%s (%s): proof %s is no test of the repository", c.skill, c.finding, p)
			}
		}
		switch {
		case len(c.proof) == 0 && c.noProof == "":
			t.Errorf("skills/%s (%s): no proof that the code does it, and no why", c.skill, c.finding)
		case strings.HasPrefix(c.noProof, "пробел:"):
			gaps++
		}
	}
	// The gaps only shrink: a test that proves a line takes it off.
	if gaps > skillProofGaps {
		t.Errorf("skills: %d promises without a proof, %d before: a new promise comes with its test", gaps, skillProofGaps)
	} else if gaps < skillProofGaps {
		t.Errorf("skills: %d promises without a proof, fewer than %d: lower skillProofGaps", gaps, skillProofGaps)
	}
}

// skillProofGaps is the number of promises of the skills with no test that
// the code keeps them (С9); only shrinks.
const skillProofGaps = 0

// testFuncs are the names of the test functions of the repository.
func testFuncs(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	fn := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range fn.FindAllSubmatch(b, -1) {
			names[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}
