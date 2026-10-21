// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBreak12_B12_6_TerminalSecretTrimSpace: a secret from a file or from
// a pipe loses one trailing newline and nothing else. The same secret typed
// at a terminal (--password-file -) goes through askSecret, which
// TrimSpaces it, so a password that starts or ends with a space is not the
// argument the literal flag or the file would give.
func TestBreak12_B12_6_TerminalSecretTrimSpace(t *testing.T) {
	const secret = "  s3cret-from-tty  "
	oldTTY, oldPass, oldSave := stdinIsTerminal, readPassword, saveTerminal
	t.Cleanup(func() { stdinIsTerminal, readPassword, saveTerminal = oldTTY, oldPass, oldSave })
	stdinIsTerminal = func() bool { return true }
	saveTerminal = func() func() { return func() {} }
	readPassword = func() ([]byte, error) { return []byte(secret), nil }

	cmd, c, tl := findLeaf(t, "pg", "user-create")
	cmd.SetErr(&strings.Builder{})
	if err := cmd.ParseFlags([]string{"c-1", "app", "--password-file", "-"}); err != nil {
		t.Fatal(err)
	}
	got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
	if err != nil {
		t.Fatal(err)
	}
	if got["password"] != secret {
		t.Fatalf("terminal --password-file -: password = %q, want %q (one trailing newline trimmed, spaces kept, as a file gives)", got["password"], secret)
	}
}

// TestBreak12_B12_7_SecretFileBOM: a file written by an editor may start
// with a UTF-8 BOM. trimNewline drops one trailing newline and leaves the
// BOM, so the secret the tool receives is not the secret in the file.
// The same reader serves --value-file, --password-file and --private-key-file.
func TestBreak12_B12_7_SecretFileBOM(t *testing.T) {
	const secret = "s3cret-value"
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("\uFEFF"+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		group, verb, arg string
		argv             []string
	}{
		{"secret", "create", "value", []string{"app/db", "--value-file", path}},
		{"pg", "user-create", "password", []string{"c-1", "app", "--password-file", path}},
		{"cert", "import", "private_key", []string{"byo", "--server-certificate", "Y2VydA==", "--private-key-file", path}},
	}
	for _, tc := range cases {
		cmd, c, tl := findLeaf(t, tc.group, tc.verb)
		if err := cmd.ParseFlags(tc.argv); err != nil {
			t.Fatal(err)
		}
		got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
		if err != nil {
			t.Fatalf("%s %s: %v", tc.group, tc.verb, err)
		}
		if got[tc.arg] != secret {
			t.Errorf("%s %s: %s = %q, want %q", tc.group, tc.verb, tc.arg, got[tc.arg], secret)
		}
	}
}
