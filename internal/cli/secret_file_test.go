// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The secrets of the trees (CLISpec.SecretFiles, owner's decision Q10):
// --<flag>-file gives the tool the argument --<flag> gives, from a file or
// from stdin (a pipe or a terminal), the same bytes the tool (MCP) gets —
// a leading BOM and one trailing newline trimmed, spaces kept (secretText);
// one of the two flags, not both;
// and the secret is printed nowhere: not in the answer, the preview or an
// error.

// secretFileCmd is a command with a SecretFiles argument.
type secretFileCmd struct {
	tool tools.Tool
	arg  string   // the tool argument of the secret
	flag string   // its value flag, without "--"
	rest []string // the command line with the other arguments of parityArgs
}

func secretFileCmds(t *testing.T) []secretFileCmd {
	t.Helper()
	reg := newRegistry()
	var out []secretFileCmd
	for name, tc := range commandsByTool() {
		for arg := range tc.cmd.SecretFiles {
			tl, _ := reg.Lookup(name)
			args := copyArgs(parityArgs[name])
			delete(args, arg)
			out = append(out, secretFileCmd{tool: tl, arg: arg, flag: tc.cmd.flagName(arg),
				rest: parityCommandOf(t, tc.group, tc.cmd, tl, args).tree})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].tool.Name < out[j].tool.Name })
	var got []string
	for _, sc := range out {
		got = append(got, sc.tool.Name+"."+sc.arg)
	}
	want := []string{"cert_import.private_key", "pg_user_create.password", "secret_add_version.value", "secret_create.value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretFiles: %v, want %v", got, want)
	}
	return out
}

func TestSecretFiles_SameArgumentAsValueFlag(t *testing.T) {
	w, fake := withJSONWorld(t)
	oldTTY, oldPass, oldSave := stdinIsTerminal, readPassword, saveTerminal
	t.Cleanup(func() { stdinIsTerminal, readPassword, saveTerminal = oldTTY, oldPass, oldSave })
	saveTerminal = func() func() { return func() {} }
	for _, sc := range secretFileCmds(t) {
		plain, _ := parityArgs[sc.tool.Name][sc.arg].(string)
		// Spaces around the secret are part of it, whichever way it comes:
		// the tool (MCP) gets them, and so must every command line (B12-6).
		secret := "  " + plain + " "
		args := copyArgs(parityArgs[sc.tool.Name])
		args[sc.arg] = secret
		file := func(content string) string {
			path := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}
		fileFlag := "--" + secretFileFlag(sc.flag)
		for _, v := range []struct {
			name  string
			flags []string
			stdin string
			tty   bool // stdin is a terminal: the line typed without echo
		}{
			{"literal --" + sc.flag, []string{"--" + sc.flag, secret}, "", false},
			{"file", []string{fileFlag, file(secret + "\n")}, "", false},
			{"file with CRLF", []string{fileFlag, file(secret + "\r\n")}, "", false},
			{"file with a BOM", []string{fileFlag, file(utf8BOM + secret + "\r\n")}, "", false}, // B12-7
			{"stdin", []string{fileFlag, "-"}, secret + "\n", false},
			{"stdin with a BOM", []string{fileFlag, "-"}, utf8BOM + secret + "\n", false},
			{"terminal", []string{fileFlag, "-"}, "", true},
		} {
			name := "evo " + strings.Join(sc.rest[:2], " ") + " " + v.flags[0] + " (" + v.name + ")"
			line := append(append([]string{}, sc.rest...), v.flags...)
			stdinIsTerminal = func() bool { return v.tty }
			readPassword = func() ([]byte, error) { return []byte(secret), nil }
			r := runJSONCase(t, jsonCase{path: name, tool: sc.tool, line: line, args: args,
				stdin: v.stdin, setup: paritySetup[sc.tool.Name]}, w, fake)
			switch {
			case r.isErr || r.code != 0:
				t.Errorf("%s: exit %d, tool error %v:\n%s\n%s", name, r.code, r.isErr, r.out, r.errOut)
			case r.out != r.text+"\n":
				t.Errorf("%s: --json is not the tool's answer to --%s:\n%s\n%s", name, sc.flag, r.out, r.text)
			case len(r.toolWrites) == 0 || !reflect.DeepEqual(r.cliWrites, r.toolWrites):
				t.Errorf("%s: the command and the tool with --%s write other things:\n  command: %s\n  tool:    %s",
					name, sc.flag, strings.Join(r.cliWrites, " | "), strings.Join(r.toolWrites, " | "))
			}
			if strings.Contains(r.out+r.errOut, plain) {
				t.Errorf("%s: the secret is printed:\n%s\n%s", name, r.out, r.errOut)
			}
		}
		stdinIsTerminal = func() bool { return false }

		// The preview: --dry-run, and --json without --yes.
		path := file(secret)
		for _, extra := range [][]string{{"--dry-run"}, {"--json"}} {
			newProcess()
			code, out, errOut := runCLI(t, "", append(append(extra, sc.rest...), "--"+secretFileFlag(sc.flag), path)...)
			if want := map[string]int{"--dry-run": clerrors.ExitOK, "--json": clerrors.ExitConfirmationRequired}[extra[0]]; code != want {
				t.Errorf("%s %v: exit %d, want %d:\n%s\n%s", sc.tool.Name, extra, code, want, out, errOut)
			}
			if strings.Contains(out+errOut, secret) {
				t.Errorf("%s %v: the secret is in the preview:\n%s\n%s", sc.tool.Name, extra, out, errOut)
			}
		}
	}
}

func TestSecretFiles_Refusals(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "user.pass")
	if err := os.WriteFile(secret, []byte("Parity-Pass-0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "nosuch.pass")
	empty := filepath.Join(t.TempDir(), "empty.pass") // printf '\n' > empty.pass
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bomOnly := filepath.Join(t.TempDir(), "bom.pass") // an editor's "empty" UTF-8 file
	if err := os.WriteFile(bomOnly, []byte(utf8BOM+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	argsOf := func(group, verb string, argv ...string) (map[string]any, error) {
		t.Helper()
		cmd, c, tl := findLeaf(t, group, verb)
		cmd.SetIn(strings.NewReader("\r\n"))
		if err := cmd.ParseFlags(argv); err != nil {
			t.Fatal(err)
		}
		return treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
	}
	for _, tc := range []struct {
		name        string
		group, verb string
		argv        []string
		message     string
		hint        string
	}{
		{"both", "pg", "user-create", []string{"c-1", "app", "--password", "x", "--password-file", secret},
			"--password and --password-file are both given", "--password-file reads it from a file or stdin"},
		{"neither", "pg", "user-create", []string{"c-1", "app"},
			"missing required flag(s): --password or --password-file", "--help"},
		{"neither, beside another", "cert", "import", []string{"byo"},
			"--private-key or --private-key-file", ""},
		{"no file", "secret", "create", []string{"app/db", "--value-file", missing},
			"--value-file " + missing + ": the file was not read: ", ""},
		// No value: a file or stdin with nothing but a newline (Fable
		// audit, REL12-9).
		{"empty file", "secret", "create", []string{"app/db", "--value-file", empty},
			"--value-file " + empty + ": the file is empty", ""},
		{"a BOM and a newline", "secret", "create", []string{"app/db", "--value-file", bomOnly},
			"--value-file " + bomOnly + ": the file is empty", ""},
		{"empty stdin", "pg", "user-create", []string{"c-1", "app", "--password-file", "-"},
			"--password-file -: stdin is empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := argsOf(tc.group, tc.verb, tc.argv...)
			var ce *clerrors.CLIError
			if !errors.As(err, &ce) || ce.Code != clerrors.CodeUsage {
				t.Fatalf("err = %v, want %s", err, clerrors.CodeUsage)
			}
			if !strings.Contains(ce.Message, tc.message) || !strings.Contains(ce.Hint, tc.hint) {
				t.Errorf("message %q, hint %q; want %q and %q", ce.Message, ce.Hint, tc.message, tc.hint)
			}
			if strings.Contains(ce.Message+ce.Hint, "Parity-Pass-0") {
				t.Errorf("the secret is in the error: %s", ce.Message)
			}
		})
	}

	// The exit code of every usage error, and the Russian texts.
	russian(t)
	_, errOut, code := execCLI(t, "pg", "user-create", "c-1", "app", "--password", "x", "--password-file", secret)
	if code != clerrors.ExitGeneral || !strings.Contains(errOut, "заданы оба флага: --password и --password-file") {
		t.Errorf("both: exit %d:\n%s", code, errOut)
	}
	_, errOut, code = execCLI(t, "pg", "user-create", "c-1", "app")
	if code != clerrors.ExitGeneral || !strings.Contains(errOut, "не заданы обязательные флаги: --password или --password-file") {
		t.Errorf("neither: exit %d:\n%s", code, errOut)
	}
	_, errOut, code = execCLI(t, "secret", "create", "app/db", "--value-file", empty)
	if code != clerrors.ExitGeneral || !strings.Contains(errOut, "--value-file "+empty+": файл пуст") {
		t.Errorf("empty file: exit %d:\n%s", code, errOut)
	}
}

// The help names the file flag beside the value flag, in both languages.
func TestSecretFiles_Help(t *testing.T) {
	out, _, code := execCLI(t, "pg", "user-create", "--help")
	for _, want := range []string{"--password string", "User password. Required. Or --password-file, safer: the value stays out of the command line and the shell history",
		"--password-file string", "--password from a file, its trailing newline trimmed ('-' reads stdin", "instead of --password"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("exit %d, no %q in:\n%s", code, want, out)
		}
	}
	russian(t)
	out, _, code = execCLI(t, "cert", "import", "--help")
	for _, want := range []string{"(обязательный). Или --private-key-file — безопаснее: значение не попадёт в командную строку и историю оболочки",
		"--private-key-file string", "--private-key из файла без завершающего перевода строки ('-' — из stdin", "вместо --private-key"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("exit %d, no %q in:\n%s", code, want, out)
		}
	}
}

// --<flag>-file - on a terminal: one line, not echoed, after a prompt that
// names the flag, as --meta field=- reads it.
func TestSecretFiles_StdinTerminalReadsWithoutEcho(t *testing.T) {
	oldTTY, oldPass, oldSave := stdinIsTerminal, readPassword, saveTerminal
	t.Cleanup(func() { stdinIsTerminal, readPassword, saveTerminal = oldTTY, oldPass, oldSave })
	stdinIsTerminal = func() bool { return true }
	saveTerminal = func() func() { return func() {} }
	readPassword = func() ([]byte, error) { return []byte("s3cret-from-tty"), nil }

	cmd, c, tl := findLeaf(t, "pg", "user-create")
	var prompt strings.Builder
	cmd.SetErr(&prompt)
	if err := cmd.ParseFlags([]string{"c-1", "app", "--password-file", "-"}); err != nil {
		t.Fatal(err)
	}
	got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
	if err != nil || got["password"] != "s3cret-from-tty" {
		t.Fatalf("args %v, err %v", got, err)
	}
	if p := prompt.String(); !strings.Contains(p, "--password") || strings.Contains(p, "s3cret") {
		t.Errorf("prompt = %q: must name the flag and not echo the value", p)
	}
}
