// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// localJSONCase is how TestContract_LocalCommandsAnswerJSON runs a command
// of localCommands: the arguments after the path, stdin, and whether the
// call succeeds in the test's world (a case that is meant to fail checks
// the form of the error; one that succeeds, the form of the answer).
type localJSONCase struct {
	args  []string
	stdin string
	ok    bool
}

// localJSONCases: one case for each command of localCommands that answers
// a call, in the order they run (diag on before diag status).
var localJSONCases = map[string]localJSONCase{
	"evo configure":        {args: []string{"--key-id", fakeKeyID, "--secret-stdin", "--project-id", localJSONProject}, stdin: fakeSecret, ok: true},
	"evo configure list":   {ok: true},
	"evo configure delete": {args: []string{"other"}, ok: true},
	"evo project use":      {args: []string{localJSONProject}, ok: true},
	"evo init":             {args: []string{"--non-interactive"}, ok: true},
	// No deployment recorded: the error is the form checked; the TCP
	// checks of a recorded one need a host (engine tests).
	"evo verify":  {ok: false},
	"evo version": {ok: true},
	// The test's transport answers the probe 200.
	"evo mcp healthcheck": {ok: true},
	"evo tools list":      {ok: true},
	"evo tools describe":  {args: []string{"vm_get"}, ok: true},
	"evo tools call":      {args: []string{"list_zones"}, ok: true},
	"evo agent setup":     {args: []string{"--client", "claude-code", "--yes"}, ok: true},
	"evo agent skill":     {ok: true},
	"evo diag on":         {ok: true},
	"evo diag status":     {ok: true},
	"evo diag off":        {ok: true},
	// The journal of the test's calls is empty: the error is the form.
	"evo diag export": {ok: false},
	"evo crew":        {ok: true},
}

// localJSONExempt are the commands whose --json output is not an answer,
// and why: a group without a verb and help print cobra's help, a completion
// script is shell code for eval, the server speaks MCP on stdio.
var localJSONExempt = map[string]string{
	"evo project":               "a group: prints its help",
	"evo tools":                 "a group: prints its help",
	"evo agent":                 "a group: prints its help",
	"evo diag":                  "a group: prints its help",
	"evo completion":            "a group: prints its help",
	"evo help":                  "cobra's help",
	"evo completion bash":       "a shell script for eval",
	"evo completion zsh":        "a shell script for eval",
	"evo completion fish":       "a shell script for eval",
	"evo completion powershell": "a shell script for eval",
	"evo mcp":                   "the MCP server: JSON-RPC on stdio, not a command's answer",
}

// localJSONMisses are the commands known to answer --json otherwise: path
// → the work item of the release plan that fixes it. The list only
// shrinks: a miss not here fails the test, and so does a line whose
// command answers JSON.
var localJSONMisses = map[string]string{
	"evo init":            "release plan 1.6, «Три формы одним PR» (Ц-9 агентного аудита rc.1): «разработка»",
	"evo crew":            "release plan 1.6, «Линия CLI» — свой вывод у всех команд: «разработка»",
	"evo mcp healthcheck": "release plan 1.6, «Линия CLI» — свой вывод у всех команд: «разработка»",
}

const localJSONProject = "00000000-0000-4000-8000-0000000000c1"

// Ц-9 of the agent's audit of rc.1: `evo init --json` succeeded with an
// empty stdout, and the agent did not know what it chose. Every command of
// localCommands is run with --json: one that succeeds prints one JSON
// document on stdout, one that fails the {"error":…} envelope on stderr,
// and neither prints text (the tree commands — TestContract_TreeJSONPurity,
// the commands of a tool — TestParity_JSONEqualsTool).
func TestContract_LocalCommandsAnswerJSON(t *testing.T) {
	setupCLI(t)
	oldRT, oldProxy := http.DefaultTransport, clerrors.ProxyFunc
	http.DefaultTransport = localJSONCloud{}
	clerrors.ProxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
	t.Cleanup(func() { http.DefaultTransport, clerrors.ProxyFunc = oldRT, oldProxy })
	saveProfile(t, "default", localJSONProject)
	saveProfile(t, "other", localJSONProject)
	writeFile(t, "docker-compose.yml", "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n")

	found := map[string]string{}
	for _, lc := range localCommands {
		c, has := localJSONCases[lc.Path]
		_, exempt := localJSONExempt[lc.Path]
		switch {
		case has && exempt:
			t.Errorf("%s: both a case and exempt", lc.Path)
			continue
		case exempt:
			continue
		case !has:
			t.Errorf("%s: a command of localCommands without a case in localJSONCases or a reason in localJSONExempt", lc.Path)
			continue
		}
		args := append(append([]string{"--json"}, strings.Fields(strings.TrimPrefix(lc.Path, "evo "))...), c.args...)
		code, out, errOut := runCLI(t, c.stdin, args...)
		what := "exit " + strconv.Itoa(code) + " out=" + clipLine(out) + " err=" + clipLine(errOut)
		switch {
		case (code == 0) != c.ok:
			t.Errorf("%s: the case expects success=%v: %s", lc.Path, c.ok, what)
		case jsonImpure(out, errOut) != "":
			found[lc.Path] = "not JSON (" + jsonImpure(out, errOut) + "): " + what
		case code == 0 && strings.TrimSpace(out) == "":
			found[lc.Path] = "succeeds with an empty stdout: " + what
		case code != 0:
			if _, env := errorEnvelope(errOut); !env {
				found[lc.Path] = "fails without the error envelope: " + what
			}
		}
	}
	for path := range localJSONCases {
		if !slices.ContainsFunc(localCommands, func(lc localCommand) bool { return lc.Path == path }) {
			t.Errorf("localJSONCases names %s, which is not in localCommands", path)
		}
	}
	for path := range localJSONExempt {
		if !slices.ContainsFunc(localCommands, func(lc localCommand) bool { return lc.Path == path }) {
			t.Errorf("localJSONExempt names %s, which is not in localCommands", path)
		}
	}
	for path, why := range found {
		if localJSONMisses[path] == "" {
			t.Errorf("%s: %s — answer JSON, or, while the plan moves it, add it to localJSONMisses with the work item", path, why)
		}
	}
	for path := range localJSONMisses {
		if found[path] == "" {
			t.Errorf("localJSONMisses names %s, which answers JSON now: remove the line", path)
		}
	}
}

// localJSONCloud answers IAM with a token, Organization with the project of
// the test, and Compute with one open zone,
// its default subnet and one Ubuntu image: what init picks from; every
// other request gets an empty list.
type localJSONCloud struct{}

func (localJSONCloud) RoundTrip(r *http.Request) (*http.Response, error) {
	const zone = "00000000-0000-4000-8000-0000000000c2"
	body := `{"items":[],"total":0}`
	switch p := r.URL.Path; {
	case strings.HasSuffix(p, "/iam/token"), strings.HasSuffix(p, "/auth/token"):
		body = `{"access_token":"tok","expires_in":3600}`
	case strings.HasSuffix(p, "/v3/projects"):
		body = `{"projects":[{"id":"` + localJSONProject + `","name":"only","status":"active"}]}`
	case strings.Contains(p, "availability-zones"):
		body = `[{"id":"` + zone + `","name":"ru.AZ-1","enabled":true}]`
	case strings.HasSuffix(p, "/subnets"):
		body = `{"items":[{"id":"00000000-0000-4000-8000-0000000000c3","name":"Default_ru.AZ-1","default":true,` +
			`"availability_zone":{"id":"` + zone + `","name":"ru.AZ-1"}}],"total":1}`
	case strings.HasSuffix(p, "/images"):
		body = `{"items":[{"id":"00000000-0000-4000-8000-0000000000c4","name":"Ubuntu-24.04","status":"active"}],"total":1}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
