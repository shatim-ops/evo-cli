// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// cloudMark is what the cloud's text carries in the world of
// TestHumanOutput_CloudTextIsClean: a line and a paragraph separator (Zl,
// Zp), a bidi override and a zero-width space (Cf), an escape sequence
// that clears a terminal, and a byte that is not UTF-8 (rawFF, put into
// the body after the JSON is made: a JSON string cannot hold it).
const (
	cloudMark = "mark\u2028Zl\u2029Zp\u202eCf\u200bZW\x1b[2JANSI" + rawFF
	rawFF     = "@@FF@@"
)

// cloudTextSuffixes end the keys of an answer whose values are the
// cloud's free text — names, descriptions, versions, messages — and not a
// value evo reads as a state or an id (name, display_name, appVersion):
// the mark goes into these.
var cloudTextSuffixes = []string{"name", "version", "description", "title", "message", "detail", "details", "comment", "label"}

func cloudText(key string) bool {
	k := normKey(key)
	for _, s := range cloudTextSuffixes {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// markCloudText puts cloudMark at the end of every value of the cloud's text
// in a JSON answer (cloudText), but a value the call names itself (an argument: the
// name of an addon the call reads, the type of a disk it asks for) — that
// one is the call's, not the cloud's, and a lookup by it must still find
// it. Another answer goes as it is.
func markCloudText(named map[string]bool, resp *http.Response) *http.Response {
	if resp.Body == nil {
		return resp
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	var v any
	if err != nil || json.Unmarshal(body, &v) != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(markValues(v, "", named))
	out := bytes.ReplaceAll(buf.Bytes(), []byte(rawFF), []byte{0xff})
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Del("Content-Length")
	return resp
}

func markValues(v any, key string, named map[string]bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = markValues(e, k, named)
		}
	case []any:
		for i, e := range x {
			x[i] = markValues(e, key, named)
		}
	case string:
		if cloudText(key) && x != "" && !named[x] {
			return x + cloudMark
		}
	}
	return v
}

// unclean names what a terminal must not get in s: a line or paragraph
// separator, a format character, a control character other than a line
// break or a tab, a byte that is not UTF-8; "" when there is none.
func unclean(s string) string {
	if !utf8.ValidString(s) {
		i := 0
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				return "a byte that is not UTF-8 near " + clipAround(s, i)
			}
			i += n
		}
	}
	for i, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
		case unicode.In(r, unicode.Zl, unicode.Zp), unicode.Is(unicode.Cf, r), unicode.IsControl(r):
			return fmt.Sprintf("%U near %s", r, clipAround(s, i))
		}
	}
	return ""
}

func clipAround(s string, i int) string {
	from, to := max(0, i-40), min(len(s), i+20)
	return fmt.Sprintf("%q", s[from:to])
}

// humanUncleanMisses are the runs whose human output still prints the
// cloud's text as it came: "tool", or "tool (every optional argument)" →
// where and who closes it. The list only shrinks: a run whose output is
// clean now fails the test. Closes by 1.9 (release plan 1.9, before
// rc.1): the fix is «разработки»; «Процесс» does not change product code.
var humanUncleanMisses = map[string]string{
	"deploy":                            "the step «ВМ <name> (<id>) активна» of deploy names the VM as the cloud sends it — «разработка»",
	"deploy (every optional argument)":  "the same step of deploy — «разработка»",
	"lb_list (every optional argument)": "the hint «Известные: …» of a refused filter (internal/tools/list_filters.go) lists the names as the cloud sends them — «разработка»",
	"tg_list (every optional argument)": "the same hint of a refused filter — «разработка»",
}

// humanLocalCommands are the local commands (localCommands): no tool, so
// no case of the world above. Each says whether its human output can carry
// the cloud's text and, if it can, why the world does not run it — a new
// local command not here fails the test.
var humanLocalCommands = map[string]struct {
	cloud bool
	why   string
}{
	"evo configure":             {true, "menus of projects and zones: the world has no IAM project list; #2026 checks its three places by its own test. Closes by 1.10"},
	"evo login":                 {true, "a browser and a loopback port: the world has no login flow. Closes by 1.10"},
	"evo logout":                {true, "deletes the key evo login made, by its token: the world has no login flow. Closes by 1.10"},
	"evo configure s3":          {true, "makes S3 keys from the access key: the world has no IAM flow for it. Closes by 1.10"},
	"evo project use":           {true, "names the project by its name from the project list (printer.Success, not output.SanitizeCell — a hypothesis from the code, to «разработка»); the world's list is empty and its IAM gives no token. Closes by 1.10"},
	"evo init":                  {true, "interactive menus of zones and images: the world runs no stdin menus. Closes by 1.10"},
	"evo verify":                {true, "prints what the ports of the deployed app answer: text of the app, not of the cloud, of the same class. Closes by 1.10"},
	"evo tools call":            {true, "prints the tool's answer as JSON — machine output, as --json, not the human one this test reads"},
	"evo configure list":        {false, "profiles on the disk"},
	"evo configure delete":      {false, "a profile on the disk"},
	"evo project":               {false, "a group"},
	"evo version":               {false, "the binary's version"},
	"evo completion":            {false, "a group"},
	"evo completion bash":       {false, "a script of the shell"},
	"evo completion zsh":        {false, "a script of the shell"},
	"evo completion fish":       {false, "a script of the shell"},
	"evo completion powershell": {false, "a script of the shell"},
	"evo mcp":                   {false, "the MCP server: JSON-RPC, not the human output"},
	"evo mcp healthcheck":       {false, "the server's liveness"},
	"evo tools":                 {false, "a group"},
	"evo tools list":            {false, "the registry"},
	"evo tools describe":        {false, "the registry"},
	"evo agent setup":           {false, "files of the MCP client"},
	"evo diag":                  {false, "a group"},
	"evo diag on":               {false, "config.yaml"},
	"evo diag off":              {false, "config.yaml"},
	"evo diag status":           {false, "the session log's file"},
	"evo diag export":           {false, "copies the session log's file"},
	"evo crew":                  {false, "credits"},
	"evo help":                  {false, "help of cobra"},
}

// Every local command is in humanLocalCommands, with a why: a new one that
// reads the cloud says how its human output is checked.
func TestHumanOutput_LocalCommandsAreDeclared(t *testing.T) {
	var bad []string
	seen := map[string]bool{}
	for _, lc := range localCommands {
		seen[lc.Path] = true
		if d, ok := humanLocalCommands[lc.Path]; !ok || d.why == "" {
			bad = append(bad, lc.Path+": a local command not in humanLocalCommands — does its human output carry the cloud's text?")
		}
	}
	for path := range humanLocalCommands {
		if !seen[path] {
			bad = append(bad, path+": in humanLocalCommands, but not a local command: remove the line")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// Release plan 1.9, «Инвариант класса «человеческий вывод печатает текст
// облака только очищенным»» (#1848, #1882, #1950, #2024 were closed place
// by place, and their tests read the cleaning function, not what the
// person saw): every command of the world of TestParity_JSONEqualsTool
// runs without --json, its answers from the cloud carrying cloudMark in
// their free text, and neither its stdout nor its stderr has a character
// of the mark but in the cleaned form (output.SanitizeCell).
func TestHumanOutput_CloudTextIsClean(t *testing.T) {
	w, fake := withJSONWorld(t)
	var named map[string]bool
	w.rt.after = func(resp *http.Response) *http.Response { return markCloudText(named, resp) }
	cases, _, _ := jsonCases(t)
	var bad []string
	found := map[string]string{}
	ran, marked := 0, 0
	run := func(c jsonCase, key string, base map[string]any) {
		args := copyArgs(base)
		done := func() {}
		if c.setup != nil {
			extra, d := c.setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			done = d
		}
		named = map[string]bool{}
		for k, a := range args {
			leaves(a, k, func(_, value string) { named[value] = true })
		}
		line := c.line
		if ec, engine := engineCommandOf(c.tool.Name); engine {
			line = engineLine(t, ec, args)
		} else if line == nil || len(base) != len(c.args) {
			cmd := commandsByTool()[c.tool.Name]
			line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
		}
		fake.reset()
		newProcess()
		lang := i18n.Lang()
		i18n.SetLang(i18n.RU)
		_, out, errOut := runCLI(t, c.stdin, append([]string{"--yes"}, line...)...)
		i18n.SetLang(lang)
		done()
		forgetRun(w)
		ran++
		if strings.Contains(out+errOut, "mark") {
			marked++
		}
		for name, s := range map[string]string{"stdout": out, "stderr": errOut} {
			if why := unclean(s); why != "" {
				found[key] = name + ": " + why
			}
		}
	}
	for _, c := range cases {
		key := c.tool.Name
		if c.state != "" {
			key += " (" + c.state + ")"
		}
		run(c, key, c.args)
		// Every optional argument set: a flag that prints more (--full)
		// prints more of the cloud's text.
		if full := commandArgs(c.tool, fullOptionalArgsOf(c)); len(full) > len(c.args) && c.state == "" {
			run(c, key+" (every optional argument)", full)
		}
	}
	for key, why := range found {
		if humanUncleanMisses[key] == "" {
			bad = append(bad, fmt.Sprintf("%s: the cloud's text goes to the terminal as it came — %s; print it through output.SanitizeCell", key, why))
		}
	}
	for key := range humanUncleanMisses {
		if found[key] == "" {
			bad = append(bad, key+": in humanUncleanMisses, but its output is clean now: remove the line")
		}
	}
	t.Logf("runs: %d; with the cloud's marked text in their output: %d", ran, marked)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
	w.rt.takeUnknown()
}

func fullOptionalArgsOf(c jsonCase) map[string]any {
	full, _ := fullOptionalArgs(c.tool, c.args, map[string]bool{})
	return full
}

// commandArgs are the arguments of args the command of tl takes: an
// argument the CLI fills itself (the project of the profile) has no flag.
func commandArgs(tl tools.Tool, args map[string]any) map[string]any {
	takes := map[string]bool{}
	if ec, engine := engineCommandOf(tl.Name); engine {
		for _, arg := range ec.Flags {
			takes[arg] = true
		}
	} else if cmd, ok := commandsByTool()[tl.Name]; ok {
		for _, pos := range parsePositionals(cmd.cmd.Args) {
			// One of the names of a positional: the other is its
			// alternative (vm_id or name).
			n := pos.names[0]
			if _, given := args[n]; !given && len(pos.names) == 2 {
				n = pos.names[1]
			}
			takes[n] = true
		}
		for _, p := range cmd.cmd.flagArgs(tl) {
			takes[p.Name] = true
		}
	}
	out := map[string]any{}
	for k, v := range args {
		if takes[k] {
			out[k] = v
		}
	}
	return out
}
