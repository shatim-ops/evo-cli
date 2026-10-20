// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testPubKey returns a fresh ed25519 public key in authorized_keys form.
func testPubKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment
}

func TestLoadUserKeys(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Missing file → no keys, no error.
	if got, _ := loadUserKeys(dir); got != nil {
		t.Fatalf("missing file: want nil, got %v", got)
	}

	k1, k2 := testPubKey(t, "user@mac"), testPubKey(t, "backup@key")
	// Before break r4 (R4-10) a line was taken by its prefix: the malformed
	// ssh-rsa line went to the VM, the option line did not. Now
	// ssh.ParseAuthorizedKey decides.
	content := "# user keys\n" + k1 + "\n\n" +
		"-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ-never-ship\n" +
		"-----END OPENSSH PRIVATE KEY-----\n" +
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ broken@key\n" +
		"garbage-that-must-never-ship\n" +
		`no-pty,command="echo hi" ` + k2 + "\n"
	if err := os.WriteFile(filepath.Join(keyDir, userKeysFile), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, file := loadUserKeys(dir)
	if file != userKeysFile {
		t.Errorf("keys read from %q, want %q", file, userKeysFile)
	}
	want := []string{k1, `no-pty,command="echo hi" ` + k2}
	if len(got) != len(want) {
		t.Fatalf("want %d keys, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d: want %q, got %q", i, want[i], got[i])
		}
	}

	// deploy_plan shows what is not sent, without the text of the line.
	keys, rejected, _ := loadUserKeyLines(dir)
	shown := describeUserKeys(keys, rejected)
	b, _ := json.Marshal(shown)
	out := string(b)
	for _, leak := range []string{"never-ship", "b3BlbnNzaC1rZXkt", "garbage"} {
		if strings.Contains(out, leak) {
			t.Errorf("deploy_plan view repeats %q: %s", leak, out)
		}
	}
	wantErr := []string{
		"lines 4-6: a PRIVATE key",
		"line 7: not a valid ssh-rsa public key",
		"line 8: not an authorized_keys entry",
	}
	if len(shown) != 2+len(wantErr) {
		t.Fatalf("describeUserKeys: %s", out)
	}
	if shown[1]["options"] == nil || !strings.Contains(out, `command=\"echo hi\"`) || shown[0]["options"] != nil {
		t.Errorf("options: %s", out)
	}
	for i, w := range wantErr {
		if e, _ := shown[2+i]["error"].(string); !strings.HasPrefix(e, w) || !strings.Contains(e, "not sent to the VM") {
			t.Errorf("entry %d: error %q, want %q…", 2+i, e, w)
		}
	}
}

// The script that appends keys on the VM: an entry with quotes, a single
// quote and a leading word that looks like a flag arrives byte for byte, and
// a second run adds nothing.
func TestUserKeysScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script runs on the Linux VM; here it needs a POSIX sh")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	keys := []string{
		`command="echo 'it''s' \"x\"; rm -rf /tmp/nope",no-agent-forwarding ` + testPubKey(t, "a@b"),
		`-e,--file=/etc/passwd ` + testPubKey(t, "dash@b"),
		testPubKey(t, "$(touch pwned) `id`"),
	}
	for _, k := range keys {
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k)); err != nil {
			t.Fatalf("fixture %q: %v", k, err)
		}
	}
	home := t.TempDir()
	for run := 0; run < 2; run++ {
		cmd := exec.Command(sh, "-c", userKeysScript(keys))
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %d: %v\n%s", run, err, out)
		}
	}
	got, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(keys, "\n") + "\n"; string(got) != want {
		t.Errorf("authorized_keys:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(home, "pwned")); err == nil {
		t.Error("the comment ran as a command")
	}
}

func TestShellQuoteMCP(t *testing.T) {
	if got := shellQuoteMCP(`k'ey`); got != `'k'\''ey'` {
		t.Errorf("quote: got %q", got)
	}
}

// Until v1.1 the pre-v1.0 file name is still read, so keys set up for the
// old engine keep reaching new VMs; the new name wins when both exist.
func TestLoadUserKeys_LegacyFile(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := testPubKey(t, "old@mac") + "\n"
	if err := os.WriteFile(filepath.Join(keyDir, legacyUserKeysFile), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if got, file := loadUserKeys(dir); len(got) != 1 || file != legacyUserKeysFile {
		t.Fatalf("legacy only: got %v from %q", got, file)
	}

	current := testPubKey(t, "new@mac") + "\n"
	if err := os.WriteFile(filepath.Join(keyDir, userKeysFile), []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	if got, file := loadUserKeys(dir); len(got) != 1 || got[0] != strings.TrimSpace(current) || file != userKeysFile {
		t.Fatalf("both files: got %v from %q, want the new file", got, file)
	}
}

// pinHostKey keeps one entry per host: the key of an earlier VM on the same
// reused IP goes, other hosts stay; an empty key only drops the old entry.
func TestPinHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	old := "203.0.113.5 ssh-ed25519 AAAAold\n[203.0.113.5]:22 ssh-rsa AAAAold2\n198.51.100.7,203.0.113.5 ssh-ed25519 AAAAboth\n198.51.100.7 ssh-ed25519 AAAAother\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pinHostKey(path, "203.0.113.5", "ssh-ed25519 AAAAnew"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "198.51.100.7 ssh-ed25519 AAAAother\n203.0.113.5 ssh-ed25519 AAAAnew\n"
	if string(got) != want {
		t.Fatalf("known_hosts:\n%s\nwant:\n%s", got, want)
	}
	if err := pinHostKey(path, "203.0.113.5", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "198.51.100.7 ssh-ed25519 AAAAother\n" {
		t.Fatalf("after drop:\n%s", got)
	}
	// IPv6: plain and bracketed with the port.
	v6 := "[2001:db8::1]:22 ssh-ed25519 AAAAold\n2001:db8::1 ssh-ed25519 AAAAold2\n"
	if err := os.WriteFile(path, []byte(v6), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pinHostKey(path, "2001:db8::1", "ssh-ed25519 AAAAnew"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "2001:db8::1 ssh-ed25519 AAAAnew\n" {
		t.Fatalf("IPv6:\n%s", got)
	}

	// No file yet and nothing to pin: an empty file, no error.
	fresh := filepath.Join(t.TempDir(), "known_hosts")
	if err := pinHostKey(fresh, "203.0.113.5", ""); err != nil {
		t.Fatal(err)
	}
}
