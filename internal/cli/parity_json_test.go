// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The --json of a command is the JSON of its tool (spec.md REQ-8, C4): for
// every tool with a command, the arguments of parityArgs, on the fake cloud
// of the parity tests, 'evo <command> --json --yes' prints to stdout what
// the tool's handler answers to the same arguments with confirmed=true,
// byte for byte; logs, whose answer is the log as text (owner's decision
// Q7), prints that text. The commands that give something else are the
// closed list jsonAliasCompat, each with the reason and the form it gives
// instead.

// jsonCompat is a command whose --json is not its tool's answer as is: why
// (reason), and what it is instead (check: "" when cmdOut is that form of
// the tool's answer text).
type jsonCompat struct {
	reason string
	// check is the form of the answer (TestParity_JSONEqualsTool); nil:
	// the answer is the tool's.
	check func(cmdOut, cmdErr, text string) string
	// onError is what the command gives where its tool fails with the
	// error text (TestParity_ErrorReasonEqualsTool): applies is false for
	// an error the command gives as the tool does; why is "" when the
	// command gives that. nil: every error is the tool's.
	onError func(code int, cmdOut, cmdErr, text string) (why string, applies bool)
}

// jsonAliasCompat are the commands whose --json is not the answer of their
// tool byte for byte, and why: the forms v1.0 gave, kept until v2.0
// (spec.md §4), and the differences that cannot be closed without taking
// a key or a value away from one of the two surfaces (in 1.2 only Q1
// breaks). An entry whose command does give the tool's answer fails
// TestParity_JSONEqualsTool: it is dropped, not kept "just in case".
var jsonAliasCompat = map[string]jsonCompat{
	"evo list-zones": {
		reason: "скрытый устаревший алиас evo vm zones: --json — массив zones из ответа инструмента, как с v1.0; до v2.0 (spec.md §4)",
		check: func(cmdOut, cmdErr, text string) string {
			return arrayOfAnswer(cmdOut, cmdErr, text, "zones", "evo list-zones is deprecated")
		}},
	"evo list-images": {
		reason: "скрытая устаревшая команда list_images (evo image list): --json — массив images из ответа инструмента, пустой — без вывода, как с v1.0; до v2.0",
		check: func(cmdOut, cmdErr, text string) string {
			return arrayOfAnswer(cmdOut, cmdErr, text, "images", "evo list-images is deprecated")
		}},
	"evo project list": {
		reason: "алиас evo org projects: ответ Organization API с ошибкой (и 5xx) — пометка note и активный проект, код 0, как с v1.0 (QA r3 C3); инструмент так делает только для 400, 403 и 404; до v2.0 (engineCommands)",
		onError: func(code int, cmdOut, _, text string) (string, bool) {
			cmd := parseObject(cmdOut)
			msg, _ := parseObject(text)["error"].(string)
			switch {
			case !strings.HasPrefix(msg, "Organization API: HTTP "):
				return "", false // no answer of the Organization API: the tool's error
			case code != 0:
				return fmt.Sprintf("exit %d, not 0", code), true
			case cmd["projects"] != nil || cmd["active"] != pvProject || cmd["note"] != scopeText(nil, errors.New(msg)):
				return "not the note with the active project", true
			}
			return "", true
		}},
	// The two below are questions to the owner (report of PR 6): each
	// surface has a key the other lacks and both have status with other
	// values, so neither can take the other's form without losing a key
	// or a value one of them gives since v1.0 (CLI) or v1.1 (MCP).
	"evo deploy": {
		reason: "итог движка, как с v1.0: status deployed/unchanged, vm_id, vm_ip, vm_name, zone_name, flavor, ports, verify…; инструмент — status ok, deployment (запись state) и log. " +
			"Ключ status есть у обоих с разными значениями — свести без потери нельзя (в 1.2 ломается только Q1): вопрос владельцу",
		check: func(cmdOut, _, text string) string {
			cmd, tool := parseObject(cmdOut), parseObject(text)
			dep, _ := tool["deployment"].(map[string]any)
			switch {
			case tool["status"] != "ok" || dep == nil:
				return "the tool's answer is not status ok with the deployment"
			case cmd["status"] != "deployed" && cmd["status"] != "unchanged":
				return fmt.Sprintf("status %v, not deployed or unchanged", cmd["status"])
			case cmd["vm_id"] != dep["vm_id"] || cmd["vm_ip"] != dep["vm_ip"]:
				return "vm_id or vm_ip is not the deployment's"
			}
			return ""
		}},
	"evo destroy": {
		reason: "итог движка, как с v1.0: status destroyed с vm_id, vm_ip, vm_name, без развёртывания — status none с message; инструмент — status ok и log. " +
			"Ключ status есть у обоих с разными значениями — свести без потери нельзя: вопрос владельцу",
		check: func(cmdOut, _, text string) string {
			cmd, tool := parseObject(cmdOut), parseObject(text)
			log, _ := tool["log"].(string)
			switch {
			case tool["status"] != "ok":
				return "the tool's answer is not status ok"
			case cmd["status"] == "destroyed" && strings.Contains(log, fmt.Sprint(cmd["vm_id"])):
			case cmd["status"] == "none" && strings.Contains(log, fmt.Sprint(cmd["message"])):
			default:
				return fmt.Sprintf("status %v is not the VM or the no-deployment of the tool's log", cmd["status"])
			}
			return ""
		}},
}

// textAnswer are the tools whose answer is text, not JSON, with the
// reason: their command prints that text as it is.
var textAnswer = map[string]string{
	"logs": "ответ — текст журнала (решение владельца Q7, единственное исключение C4)",
}

// jsonCase is one command of a tool with the arguments it gives the tool.
type jsonCase struct {
	path  string // "evo vm get", "evo destroy"
	state string // the state of the directory, when the command has several
	tool  tools.Tool
	line  []string       // the command line without --json --yes; nil: built from args when the case runs
	args  map[string]any // the tool's arguments for the same input
	setup func(t *testing.T, w parityWorld) (map[string]any, func())
}

// jsonCases are the commands of every tool with a command: the command its
// CLISpec names (a tree, or an engine command of engineCommands), deploy in
// its other states, and the aliases engineCommands lists, each with the
// arguments of parityArgs. A tool with a command and no entry in
// parityArgs, or an entry that is no tool, is reported in bad.
func jsonCases(t *testing.T) (cases []jsonCase, withCommand int, bad []string) {
	t.Helper()
	reg := newRegistry()
	commands := commandsByTool()
	for name := range parityArgs {
		if _, ok := reg.Lookup(name); !ok {
			bad = append(bad, name+": in parityArgs, but no tool")
		}
	}
	for _, tl := range reg.Tools() {
		if tl.CLI.Verb == "" {
			continue
		}
		withCommand++
		args, ok := parityArgs[tl.Name]
		if !ok {
			bad = append(bad, tl.Name+": has a command and no entry in parityArgs")
			continue
		}
		jc := jsonCase{tool: tl, args: args, setup: paritySetup[tl.Name]}
		if ec, engine := engineCommandOf(tl.Name); engine {
			jc.path, jc.line = ec.Path, engineLine(t, ec, args)
		} else if c, ok := commands[tl.Name]; ok {
			jc.path = strings.Join(strings.Fields("evo "+c.group.Name+" "+c.cmd.Verb), " ")
		} else {
			bad = append(bad, tl.Name+": no command in the trees")
			continue
		}
		cases = append(cases, jc)
	}
	// deploy in its other states: the directory has a deployment, which it
	// updates, or which runs the compose file as it is.
	if dep, ok := reg.Lookup("deploy"); ok {
		cases = append(cases,
			jsonCase{path: "evo deploy", state: "deployment recorded", tool: dep, line: []string{"deploy"}, args: parityArgs["deploy"], setup: recordDeployment},
			jsonCase{path: "evo deploy", state: "compose unchanged", tool: dep, line: []string{"deploy"}, args: parityArgs["deploy"], setup: recordUnchanged})
	}
	// The aliases, with what each gives its tool.
	for _, ec := range engineCommands {
		if ec.Alias == "" {
			continue
		}
		tl, _ := reg.Lookup(ec.Tool)
		jc := jsonCase{path: ec.Path, tool: tl, line: strings.Fields(strings.TrimPrefix(ec.Path, "evo ")), args: map[string]any{}}
		if ec.Tool == "destroy" {
			// destroy without vm_id: the deployment of the directory, and
			// a directory without one.
			nothing := jc
			nothing.state = "no deployment"
			jc.state, jc.setup = "deployment recorded", recordDeployment
			cases = append(cases, nothing)
		}
		cases = append(cases, jc)
	}
	return cases, withCommand, bad
}

// engineLine is the command line of the engine command ec for the tool
// arguments args: each argument as the flag of ec that gives it.
func engineLine(t *testing.T, ec engineCommand, args map[string]any) []string {
	t.Helper()
	line := strings.Fields(strings.TrimPrefix(ec.Path, "evo "))
	flagOf := map[string]string{}
	for flag, arg := range ec.Flags {
		flagOf[arg] = flag
	}
	for _, k := range sortedKeys(args) {
		flag, ok := flagOf[k]
		switch {
		case !ok:
			t.Fatalf("%s: argument %s has no flag", ec.Path, k)
		case flag == "no-refresh": // refresh=false
			if args[k] == false {
				line = append(line, "--no-refresh")
			}
		default:
			line = append(line, "--"+flag+"="+flagText(args[k]))
		}
	}
	return line
}

// jsonRun is what the command of a case printed and what its tool
// answered, each run in the world as the case's setup leaves it.
type jsonRun struct {
	code        int
	out, errOut string
	text        string
	isErr       bool
	// cliWrites, toolWrites: what each sent the cloud that is not a read
	// (a preview reads only), with the bodies.
	cliWrites, toolWrites []string
}

// runJSONCase runs the command of c with --json --yes, then calls its
// tool with the same arguments and confirmed=true, the fake cloud put back
// before each.
func runJSONCase(t *testing.T, c jsonCase, w parityWorld, fake *jsonFake) jsonRun {
	t.Helper()
	prepare := func() (map[string]any, func()) {
		args := copyArgs(c.args)
		if c.setup == nil {
			return args, func() {}
		}
		extra, done := c.setup(t, w)
		for k, v := range extra {
			args[k] = v
		}
		return args, done
	}
	reset, takeWrites := fake.reset, func() []string {
		ws := fake.takeWrites()
		for i := range ws {
			ws[i] = sameVolatile(ws[i])
		}
		return ws
	}
	var r jsonRun
	args, done := prepare()
	line := c.line
	if line == nil {
		cmd := commandsByTool()[c.tool.Name]
		line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
	}
	reset()
	newProcess()
	r.code, r.out, r.errOut = runCLI(t, "", append([]string{"--json", "--yes"}, line...)...)
	r.cliWrites = takeWrites()
	done()
	_ = os.Remove(state.DefaultPath(w.dir))

	args, done = prepare()
	args["confirmed"] = true
	reset()
	newProcess()
	flagJSON, flagDryRun, flagYes = false, false, false
	tl, _ := newRegistry().Lookup(c.tool.Name)
	r.text, r.isErr = tl.Handler(context.Background(), args)
	r.toolWrites = takeWrites()
	done()
	// What the command or the tool recorded (deploy writes the state) is
	// not the next case's.
	_ = os.Remove(state.DefaultPath(w.dir))
	r.out, r.text = sameVolatile(r.out), sameVolatile(r.text)
	return r
}

// planID is the id deploy_plan gives each plan: two calls make two plans;
// stamp is a time of the call (expires_at of a plan).
var (
	planID = regexp.MustCompile(`plan-[0-9a-f]{12,}`)
	stamp  = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
)

// sameVolatile reads the ids and the times each call makes anew as one.
func sameVolatile(s string) string {
	return stamp.ReplaceAllString(planID.ReplaceAllString(sameJobID(s), "plan-<id>"), "<time>")
}

func TestParity_JSONEqualsTool(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, withCommand, bad := jsonCases(t)
	compat := 0
	seen, logged := map[string]bool{}, map[string]bool{}
	for _, c := range cases {
		r := runJSONCase(t, c, w, fake)
		seen[c.path] = true
		name := c.path
		if c.state != "" {
			name += " (" + c.state + ")"
		}
		what := fmt.Sprintf("%s (%s): exit %d out=%s err=%s; tool: %s", name, c.tool.Name, r.code, clipLine(r.out), clipLine(r.errOut), clipLine(r.text))
		switch {
		case r.isErr:
			bad = append(bad, "the tool fails on the fake cloud (give the fake what it needs): "+what)
			continue
		case r.code != 0:
			bad = append(bad, "the command fails where the tool does not: "+what)
			continue
		}
		if !reflect.DeepEqual(r.cliWrites, r.toolWrites) {
			// The same input makes the same change: what the command and
			// the tool send the cloud, beyond the reads of a preview.
			bad = append(bad, fmt.Sprintf("%s: the command and the tool write other things to the cloud:\n  command: %s\n  tool:    %s",
				name, clipLine(strings.Join(r.cliWrites, " | ")), clipLine(strings.Join(r.toolWrites, " | "))))
		}
		equal := r.out == r.text+"\n"
		if textAnswer[c.tool.Name] != "" {
			equal = r.out == r.text
		}
		if ca, ok := jsonAliasCompat[c.path]; ok && ca.check != nil {
			switch why := ca.check(r.out, r.errOut, r.text); {
			case equal:
				bad = append(bad, c.path+": in jsonAliasCompat, but --json is the tool's answer: drop the entry")
			case why != "":
				bad = append(bad, c.path+": not the form jsonAliasCompat gives ("+why+"): "+what)
			default:
				compat++
				if !logged[c.path] {
					t.Logf("%s: %s", c.path, ca.reason)
					logged[c.path] = true
				}
			}
			continue
		}
		if !equal {
			bad = append(bad, "--json is not the tool's answer: "+what+"\n  "+jsonDiff(r.out, r.text))
		}
	}
	for path, ca := range jsonAliasCompat {
		if !seen[path] {
			bad = append(bad, path+": in jsonAliasCompat, but no command of a tool")
		}
		if ca.reason == "" || ca.check == nil && ca.onError == nil {
			bad = append(bad, path+": an entry of jsonAliasCompat needs a reason and a check")
		}
	}
	t.Logf("tools with a command: %d; commands compared: %d, of them in jsonAliasCompat: %d", withCommand, len(cases), compat)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// jsonDiff names the keys whose values differ between two JSON objects.
func jsonDiff(a, b string) string {
	var am, bm map[string]any
	if json.Unmarshal([]byte(a), &am) != nil || json.Unmarshal([]byte(b), &bm) != nil {
		return "not two JSON objects"
	}
	var keys []string
	for k := range am {
		if _, ok := bm[k]; !ok {
			keys = append(keys, k+" (command only)")
		} else if !reflect.DeepEqual(am[k], bm[k]) {
			keys = append(keys, k)
		}
	}
	for k := range bm {
		if _, ok := am[k]; !ok {
			keys = append(keys, k+" (tool only)")
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "the same values, other bytes"
	}
	return "keys: " + strings.Join(keys, ", ")
}

func copyArgs(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// The errors of a command are the errors of its tool (spec.md REQ-8): for
// every command of TestParity_JSONEqualsTool, with the same arguments, on
// a cloud whose every API fails the same way — HTTP 404, HTTP 500, 403, no
// answer at all — the command fails exactly when the tool does, the reason
// of its stderr envelope ({"error":{"reason":…}}) is the reason of the
// tool's answer, and its exit code is the code docs/exit-codes.md gives
// that reason (the code of the envelope too). A command that reads no API
// (status without a deployment, deploy list) succeeds on both.
func TestParity_ErrorReasonEqualsTool(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	faults := []struct {
		name string
		fail func(*http.Request) (*http.Response, error)
	}{
		{"HTTP 404", httpFault(404, `{"message":"not found","code":"not_found"}`)},
		// Object Storage answers 400 here and below: minio repeats a 5xx
		// and a failed connection for ~8 s.
		{"HTTP 500", func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "s3.cloud.ru" {
				return s3BadRequest(r)
			}
			return httpFault(500, `{"message":"internal error"}`)(r)
		}},
		{"HTTP 403", httpFault(403, `{"message":"forbidden","code":"permission_denied"}`)},
		// No answer: a certificate the system does not trust (a proxy that
		// inspects TLS), which no client repeats — a refused connection
		// is, with waits of seconds.
		{"no answer", func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "s3.cloud.ru" {
				return s3BadRequest(r)
			}
			return nil, &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}
		}},
	}
	reasons := map[string]int{}
	compat := map[string]bool{}
	failed, succeeded := 0, 0
	for _, f := range faults {
		w.rt.extra = func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				return fake.answer(r), nil // the kube token: the call fails at the cluster
			}
			return f.fail(r)
		}
		for _, c := range cases {
			name := c.path
			if c.state != "" {
				name += " (" + c.state + ")"
			}
			r := runJSONCase(t, c, w, fake)
			env, failedCLI := errorEnvelope(r.errOut)
			reason, _ := parseObject(r.text)["reason"].(string)
			what := fmt.Sprintf("%s, %s: exit %d err=%s; tool: %s", name, f.name, r.code, clipLine(r.errOut), clipLine(r.text))
			if ca, ok := jsonAliasCompat[c.path]; ok && ca.onError != nil && r.isErr {
				if why, applies := ca.onError(r.code, r.out, r.errOut, r.text); applies {
					if why != "" {
						bad = append(bad, c.path+": not what jsonAliasCompat gives where the tool fails ("+why+"): "+what)
					}
					compat[c.path] = true
					continue
				}
			}
			switch {
			case !r.isErr && r.code == 0:
				succeeded++
				continue
			case !r.isErr:
				bad = append(bad, "the command fails where the tool does not: "+what)
			case r.code == 0 || !failedCLI:
				bad = append(bad, "the tool fails where the command does not: "+what)
			case env.Reason != reason:
				bad = append(bad, fmt.Sprintf("reason %s ≠ the tool's %s: %s", env.Reason, reason, what))
			case r.code != clerrors.ExitCode(clerrors.New(reason, "", "")) || env.Code != r.code:
				bad = append(bad, fmt.Sprintf("exit %d (envelope %d), but %s exits %d (docs/exit-codes.md): %s",
					r.code, env.Code, reason, clerrors.ExitCode(clerrors.New(reason, "", "")), what))
			default:
				failed++
				reasons[reason]++
			}
		}
	}
	var counts []string
	for _, r := range sortedKeysInt(reasons) {
		counts = append(counts, fmt.Sprintf("%s %d", r, reasons[r]))
	}
	t.Logf("commands: %d, faults: %d; failed alike: %d (%s); succeeded on both: %d", len(cases), len(faults), failed, strings.Join(counts, ", "), succeeded)
	for path, ca := range jsonAliasCompat {
		if ca.onError != nil && !compat[path] {
			bad = append(bad, path+": jsonAliasCompat says what it gives where its tool fails, but the tool never failed")
		} else if compat[path] {
			t.Logf("%s: %s", path, ca.reason)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// httpFault answers every request with status and body.
func httpFault(status int, body string) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
		return w.Result(), nil
	}
}

// s3BadRequest is the 400 of Object Storage.
func s3BadRequest(r *http.Request) (*http.Response, error) {
	return httpFault(400, `<Error><Code>BadRequest</Code><Message>bad request</Message></Error>`)(r)
}

// cliError is the error envelope of a command with --json.
type cliError struct {
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

// errorEnvelope is the last {"error":…} line of stderr.
func errorEnvelope(stderr string) (cliError, bool) {
	var out cliError
	found := false
	for _, line := range strings.Split(stderr, "\n") {
		var env struct {
			Error *cliError `json:"error"`
		}
		if json.Unmarshal([]byte(line), &env) == nil && env.Error != nil {
			out, found = *env.Error, true
		}
	}
	return out, found
}

func sortedKeysInt(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
