// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// TestDocsExamples_Counts pins the numbers the user docs state: 155 tools,
// 24 --toolsets groups, 89 tools left under --read-only, 27 tools in
// deploy,vm,vpc, and 17 read-only tools in vm,vpc,disk,image.
func TestDocsExamples_Counts(t *testing.T) {
	list := newRegistry().Tools()
	groups := map[string]int{}
	readOnly := 0
	readOnlySlice := 0
	for _, tl := range list {
		groups[tl.Group]++
		if tools.AlwaysNeedsConsent(tl) {
			continue
		}
		readOnly++
		switch tl.Group {
		case "vm", "vpc", "disk", "image":
			readOnlySlice++
		}
	}
	subset := groups["deploy"] + groups["vm"] + groups["vpc"]
	if len(list) != 155 || len(groups) != 24 || readOnly != 89 || subset != 27 || readOnlySlice != 17 {
		names := make([]string, 0, len(groups))
		for g, n := range groups {
			names = append(names, fmt.Sprintf("%s=%d", g, n))
		}
		t.Fatalf("binary: tools %d, groups %d (%s), read-only %d, deploy+vm+vpc %d, read-only vm+vpc+disk+image %d",
			len(list), len(groups), strings.Join(names, " "), readOnly, subset, readOnlySlice)
	}
	if got := len(tools.Groups()); got != len(groups) {
		t.Errorf("tools.Groups() lists %d names, the registry uses %d", got, len(groups))
	}

	flat := func(rel string) string {
		return strings.Join(strings.Fields(string(mustReadRepo(t, rel))), " ")
	}
	mcp := flat("docs/mcp.md")
	readme := flat("README.md")
	dog := flat("docs/internal/dogfood-protocol.md")
	for _, text := range []string{mcp, readme, dog} {
		if !strings.Contains(text, "155") {
			t.Errorf("a user doc dropped the tool count 155")
			break
		}
	}
	if !strings.Contains(mcp, "остаются 89") || !strings.Contains(mcp, "27 инструментов") || !strings.Contains(mcp, "17 инструментов") {
		t.Errorf("docs/mcp.md no longer states 89 read-only, 27 in deploy,vm,vpc, or 17 in vm,vpc,disk,image")
	}
	if !strings.Contains(dog, "27 инструментов") || !strings.Contains(dog, "155 инструментов") {
		t.Errorf("docs/internal/dogfood-protocol.md no longer states 155 tools or 27 in deploy,vm,vpc")
	}
}

// TestDocsExamples_ExitCodes checks the reason names in docs/exit-codes.md
// against ExitCode, and that evo --help prints the same numeric codes.
func TestDocsExamples_ExitCodes(t *testing.T) {
	text := string(mustReadRepo(t, "docs/exit-codes.md"))
	rows := exitCodeRows(text)
	if len(rows) < 7 {
		t.Fatalf("parsed %d exit-code rows", len(rows))
	}
	help := clerrors.ExitCodesHelp()
	for _, row := range rows {
		if !strings.Contains(help, strconv.Itoa(row.code)) {
			t.Errorf("evo --help does not list exit code %d", row.code)
		}
		for _, reason := range row.reasons {
			// The cell also names env vars in backticks (MCP_BEARER_TOKEN).
			// Only symbolic error reasons are exit codes.
			if !clerrors.IsCode(reason) {
				continue
			}
			got := clerrors.ExitCode(clerrors.New(reason, "m", ""))
			if got != row.code {
				t.Errorf("docs: %s exits %d; binary exits %d", reason, row.code, got)
			}
		}
	}
	// The JSON shape in the same document: code is the exit code of reason.
	for _, body := range jsonBodies(text) {
		var wrap struct {
			Error struct {
				Code   int    `json:"code"`
				Reason string `json:"reason"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &wrap) != nil || wrap.Error.Reason == "" {
			continue
		}
		if got := clerrors.ExitCode(clerrors.New(wrap.Error.Reason, "m", "")); got != wrap.Error.Code {
			t.Errorf("sample JSON: reason %s is exit %d, the sample says %d", wrap.Error.Reason, got, wrap.Error.Code)
		}
	}
}

type exitRow struct {
	code    int
	reasons []string
}

var reasonToken = regexp.MustCompile("`([A-Z][A-Z0-9_]+)`")

func exitCodeRows(text string) []exitRow {
	var out []exitRow
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 5 {
			continue
		}
		raw := strings.Trim(cols[1], " `")
		code, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		var reasons []string
		for _, m := range reasonToken.FindAllStringSubmatch(cols[3], -1) {
			reasons = append(reasons, m[1])
		}
		out = append(out, exitRow{code: code, reasons: reasons})
	}
	return out
}

// TestDocsExamples_MCPConfig checks the Cursor and Claude Code MCP JSON:
// it parses, and an evo command's args are a real evo invocation.
func TestDocsExamples_MCPConfig(t *testing.T) {
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	var n int
	for _, rel := range userDocs {
		text := string(mustReadRepo(t, rel))
		for _, body := range mcpJSONObjects(text) {
			n++
			if err := checkMCPJSON(root, body); err != nil {
				t.Errorf("%s: %s\n%s", rel, err, body)
			}
		}
		for _, args := range inlineMCPArgs(text) {
			n++
			if err := checkDocArgv(root, args, docInvocation{Fence: true}); err != nil {
				t.Errorf("%s: mcp args %v: %s", rel, args, err)
			}
		}
	}
	if n < 4 {
		t.Fatalf("only %d MCP configs parsed", n)
	}
}

// mcpJSONObjects returns every JSON object in the document that has an
// mcpServers field, fences and one-line prose alike. Brace matching stops
// at the object's end, so a trailing sentence is not part of the value.
func mcpJSONObjects(text string) []string {
	var out []string
	seen := map[string]bool{}
	rest := text
	for {
		i := strings.Index(rest, "mcpServers")
		if i < 0 {
			break
		}
		start := strings.LastIndex(rest[:i], "{")
		if start >= 0 {
			obj, ok := balancedObject(rest[start:])
			if ok && strings.Contains(obj, `"mcpServers"`) && !seen[obj] {
				seen[obj] = true
				out = append(out, obj)
			}
		}
		rest = rest[i+len("mcpServers"):]
	}
	return out
}

func balancedObject(s string) (string, bool) {
	depth := 0
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

func jsonBodies(text string) []string {
	var out []string
	rest := text
	for {
		i := strings.Index(rest, "```json")
		if i < 0 {
			break
		}
		rest = rest[i+7:]
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			break
		}
		rest = rest[nl+1:]
		end := strings.Index(rest, "```")
		if end < 0 {
			break
		}
		out = append(out, strings.TrimSpace(rest[:end]))
		rest = rest[end+3:]
	}
	// One-line configs live in prose and backticks.
	return append(out, regexp.MustCompile(`\{[^{}]*"mcpServers"\s*:\s*\{.*?\}\s*\}`).FindAllString(text, -1)...)
}

func inlineMCPArgs(text string) [][]string {
	var out [][]string
	for _, m := range regexp.MustCompile(`"args"\s*:\s*(\[[^]\n]*\])`).FindAllStringSubmatch(text, -1) {
		var args []string
		if json.Unmarshal([]byte(m[1]), &args) != nil {
			continue
		}
		out = append(out, args)
	}
	return out
}

func checkMCPJSON(root *cobra.Command, body string) error {
	if !json.Valid([]byte(body)) {
		return fmt.Errorf("invalid JSON")
	}
	var cfg struct {
		McpServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Type    string   `json:"type"`
			URL     string   `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		return err
	}
	srv, ok := cfg.McpServers["evo"]
	if !ok {
		return fmt.Errorf("mcpServers.evo is missing")
	}
	if srv.Command != "" && srv.Command != "evo" && srv.Command != "evo.exe" {
		return fmt.Errorf("command %q", srv.Command)
	}
	if srv.Command != "" {
		if err := checkDocArgv(root, srv.Args, docInvocation{Fence: true}); err != nil {
			return err
		}
	}
	if srv.URL != "" {
		u, err := url.Parse(srv.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("url %q", srv.URL)
		}
	}
	if srv.Command == "" && srv.URL == "" {
		return fmt.Errorf("neither command nor url")
	}
	return nil
}

// TestDocsExamples_Links checks relative links and heading anchors between
// the user docs. Generated command pages are targets only.
func TestDocsExamples_Links(t *testing.T) {
	anchors := map[string]map[string]bool{}
	headingAnchors := func(rel string) map[string]bool {
		if a, ok := anchors[rel]; ok {
			return a
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			return nil
		}
		a := anchorSet(string(b))
		anchors[rel] = a
		return a
	}
	var n int
	for _, rel := range userDocs {
		text := string(mustReadRepo(t, rel))
		for _, lnk := range markdownLinks(text) {
			if skipDocLink(lnk.raw) {
				continue
			}
			n++
			target, frag := resolveDocLink(rel, lnk.raw)
			full := filepath.Join(repoRoot, target)
			st, err := os.Stat(full)
			if err != nil || st.IsDir() {
				t.Errorf("%s:%d: %s does not resolve (%s)", rel, lnk.line, lnk.raw, target)
				continue
			}
			if frag == "" {
				continue
			}
			if !strings.HasSuffix(target, ".md") {
				t.Errorf("%s:%d: anchor on a non-markdown file %s", rel, lnk.line, lnk.raw)
				continue
			}
			if !headingAnchors(target)[frag] {
				t.Errorf("%s:%d: no heading #%s in %s", rel, lnk.line, frag, target)
			}
		}
	}
	if n < 20 {
		t.Fatalf("only %d relative links parsed", n)
	}
}

type docLink struct {
	raw  string
	line int
}

var mdLink = regexp.MustCompile(`\[[^\]\n]*\]\(([^)\n]+)\)`)

func markdownLinks(src string) []docLink {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []docLink
	line := 1
	for {
		i := strings.Index(src, "```")
		if i < 0 {
			out = append(out, linksIn(src, line)...)
			break
		}
		out = append(out, linksIn(src[:i], line)...)
		line += strings.Count(src[:i], "\n")
		src = src[i+3:]
		nl := strings.IndexByte(src, '\n')
		if nl < 0 {
			break
		}
		line++
		src = src[nl+1:]
		end := strings.Index(src, "```")
		if end < 0 {
			break
		}
		line += strings.Count(src[:end], "\n")
		src = src[end+3:]
	}
	return out
}

func linksIn(src string, start int) []docLink {
	var out []docLink
	for _, m := range mdLink.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, docLink{
			raw:  strings.TrimSpace(src[m[2]:m[3]]),
			line: start + strings.Count(src[:m[2]], "\n"),
		})
	}
	return out
}

func skipDocLink(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "irc:")
}

func resolveDocLink(from, raw string) (path, frag string) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexAny(raw, " \t"); i >= 0 {
		raw = raw[:i]
	}
	pathPart, frag, _ := strings.Cut(raw, "#")
	frag = strings.TrimSpace(frag)
	if pathPart == "" {
		return from, frag
	}
	dir := filepath.Dir(from)
	if dir == "." {
		dir = ""
	}
	joined := filepath.Clean(filepath.Join(dir, pathPart))
	return joined, frag
}

func anchorSet(src string) map[string]bool {
	seen := map[string]int{}
	out := map[string]bool{}
	for _, h := range atxHeadings(src) {
		base := githubAnchor(h)
		n := seen[base]
		seen[base]++
		slug := base
		if n > 0 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		out[slug] = true
	}
	return out
}

func atxHeadings(src string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trim, "#") {
			continue
		}
		i := 0
		for i < len(trim) && trim[i] == '#' {
			i++
		}
		if i == 0 || i > 6 || i == len(trim) || trim[i] != ' ' {
			continue
		}
		text := strings.TrimSpace(trim[i:])
		text = strings.TrimRight(text, "#")
		text = strings.TrimSpace(text)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// githubAnchor is the slug GitHub gives an ATX heading: lowercase, no
// punctuation, spaces become one hyphen. Hyphens already in the heading
// stay, so "чтение --read-only" is "чтение---read-only".
func githubAnchor(h string) string {
	h = strings.ReplaceAll(h, "`", "")
	h = strings.ReplaceAll(h, "**", "")
	h = strings.ReplaceAll(h, "__", "")
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			if space && b.Len() > 0 {
				b.WriteByte('-')
				space = false
			}
			b.WriteRune(r)
		case unicode.IsSpace(r):
			space = true
		default:
			// punctuation is dropped and does not become a hyphen
		}
	}
	return b.String()
}

// TestDocsExamples_Offline runs the commands the docs describe as usable
// without the cloud and checks the exit codes the docs name.
func TestDocsExamples_Offline(t *testing.T) {
	out, errOut, code := execOffline(t, "version")
	if code != 0 || !strings.Contains(out+errOut, "evo") {
		t.Fatalf("evo version: exit %d\n%s%s", code, out, errOut)
	}
	_, _, code = execOffline(t, "completion", "bash")
	if code != 0 {
		t.Fatalf("evo completion bash: exit %d", code)
	}
	out, errOut, code = execOffline(t, "mcp", "--help")
	if code != 0 || !strings.Contains(out, "--read-only") || !strings.Contains(out, "--toolsets") {
		t.Fatalf("evo mcp --help: exit %d\n%s%s", code, out, errOut)
	}
	// docs/mcp.md: an unknown group is exit 1, and --read-only does not
	// combine with --allow-secret-export (exit 1).
	_, _, code = execOffline(t, "mcp", "--toolsets", "not-a-group")
	if code != clerrors.ExitGeneral {
		t.Errorf("unknown --toolsets group: exit %d, docs say 1", code)
	}
	_, _, code = execOffline(t, "mcp", "--read-only", "--allow-secret-export")
	if code != clerrors.ExitGeneral {
		t.Errorf("--read-only with --allow-secret-export: exit %d, docs say 1", code)
	}
	_, _, code = execOffline(t, "no-such-command")
	if code != clerrors.ExitGeneral {
		t.Errorf("unknown command: exit %d, docs say 1", code)
	}
	help, _, code := execOffline(t, "--help")
	if code != 0 {
		t.Fatalf("evo --help: exit %d", code)
	}
	for _, want := range []string{"0", "1", "2", "3", "4", "5", "130"} {
		if !strings.Contains(help, want) {
			t.Errorf("evo --help does not mention exit code %s", want)
		}
	}
}

func execOffline(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("CLOUDRU_KEY_ID", "00000000000000000000000000000000")
	t.Setenv("CLOUDRU_SECRET", "fake-secret")
	t.Setenv("CLOUDRU_API_KEY", "")
	t.Setenv("CLOUDRU_PROJECT_ID", "00000000-0000-0000-0000-000000000000")
	// The completion command closes over stdout at the moment the command
	// tree is built, so the pipe has to be in place before newRootCmd.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	var leaked bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&leaked, r)
		close(done)
	}()
	root := newRootCmd()
	root.SetArgs(args)
	var out, errOut strings.Builder
	root.SetOut(&out)
	root.SetErr(&errOut)
	_, runErr := root.ExecuteContextC(context.Background())
	os.Stdout = old
	_ = w.Close()
	<-done
	_ = r.Close()
	if runErr != nil {
		errOut.WriteString(runErr.Error())
	}
	out.Write(leaked.Bytes())
	return out.String(), errOut.String(), clerrors.ExitCode(runErr)
}

// TestDocsExamples_ImageNotInProjectPreview pins a 1.2 behaviour the user
// docs still describe as 1.1: an image the project cannot use is refused
// on the unconfirmed preview, and that preview reads the catalogue.
// docs/exit-codes.md and docs/mcp.md say the check happens only with
// confirmed=true and that the preview does not call the cloud.
func TestDocsExamples_ImageNotInProjectPreview(t *testing.T) {
	for _, tool := range []string{"provision", "deploy"} {
		t.Run(tool, func(t *testing.T) {
			cloud := withVMCreateCloud(t)
			cloud.hidden[imgID(1)] = true
			args := []string{"--json", "tools", "call", tool, "--arg", "image=" + imgID(1)}
			if tool == "provision" {
				args = append(args, "--arg", "name=web")
			} else {
				// deploy reads its compose file first (D48).
				compose := filepath.Join(t.TempDir(), "docker-compose.yml")
				writeFile(t, compose, "services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n")
				t.Setenv("CLOUDRU_COMPOSE_FILE", compose)
			}
			out, errOut, code := execCLI(t, args...)
			body := out + errOut
			if code != clerrors.ExitNotFound || strings.Contains(body, "confirmation_required") || !strings.Contains(body, "NOT_FOUND") {
				t.Fatalf("unconfirmed %s: exit %d, want 4 NOT_FOUND before confirmation\n%s", tool, code, body)
			}
			noMutations(t, cloud)
		})
	}

	falseClaim := []struct{ file, phrase string }{
		{"docs/exit-codes.md", "превью в облако не ходит"},
		{"docs/mcp.md", "превью без подтверждения в облако не ходит"},
	}
	for _, c := range falseClaim {
		flat := strings.Join(strings.Fields(string(mustReadRepo(t, c.file))), " ")
		if strings.Contains(flat, c.phrase) {
			t.Errorf("%s still says %q, but an unconfirmed provision and deploy already exit 4 NOT_FOUND after reading the project catalogue", c.file, c.phrase)
		}
	}
}
