// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package agentconf

import (
	stderrors "errors"
	"strings"
	"testing"
)

// The golden files of every client and case are in internal/cli
// (TestAgentSetup_Golden); these are the edge cases of the formats.

var evo = Entry{{"command", "evo"}, {"args", []string{"mcp", "--toolsets", "vm"}}}

// owned are the keys evo writes in an entry, in either of its forms: the
// rest of an entry already there is the user's.
var owned = []string{"type", "command", "args", "url", "headers"}

func TestJSON(t *testing.T) {
	for _, tc := range []struct {
		name, old, want string
		comments        bool
		state           State
	}{
		{name: "empty", old: "", state: Added,
			want: "{\n  \"mcpServers\": {\n    \"evo\": {\n      \"command\": \"evo\",\n      \"args\": [\n        \"mcp\",\n        \"--toolsets\",\n        \"vm\"\n      ]\n    }\n  }\n}\n"},
		{name: "empty root object", old: "{}\n", state: Added,
			want: "{\n  \"mcpServers\": {\n    \"evo\": {\n      \"command\": \"evo\",\n      \"args\": [\n        \"mcp\",\n        \"--toolsets\",\n        \"vm\"\n      ]\n    }\n  }\n}\n"},
		{name: "one line keeps one line", old: `{"theme":"dark","mcpServers":{}}`, state: Added,
			want: `{"theme":"dark","mcpServers":{"evo":{"command":"evo","args":["mcp","--toolsets","vm"]}}}`},
		{name: "tabs and CRLF", old: "{\r\n\t\"mcpServers\": {\r\n\t\t\"x\": {\"command\": \"x\"}\r\n\t}\r\n}\r\n", state: Added,
			want: "{\r\n\t\"mcpServers\": {\r\n\t\t\"x\": {\"command\": \"x\"},\r\n\t\t\"evo\": {\r\n\t\t\t\"command\": \"evo\",\r\n\t\t\t\"args\": [\r\n\t\t\t\t\"mcp\",\r\n\t\t\t\t\"--toolsets\",\r\n\t\t\t\t\"vm\"\r\n\t\t\t]\r\n\t\t}\r\n\t}\r\n}\r\n"},
		{name: "same entry, other order and layout", old: `{"mcpServers": {"evo": {"args": ["mcp", "--toolsets", "vm"], "command": "evo"}}}`, state: Same,
			want: `{"mcpServers": {"evo": {"args": ["mcp", "--toolsets", "vm"], "command": "evo"}}}`},
		{name: "the user's keys do not count", state: Same,
			old:  `{"mcpServers": {"evo": {"command": "evo", "env": {"A": "1"}, "args": ["mcp", "--toolsets", "vm"], "timeout": 60}}}`,
			want: `{"mcpServers": {"evo": {"command": "evo", "env": {"A": "1"}, "args": ["mcp", "--toolsets", "vm"], "timeout": 60}}}`},
		{name: "replaced: evo's keys first, the user's kept, evo's not in the entry gone", state: Replaced,
			old:  "{\n  \"mcpServers\": {\n    \"evo\": {\n      \"type\": \"http\",\n      \"env\": {\"A\": \"1\"},\n      \"url\": \"http://x/mcp\",\n      \"timeout\": 60\n    }\n  }\n}\n",
			want: "{\n  \"mcpServers\": {\n    \"evo\": {\n      \"command\": \"evo\",\n      \"args\": [\n        \"mcp\",\n        \"--toolsets\",\n        \"vm\"\n      ],\n      \"env\": {\n        \"A\": \"1\"\n      },\n      \"timeout\": 60\n    }\n  }\n}\n"},
		{name: "replaced in JSONC: the user's keys kept", comments: true, state: Replaced,
			old:  "{\"servers\": {\"evo\": {\"command\": \"old\", /* mine */ \"disabled\": false,},},}",
			want: "{\"servers\": {\"evo\": {\"command\":\"evo\",\"args\":[\"mcp\",\"--toolsets\",\"vm\"],\"disabled\":false},},}"},
		{name: "JSONC with CRLF: a line comment keeps its line ending", comments: true, state: Added,
			old:  "{\r\n  \"servers\": {\r\n    \"x\": {\"command\": \"x\"} // mine\r\n  }\r\n}\r\n",
			want: "{\r\n  \"servers\": {\r\n    \"x\": {\"command\": \"x\"},\r\n    \"evo\": {\r\n      \"command\": \"evo\",\r\n      \"args\": [\r\n        \"mcp\",\r\n        \"--toolsets\",\r\n        \"vm\"\r\n      ]\r\n    } // mine\r\n  }\r\n}\r\n"},
		{name: "JSONC comments and trailing commas kept", comments: true, state: Added,
			old:  "{\n  // servers\n  \"servers\": {\n    \"x\": {\"command\": \"x\"}, /* last */\n  },\n}\n",
			want: "{\n  // servers\n  \"servers\": {\n    \"x\": {\"command\": \"x\"},\n    \"evo\": {\n      \"command\": \"evo\",\n      \"args\": [\n        \"mcp\",\n        \"--toolsets\",\n        \"vm\"\n      ]\n    }, /* last */\n  },\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			servers := "mcpServers"
			if tc.comments {
				servers = "servers"
			}
			r, err := JSON([]byte(tc.old), servers, "evo", evo, owned, tc.comments)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != tc.state || string(r.Data) != tc.want {
				t.Errorf("state %d, got:\n%s\nwant:\n%s", r.State, r.Data, tc.want)
			}
		})
	}
}

func TestJSON_Refuses(t *testing.T) {
	for _, tc := range []struct {
		name, old, want string
		comments        bool
		line            int
	}{
		{name: "syntax", old: "{\n  \"a\": 1,\n  \"b\" 2\n}\n", want: "invalid JSON", line: 3},
		{name: "comment in strict JSON", old: "{\n  // c\n  \"a\": 1\n}\n", want: "invalid JSON", line: 2},
		{name: "not an object", old: "[1]\n", want: "not a JSON object", line: 1},
		{name: "servers not an object", old: "{\"mcpServers\": [1]}", want: `"mcpServers" is not an object`, line: 1},
		{name: "duplicate entry", old: "{\"mcpServers\": {\n\"evo\": {},\n\"evo\": {}}}", want: `key "evo" appears twice`, line: 3},
		{name: "duplicate key of the entry", old: "{\"mcpServers\": {\"evo\": {\n\"env\": {},\n\"env\": {}}}}", want: `key "env" appears twice`, line: 3},
		{name: "unterminated comment", old: "{ /* x", comments: true, want: "unterminated /* comment", line: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := JSON([]byte(tc.old), "mcpServers", "evo", evo, owned, tc.comments)
			var fe *FileError
			if !stderrors.As(err, &fe) || !strings.Contains(fe.Msg, tc.want) || fe.Line != tc.line {
				t.Errorf("got %v (%+v), want %q at line %d", err, fe, tc.want, tc.line)
			}
		})
	}
}

var codexTable = []string{"mcp_servers", "evo"}

func TestTOML(t *testing.T) {
	block := "[mcp_servers.evo]\ncommand = \"evo\"\nargs = [\"mcp\", \"--toolsets\", \"vm\"]\n"
	for _, tc := range []struct {
		name, old, want string
		state           State
	}{
		{name: "empty", old: "\n", want: block, state: Added},
		{name: "no trailing newline", old: `model = "x"`, want: "model = \"x\"\n\n" + block, state: Added},
		{name: "after the last server, before other tables",
			old:   "model = \"x\"\n\n[mcp_servers.a]\ncommand = \"a\" # note\n\n# profiles\n[profiles.p]\nmodel = \"y\"\n",
			want:  "model = \"x\"\n\n[mcp_servers.a]\ncommand = \"a\" # note\n\n" + block + "\n# profiles\n[profiles.p]\nmodel = \"y\"\n",
			state: Added},
		{name: "same, other quoting and layout",
			old:   "[mcp_servers.evo]\nargs = [\n  'mcp', # flags\n  \"--toolsets\", \"vm\",\n]\n\"command\" = '''evo'''\n",
			want:  "[mcp_servers.evo]\nargs = [\n  'mcp', # flags\n  \"--toolsets\", \"vm\",\n]\n\"command\" = '''evo'''\n",
			state: Same},
		{name: "replaced: evo's keys first, the user's keys and sub-table kept, comments around kept",
			old:   "# top\n[mcp_servers.evo]\ncommand = \"old\" # mine\nstartup_timeout_sec = 30\nurl = \"http://x\"\n\n[mcp_servers.evo.env]\nA = \"1\"\n\n# tail\n[other]\nk = 1979-05-27 07:32:00Z\n",
			want:  "# top\n" + block + "startup_timeout_sec = 30\n\n[mcp_servers.evo.env]\nA = \"1\"\n\n# tail\n[other]\nk = 1979-05-27 07:32:00Z\n",
			state: Replaced},
		{name: "replaced: a sub-table of evo's key goes",
			old:   "[mcp_servers.evo]\nurl = \"http://x\"\n\n[mcp_servers.evo.headers]\nAuthorization = \"Bearer x\"\n",
			want:  block,
			state: Replaced},
		{name: "replaced: the sub-table before the table stays",
			old:   "[mcp_servers.evo.env]\nA = \"1\"\n\n[mcp_servers.evo]\ncommand = \"old\"\n",
			want:  "[mcp_servers.evo.env]\nA = \"1\"\n\n" + block,
			state: Replaced},
		{name: "only a sub-table: the table goes before it",
			old:   "x = 1\n[mcp_servers.evo.env]\nA = \"1\"\n",
			want:  "x = 1\n" + block + "[mcp_servers.evo.env]\nA = \"1\"\n",
			state: Added},
		{name: "only sub-tables of evo's keys: they go, the table comes",
			old:   "x = 1\n\n[mcp_servers.evo.headers]\nAuthorization = \"x\"\n",
			want:  "x = 1\n" + block,
			state: Replaced},
		{name: "the user's keys and sub-tables do not count",
			old:   "[mcp_servers.evo]\ncommand = \"evo\"\nstartup_timeout_sec = 30\nargs = [\"mcp\", \"--toolsets\", \"vm\"]\n[mcp_servers.evo.env]\nA = \"1\"\n",
			want:  "[mcp_servers.evo]\ncommand = \"evo\"\nstartup_timeout_sec = 30\nargs = [\"mcp\", \"--toolsets\", \"vm\"]\n[mcp_servers.evo.env]\nA = \"1\"\n",
			state: Same},
		{name: "multi-line strings that look like headers",
			old:   "s = \"\"\"\n[mcp_servers.evo]\n\"\"\"\nt = '''\n[x]'''\n",
			want:  "s = \"\"\"\n[mcp_servers.evo]\n\"\"\"\nt = '''\n[x]'''\n\n" + block,
			state: Added},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := TOML([]byte(tc.old), codexTable, evo, owned)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != tc.state || string(r.Data) != tc.want {
				t.Errorf("state %d, got:\n%s\nwant:\n%s", r.State, r.Data, tc.want)
			}
		})
	}
}

func TestTOML_Refuses(t *testing.T) {
	for _, tc := range []struct {
		name, old, want string
		line            int
	}{
		{name: "unterminated string", old: "a = 1\nb = \"x\n", want: "line break in a string", line: 2},
		{name: "unterminated multi-line string", old: "a = '''x\n\n", want: "unterminated string", line: 1},
		{name: "bad header", old: "[a\n", want: `expected "]"`, line: 1},
		{name: "garbage after value", old: "a = 1 2\n", want: "expected the end of the line", line: 1},
		{name: "invalid escape", old: "a = \"\\q\"\n", want: "invalid escape", line: 1},
		{name: "dotted keys", old: "[mcp_servers]\nevo.command = \"evo\"\n", want: "dotted key or an inline table", line: 2},
		{name: "inline table", old: "[mcp_servers]\nevo = { command = \"evo\" }\n", want: "dotted key or an inline table", line: 2},
		{name: "servers as a value", old: "mcp_servers = {}\n", want: "is set as a value", line: 1},
		{name: "array of tables", old: "[[mcp_servers]]\nx = 1\n", want: "array of tables", line: 1},
		{name: "twice", old: "[mcp_servers.evo]\n[mcp_servers.evo]\n", want: "appears twice", line: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TOML([]byte(tc.old), codexTable, evo, owned)
			var fe *FileError
			if !stderrors.As(err, &fe) || !strings.Contains(fe.Msg, tc.want) || fe.Line != tc.line {
				t.Errorf("got %v (%+v), want %q at line %d", err, fe, tc.want, tc.line)
			}
		})
	}
}

func TestSection(t *testing.T) {
	body := "rule 1\n"
	sec := sectionBegin + "\nrule 1\n" + sectionEnd + "\n"
	for _, tc := range []struct {
		name, old, want string
		state           State
	}{
		{name: "new file", old: "", want: sec, state: Added},
		{name: "appended after a blank line", old: "# Mine\n", want: "# Mine\n\n" + sec, state: Added},
		{name: "no final newline", old: "# Mine", want: "# Mine\n\n" + sec, state: Added},
		{name: "same", old: "a\n" + sec + "b\n", want: "a\n" + sec + "b\n", state: Same},
		{name: "replaced, text around kept", old: "a\n" + SectionBeginPrefix + " old -->\nrule 0\n" + sectionEnd + "\nb\n", want: "a\n" + sec + "b\n", state: Replaced},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Section([]byte(tc.old), body)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != tc.state || string(r.Data) != tc.want {
				t.Errorf("state %d, got:\n%q\nwant:\n%q", r.State, r.Data, tc.want)
			}
		})
	}
	for _, old := range []string{"a\n" + sectionEnd + "\n", sectionBegin + "\n", sec + sec, sectionEnd + "\n" + sectionBegin + "\n"} {
		var fe *FileError
		if _, err := Section([]byte(old), body); !stderrors.As(err, &fe) || fe.Line == 0 {
			t.Errorf("%q: got %v, want a FileError with a line", old, err)
		}
	}
}

func TestWhole(t *testing.T) {
	if r := Whole(nil, []byte("x\n")); r.State != Added {
		t.Errorf("absent: %d", r.State)
	}
	if r := Whole([]byte("x\r\n"), []byte("x\n")); r.State != Same || string(r.Data) != "x\r\n" {
		t.Errorf("same: %d %q", r.State, r.Data)
	}
	if r := Whole([]byte("y\n"), []byte("x\n")); r.State != Replaced || string(r.Data) != "x\n" {
		t.Errorf("other: %d %q", r.State, r.Data)
	}
}

func TestDiff(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n"
	got := strings.Join(Diff("f", []byte(old), []byte("1\n2\n3\n4\nX\n6\n7\n8\n")), "\n")
	want := "--- f\n+++ f\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+X\n 6\n 7\n 8"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = strings.Join(Diff("f", nil, []byte("a\n")), "\n")
	if want := "--- /dev/null\n+++ f\n@@ -0,0 +1,1 @@\n+a"; got != want {
		t.Errorf("new file: got:\n%s\nwant:\n%s", got, want)
	}
	if d := Diff("f", []byte(old), []byte(old)); d != nil {
		t.Errorf("equal: %q", d)
	}
}
