// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// User docs checked against the binary. docs/commands.md and docs/tools.md
// are generated and already covered by TestCommandsDoc and TestToolsDoc.
var userDocs = []string{
	"README.md",
	"docs/guide.md",
	"docs/install.md",
	"docs/auth.md",
	"docs/mcp.md",
	"docs/troubleshooting.md",
	"docs/exit-codes.md",
	"docs/internal/dogfood-protocol.md",
}

// docExampleSkip are invocations the checker does not treat as a runnable
// command line. Each entry is a closed exception: the substring must occur
// in the invocation, and Reason says why it is not a discrepancy.
var docExampleSkip = []struct {
	File, Contains, Reason string
}{
	{"README.md", "evo <команда> --help", "шаблон справки: на месте команды стоит плейсхолдер, не имя"},
	{"docs/guide.md", "evo <команда> --help", "шаблон справки: на месте команды стоит плейсхолдер, не имя"},
	{"docs/troubleshooting.md", "evo vm create my-vm ru.AZ-2", "пример ошибки: зона без флага — лишний аргумент, текст рядом описывает отказ"},
}

// TestDocsExamples_CommandLines checks every `evo …` in fenced shell blocks
// and inline code of the user docs: the command path exists (aliases and
// hidden commands included), every flag exists, and the number of
// positional arguments is what the command accepts. Placeholders (<id>,
// <имя-вм>) count as one value. A token that is only `…` or `...` means
// the line is a pattern, not a fixed arity.
func TestDocsExamples_CommandLines(t *testing.T) {
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	var n int
	for _, rel := range userDocs {
		text := string(mustReadRepo(t, rel))
		for _, inv := range docInvocations(rel, text) {
			if reason := skipInvocation(inv); reason != "" {
				continue
			}
			n++
			for _, argv := range expandDocArgv(inv.Argv) {
				if err := checkDocArgv(root, argv, inv); err != nil {
					t.Errorf("%s:%d: %s\n  %s", inv.File, inv.Line, err, inv.Text)
				}
			}
		}
	}
	if n < 50 {
		t.Fatalf("only %d invocations parsed: the extractor missed the docs", n)
	}
}

type docInvocation struct {
	File, Text string
	Line       int
	Argv       []string
	// Fence is a shell block: a line there is an example, so a missing
	// required argument is a discrepancy. Inline code is often the name
	// of a command or a flag, and a catalogue line (a/b, « / », «→») lists
	// commands rather than one invocation.
	Fence, Catalogue bool
}

func skipInvocation(inv docInvocation) string {
	for _, s := range docExampleSkip {
		if (s.File == "" || s.File == inv.File) && strings.Contains(inv.Text, s.Contains) {
			return s.Reason
		}
	}
	return ""
}

func mustReadRepo(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// docInvocations returns shell invocations of evo from fenced blocks and
// from inline code. JSON and YAML fences are not shell.
func docInvocations(file, src string) []docInvocation {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []docInvocation
	line := 1
	for {
		i := strings.Index(src, "```")
		if i < 0 {
			out = append(out, inlineInvocations(file, src, line)...)
			break
		}
		out = append(out, inlineInvocations(file, src[:i], line)...)
		line += strings.Count(src[:i], "\n")
		src = src[i+3:]
		nl := strings.IndexByte(src, '\n')
		if nl < 0 {
			break
		}
		lang := strings.TrimSpace(src[:nl])
		line++
		src = src[nl+1:]
		end := strings.Index(src, "```")
		if end < 0 {
			break
		}
		body := src[:end]
		if shellFence(lang) {
			out = append(out, fenceInvocations(file, body, line)...)
		}
		line += strings.Count(body, "\n")
		src = src[end+3:]
	}
	return out
}

func shellFence(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "", "sh", "bash", "shell", "console", "text", "zsh", "fish", "powershell":
		return true
	default:
		return false
	}
}

func fenceInvocations(file, body string, start int) []docInvocation {
	var out []docInvocation
	for _, ln := range logicalLines(body, start) {
		out = append(out, invocationsFromLine(file, ln.text, ln.line, true)...)
	}
	return out
}

type logicalLine struct {
	text string
	line int
}

// logicalLines joins a trailing backslash with the next physical line.
// The reported line is the first line of the continuation.
func logicalLines(body string, start int) []logicalLine {
	raw := strings.Split(body, "\n")
	var out []logicalLine
	var b strings.Builder
	begin := start
	for i, line := range raw {
		trim := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trim, `\`) && !strings.HasSuffix(trim, `\\`) {
			if b.Len() == 0 {
				begin = start + i
			}
			b.WriteString(strings.TrimSpace(strings.TrimSuffix(trim, `\`)))
			b.WriteByte(' ')
			continue
		}
		if b.Len() == 0 {
			begin = start + i
		}
		b.WriteString(line)
		out = append(out, logicalLine{text: b.String(), line: begin})
		b.Reset()
	}
	if b.Len() > 0 {
		out = append(out, logicalLine{text: b.String(), line: begin})
	}
	return out
}

var inlineCode = regexp.MustCompile("(?s)`([^`]+)`")

func inlineInvocations(file, src string, start int) []docInvocation {
	var out []docInvocation
	for _, m := range inlineCode.FindAllStringSubmatchIndex(src, -1) {
		body := src[m[2]:m[3]]
		if !evoWord(body) {
			continue
		}
		line := start + strings.Count(src[:m[2]], "\n")
		flat := strings.Join(strings.Fields(body), " ")
		out = append(out, invocationsFromLine(file, flat, line, false)...)
	}
	return out
}

func evoWord(s string) bool {
	for _, f := range strings.Fields(s) {
		if f == "evo" || f == "evo.exe" || strings.HasSuffix(f, "/evo") {
			return true
		}
	}
	return false
}

// evoArgv tokenizes one shell line and returns the arguments of its evo
// invocation, without the word evo. nil means the line does not run evo.
func evoArgv(line string) []string {
	line = stripShellComment(line)
	if strings.TrimSpace(line) == "" {
		return nil
	}
	for _, seg := range splitShell(line) {
		toks := shellTokens(seg)
		toks = stripRedirect(toks)
		// NAME=value prefixes, then evo. "claude mcp add … -- evo mcp"
		// keeps the evo after the last "--".
		i := indexEvo(toks)
		if i < 0 {
			continue
		}
		argv := toks[i+1:]
		if len(argv) == 0 {
			continue // the file name, not an invocation
		}
		return argv
	}
	return nil
}

func isEvoBin(t string) bool {
	return t == "evo" || t == "evo.exe" || strings.HasSuffix(t, "/evo") || strings.HasSuffix(t, "/evo.exe")
}

func isEnvAssign(t string) bool {
	i := strings.IndexByte(t, '=')
	if i <= 0 || strings.HasPrefix(t, "-") {
		return false
	}
	for _, r := range t[:i] {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func indexEvo(toks []string) int {
	// Prefer the evo that follows a bare "--" (the command of `claude mcp add`).
	for i := len(toks) - 1; i >= 1; i-- {
		if toks[i-1] == "--" && isEvoBin(toks[i]) {
			return i
		}
	}
	// Otherwise evo is the command only when it is the command word.
	// `go build -o bin/evo`, `docker run --name evo` and `rm ~/.local/bin/evo`
	// name the file, they do not run it.
	i := 0
	for i < len(toks) && (isEnvAssign(toks[i]) || toks[i] == "$" || toks[i] == ">" || toks[i] == "sudo" || toks[i] == "command" || toks[i] == "exec" || toks[i] == "time" || toks[i] == "nohup") {
		i++
	}
	if i < len(toks) && isEvoBin(toks[i]) {
		return i
	}
	return -1
}

// foreignCmd are programs a doc pipes evo into or out of. A later segment
// whose first word is not one of these, after an evo command, is another
// evo command (`evo vm list | disk list | k8s list`).
var foreignCmd = map[string]bool{
	"pass": true, "jq": true, "echo": true, "printf": true, "openssl": true,
	"curl": true, "docker": true, "claude": true, "go": true, "rm": true,
	"install": true, "export": true, "kill": true, "sudo": true, "cat": true,
	"head": true, "tail": true, "grep": true, "awk": true, "sed": true,
	"xargs": true, "tee": true, "ssh": true, "scp": true, "kubectl": true,
	"helm": true, "git": true, "tar": true, "gzip": true, "gunzip": true,
	"base64": true, "env": true, "cd": true, "mkdir": true, "cp": true,
	"mv": true, "ln": true, "chmod": true, "chown": true, "source": true,
	"wait": true, "sleep": true, "which": true, "command": true, "exec": true,
	"time": true, "nohup": true, "less": true, "more": true, "wc": true,
	"sort": true, "uniq": true, "cut": true, "tr": true, "find": true,
	"python": true, "python3": true, "node": true, "npm": true, "npx": true,
	"brew": true, "apt": true, "true": true, "false": true, "test": true,
	"read": true, "set": true, "watch": true, "bash": true, "sh": true,
	"zsh": true, "fish": true, "pwsh": true, "powershell": true, "cmd": true,
	"unzip": true, "sha256sum": true, "shasum": true, "open": true,
}

func lineStartsWithEvo(seg string) bool {
	return indexEvo(shellTokens(stripShellComment(seg))) >= 0
}

func foreignSegment(seg string) bool {
	toks := shellTokens(stripShellComment(seg))
	i := 0
	for i < len(toks) && isEnvAssign(toks[i]) {
		i++
	}
	return i < len(toks) && foreignCmd[toks[i]]
}

func catalogueLine(s string) bool {
	if strings.Contains(s, "→") || strings.Contains(s, " / ") {
		return true
	}
	for _, t := range shellTokens(s) {
		if altToken.MatchString(t) {
			return true
		}
	}
	return false
}

var docBracket = regexp.MustCompile(`\[[^\[\]]*\]`)

// expandDocBrackets turns a documentation pattern into tokens the checker
// can read. `[--dry-run|--yes]` contributes both flags, `[--kind a|b|c]`
// contributes the flag and one sample value, `[часть имени]` is one
// optional positional.
func expandDocBrackets(s string) string {
	return docBracket.ReplaceAllStringFunc(s, func(g string) string {
		inner := strings.TrimSpace(g[1 : len(g)-1])
		if inner == "" || !strings.HasPrefix(inner, "-") {
			return " <opt>"
		}
		var b strings.Builder
		for _, f := range strings.Fields(inner) {
			if strings.Contains(f, "|") {
				parts := strings.Split(f, "|")
				if strings.HasPrefix(parts[0], "-") {
					for _, p := range parts {
						b.WriteByte(' ')
						b.WriteString(p)
					}
					continue
				}
				f = parts[0]
			}
			b.WriteByte(' ')
			b.WriteString(f)
		}
		return b.String()
	})
}

// splitDocCatalogue splits a cheat-sheet line on «→» and « / ». Each piece
// is its own evo command (`evo configure / whoami / doctor`).
func splitDocCatalogue(line string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	flush := func() {
		if t := strings.TrimSpace(b.String()); t != "" {
			out = append(out, t)
		}
		b.Reset()
	}
	for i := 0; i < len(line); {
		c := line[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		if !inSingle && !inDouble {
			if strings.HasPrefix(line[i:], "→") {
				flush()
				i += len("→")
				continue
			}
			if strings.HasPrefix(line[i:], " / ") {
				flush()
				i += 3
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	flush()
	if len(out) <= 1 {
		return out
	}
	for i, p := range out {
		if !lineStartsWithEvo(p) && !foreignSegment(p) {
			out[i] = "evo " + strings.TrimSpace(p)
		}
	}
	return out
}

func invocationsFromLine(file, line string, n int, fence bool) []docInvocation {
	line = strings.ReplaceAll(line, `\|`, " | ")
	code, comment := splitShellCommentParts(line)
	var out []docInvocation
	// A cheat-sheet line starts with evo. « / » and «→» then separate
	// commands, and a pipe may continue with another evo command.
	if lineStartsWithEvo(code) {
		code = expandDocBrackets(code)
		cat := catalogueLine(code)
		for _, piece := range splitDocCatalogue(code) {
			segs := splitShell(piece)
			sawEvo := false
			for _, seg := range segs {
				seg = strings.TrimSpace(seg)
				if seg == "" {
					continue
				}
				if !lineStartsWithEvo(seg) && sawEvo && !foreignSegment(seg) {
					seg = "evo " + seg
				}
				argv := evoArgv(seg)
				if argv == nil {
					continue
				}
				sawEvo = true
				out = append(out, docInvocation{
					File: file, Text: strings.TrimSpace(seg), Line: n, Argv: cutAtDash(argv),
					Fence: fence, Catalogue: cat,
				})
			}
		}
	} else if strings.TrimSpace(code) != "" {
		// Sample output and prose name a command mid-sentence
		// ('evo configure' или …). That mention is not a shell line.
		out = append(out, harvestEvoMentions(file, code, n)...)
	}
	out = append(out, harvestEvoMentions(file, comment, n)...)
	return out
}

// cutAtDash drops the gloss after an em dash (`evo vpc --help — подсети`).
func cutAtDash(argv []string) []string {
	for i, a := range argv {
		if a == "—" || a == "–" {
			return argv[:i]
		}
	}
	return argv
}

// harvestEvoMentions reads `evo …` out of a comment or a sentence. The
// command ends at an em dash or at the first prose word.
func harvestEvoMentions(file, line string, n int) []docInvocation {
	if !evoWord(line) {
		return nil
	}
	toks := shellTokens(line)
	var out []docInvocation
	for i := 0; i < len(toks); i++ {
		if toks[i] != "evo" && toks[i] != "evo.exe" {
			continue
		}
		// `install evo ~/.local/bin` and `docker run --name evo` name the
		// file. A mention is a command only at the start of a clause.
		prev := ""
		if i > 0 {
			prev = toks[i-1]
		}
		if !evoMentionBoundary(prev) {
			continue
		}
		var argv []string
		j := i + 1
		for j < len(toks) {
			t := toks[j]
			if t == "—" || t == "–" || t == "|" || t == "/" || t == "→" {
				break
			}
			if hasCyrillic(t) && !placeholderToken(t) {
				break
			}
			if strings.ContainsAny(t, ",:;»)") && !placeholderToken(t) && !isFlagToken(t) {
				break
			}
			argv = append(argv, t)
			j++
		}
		if len(argv) > 0 {
			out = append(out, docInvocation{
				File: file, Text: "evo " + strings.Join(argv, " "), Line: n, Argv: argv,
				Catalogue: true,
			})
		}
		i = j
	}
	return out
}

func evoMentionBoundary(prev string) bool {
	if prev == "" {
		return true
	}
	if strings.HasSuffix(prev, ";") || strings.HasSuffix(prev, ":") || strings.HasSuffix(prev, "(") {
		return true
	}
	switch prev {
	case "|", "||", "&&", "&", "sudo", "command", "exec", "time", "nohup", "env", "watch", "then", "do":
		return true
	default:
		return false
	}
}

func splitShellCommentParts(s string) (code, comment string) {
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(b.String()), strings.TrimSpace(s[i+1:])
			}
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String()), ""
}

func stripShellComment(s string) string {
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(b.String())
			}
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// splitShell splits a line on pipes and separators that are real shell
// syntax. "list|describe|call" stays one word: the bar has no spaces.
func splitShell(s string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	flush := func() {
		if t := strings.TrimSpace(b.String()); t != "" {
			out = append(out, t)
		}
		b.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		if !inSingle && !inDouble {
			switch {
			case c == ';':
				flush()
				continue
			case c == '&' && i+1 < len(s) && s[i+1] == '&':
				flush()
				i++
				continue
			case c == '|' && i+1 < len(s) && s[i+1] == '|':
				flush()
				i++
				continue
			case c == '|' && (i == 0 || s[i-1] == ' ' || i+1 == len(s) || s[i+1] == ' '):
				flush()
				continue
			}
		}
		b.WriteByte(c)
	}
	flush()
	return out
}

// shellTokens splits a segment on whitespace, keeping quotes and $(…) whole.
func shellTokens(s string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	depth := 0
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble && depth == 0:
			inSingle = !inSingle
			continue
		case c == '"' && !inSingle && depth == 0:
			inDouble = !inDouble
			continue
		case c == '<' && !inSingle && !inDouble && depth == 0:
			// <ID ВМ> is one positional even with spaces inside.
			if j := strings.IndexByte(s[i:], '>'); j >= 0 {
				b.WriteString(s[i : i+j+1])
				i += j
				continue
			}
		case c == '$' && !inSingle && i+1 < len(s) && s[i+1] == '(':
			depth++
			i++
			b.WriteString("VALUE")
			continue
		case c == '(' && depth > 0 && !inSingle:
			depth++
		case c == ')' && depth > 0 && !inSingle:
			depth--
			continue
		case (c == ' ' || c == '\t') && !inSingle && !inDouble && depth == 0:
			flush()
			continue
		}
		if depth == 0 {
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

func stripRedirect(toks []string) []string {
	var out []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t == "<" || t == ">" || t == ">>" || t == "2>" || t == "2>>":
			i++ // the path
			continue
		case t == "&" || t == `\`:
			continue
		case strings.HasPrefix(t, ">>") || strings.HasPrefix(t, ">") || t == "2>&1" || strings.HasPrefix(t, "2>"):
			continue
		case strings.HasPrefix(t, "<") && !placeholderToken(t):
			continue
		default:
			out = append(out, t)
		}
	}
	return out
}

var altToken = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*([|/][A-Za-z][A-Za-z0-9-]*)+$`)

// expandDocArgv turns a shorthand word (list|describe|call, kubectl/apply/helm)
// into one argv per alternative. Other lines stay as they are.
func expandDocArgv(argv []string) [][]string {
	idx := -1
	for i, a := range argv {
		if altToken.MatchString(a) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return [][]string{argv}
	}
	alts := strings.FieldsFunc(argv[idx], func(r rune) bool { return r == '|' || r == '/' })
	out := make([][]string, 0, len(alts))
	for _, alt := range alts {
		cp := append([]string{}, argv...)
		cp[idx] = alt
		out = append(out, cp)
	}
	// Expand a second shorthand in the same argv, if a doc ever has one.
	var more [][]string
	for _, cp := range out {
		if hasAlt(cp) {
			more = append(more, expandDocArgv(cp)...)
		} else {
			more = append(more, cp)
		}
	}
	return more
}

func hasAlt(argv []string) bool {
	for _, a := range argv {
		if altToken.MatchString(a) {
			return true
		}
	}
	return false
}

func placeholderToken(s string) bool {
	return len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>'
}

func patternToken(s string) bool {
	return s == "…" || s == "..." || s == ".." || placeholderToken(s)
}

func checkDocArgv(root *cobra.Command, argv []string, inv docInvocation) error {
	cmd := root
	var pos []string
	skipArity := false
	restMode := false
	// A fenced example states an arity. A catalogue and an inline mention
	// of a command name do not, until they actually show a positional.
	strictMin := inv.Fence && !inv.Catalogue
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			pos = append(pos, argv[i+1:]...)
			break
		}
		if !restMode && isFlagToken(a) {
			name, hasVal, _ := splitFlag(a)
			if name == "help" || name == "h" {
				continue
			}
			fl := lookupFlag(cmd, name)
			if fl == nil {
				return errf("%s: no flag --%s", cmd.CommandPath(), name)
			}
			if !hasVal && flagNeedsValue(fl) {
				if i+1 >= len(argv) {
					if strictMin {
						return errf("%s: flag --%s needs a value", cmd.CommandPath(), name)
					}
					continue
				}
				i++
			}
			continue
		}
		if !restMode {
			if sub := findSub(cmd, a); sub != nil {
				cmd = sub
				continue
			}
		}
		if patternToken(a) && (a == "…" || a == "..." || a == "..") {
			skipArity = true
			continue
		}
		if patternToken(a) && findSub(cmd, strings.Trim(a, "<>")) == nil && len(pos) == 0 && !cmdAcceptsPos(cmd) && cmd.HasSubCommands() {
			// <команда> stands for whichever subcommand. Flags after it
			// are the persistent ones.
			return checkTemplateFlags(root, argv[i+1:])
		}
		pos = append(pos, a)
		if strings.Contains(cmd.Use, "...") {
			restMode = true
		}
	}
	if skipArity {
		return nil
	}
	return checkArity(cmd, pos, strictMin)
}

func checkTemplateFlags(root *cobra.Command, argv []string) error {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if !isFlagToken(a) {
			continue
		}
		name, hasVal, _ := splitFlag(a)
		if name == "help" || name == "h" {
			continue
		}
		fl := lookupFlag(root, name)
		if fl == nil {
			return errf("%s: no flag --%s", root.CommandPath(), name)
		}
		if !hasVal && flagNeedsValue(fl) && i+1 < len(argv) {
			i++
		}
	}
	return nil
}

func isFlagToken(s string) bool {
	return strings.HasPrefix(s, "-") && s != "-" && !strings.HasPrefix(s, "---")
}

func splitFlag(s string) (name string, hasVal, inline bool) {
	s = strings.TrimLeft(s, "-")
	if i := strings.IndexByte(s, '='); i >= 0 {
		return s[:i], true, true
	}
	return s, false, false
}

func flagNeedsValue(f *pflag.Flag) bool {
	switch f.Value.Type() {
	case "bool", "boolSlice", "count":
		return false
	default:
		return true
	}
}

func lookupFlag(cmd *cobra.Command, name string) *pflag.Flag {
	sets := []*pflag.FlagSet{cmd.InheritedFlags(), cmd.PersistentFlags(), cmd.LocalFlags(), cmd.Flags()}
	for _, set := range sets {
		if set == nil {
			continue
		}
		if f := set.Lookup(name); f != nil {
			return f
		}
		var found *pflag.Flag
		set.VisitAll(func(f *pflag.Flag) {
			if f.Shorthand == name {
				found = f
			}
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func findSub(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name || commandHasAlias(c, name) {
			return c
		}
	}
	return nil
}

func commandHasAlias(c *cobra.Command, name string) bool {
	for _, a := range c.Aliases {
		if a == name {
			return true
		}
	}
	return false
}

func cmdAcceptsPos(cmd *cobra.Command) bool {
	if cmd.Args != nil {
		// NoArgs fails on one argument; anything else that accepts one
		// succeeds. A custom function is asked with a single placeholder.
		return cmd.Args(cmd, []string{"VALUE"}) == nil
	}
	_, max := arityFromUse(cmd.Use)
	return max != 0
}

func checkArity(cmd *cobra.Command, pos []string, strictMin bool) error {
	if !strictMin && len(pos) == 0 {
		return nil
	}
	if cmd.Args != nil {
		if err := cmd.Args(cmd, pos); err != nil {
			return err
		}
		return nil
	}
	min, max := arityFromUse(cmd.Use)
	if len(pos) < min || (max >= 0 && len(pos) > max) {
		return errf("%s: %d positional argument(s), the command takes %s", cmd.CommandPath(), len(pos), strings.TrimPrefix(cmd.Use, cmd.Name()))
	}
	return nil
}

// arityFromUse reads cobra's Use line. max < 0 means unlimited.
func arityFromUse(use string) (min, max int) {
	fields := strings.Fields(use)
	if len(fields) <= 1 {
		return 0, 0
	}
	for _, f := range fields[1:] {
		switch {
		case f == "[flags]" || f == "[command]" || f == "[commands]":
			continue
		case strings.Contains(f, "..."):
			if strings.HasPrefix(f, "[") {
				return min, -1
			}
			return min + 1, -1
		case strings.HasPrefix(f, "["):
			max++
		default:
			min++
			max++
		}
	}
	return min, max
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
