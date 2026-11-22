// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package repolayout

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeEvo answers what install.sh asks of the binary it installed.
const fakeEvo = `#!/bin/sh
case "$1" in
completion) echo "# evo completion for $2" ;;
version) echo "evo 9.9.9" ;;
esac
`

// fakeRelease writes a release v9.9.9 of this host's platform under dir:
// the archive with evo and cloudru-vm and its checksums.txt.
func fakeRelease(t *testing.T, dir string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"evo", "cloudru-vm"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(fakeEvo))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(fakeEvo)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := "evo_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	rel := filepath.Join(dir, "v9.9.9")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	writeFile(t, filepath.Join(rel, archive), buf.String(), 0o644)
	writeFile(t, filepath.Join(rel, "checksums.txt"), hex.EncodeToString(sum[:])+"  "+archive+"\n", 0o644)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// Release plan 1.5, «Установка автодополнения» (owner's decision 28.10):
// install.sh puts the completion where each shell finds it without a
// change to its configuration — fish, bash (bash-completion), zsh into a
// directory of $fpath in the home — writes no shell's rc file, overwrites
// only its own files on a second run, and --uninstall removes the
// binaries and those files, nothing else.
func TestInstallSh_CompletionFilesAndUninstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	for _, tool := range []string{"sh", "tar", "curl", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s here", tool)
		}
	}
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	rel := filepath.Join(tmp, "local release")
	releaseURL := url.URL{Scheme: "file", Path: rel}
	fakeRelease(t, rel)
	// fish is found by its configuration directory; zsh is a fake whose
	// $fpath holds ~/.zfunc (a directory of the home it may write).
	// The fish file goes in with no fish binary on PATH: this pins the
	// behaviour as it is, not as it should be — a completion for a shell
	// that is not installed (#2038). The fix and this expectation change
	// together, candidate 1.10 (release plan 1.9, Astra #2103).
	if err := os.MkdirAll(filepath.Join(home, ".config", "fish"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".zfunc"), 0o755); err != nil {
		t.Fatal(err)
	}
	shims := filepath.Join(tmp, "shims")
	writeFile(t, filepath.Join(shims, "zsh"), "#!/bin/sh\necho \"$HOME/.zfunc\"\n", 0o755)
	foreign := filepath.Join(home, ".config", "fish", "completions", "other.fish")
	writeFile(t, foreign, "# not evo's\n", 0o644)

	run := func(args ...string) string {
		t.Helper()
		// umask 0: the modes of the files are install.sh's, not the umask's.
		cmd := exec.Command("sh", "-c", `umask 0; exec sh "$0" "$@"`, filepath.Join("..", "..", "install.sh"))
		cmd.Args = append(cmd.Args, args...)
		cmd.Env = append(os.Environ(),
			"HOME="+home, "XDG_DATA_HOME=", "XDG_CONFIG_HOME=",
			"INSTALL_DIR="+filepath.Join(tmp, "bin"),
			"CLOUDRU_VM_VERSION=v9.9.9", "CLOUDRU_VM_BASE_URL="+releaseURL.String(),
			"PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("install.sh %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	files := map[string]string{
		filepath.Join(home, ".config", "fish", "completions", "evo.fish"):               "fish",
		filepath.Join(home, ".local", "share", "bash-completion", "completions", "evo"): "bash",
		filepath.Join(home, ".zfunc", "_evo"):                                           "zsh",
	}
	for i := 0; i < 2; i++ { // a second run overwrites its own files
		out := run()
		for path, sh := range files {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "# evo completion for "+sh+"\n" {
				t.Errorf("run %d: %s: %q, %v — want evo's %s completion\n%s", i+1, path, got, err, sh, out)
			}
			// 0644 whatever the umask: nobody else writes what the shell runs.
			if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0o644 {
				t.Errorf("run %d: %s: mode %v, want 0644", i+1, path, fi.Mode().Perm())
			}
		}
	}
	for _, rc := range []string{".zshrc", ".bashrc", ".bash_profile", ".profile", filepath.Join(".config", "fish", "config.fish")} {
		if _, err := os.Stat(filepath.Join(home, rc)); err == nil {
			t.Errorf("install.sh wrote %s: a shell's configuration is not changed unasked", rc)
		}
	}

	out := run("--uninstall")
	for _, path := range append([]string{filepath.Join(tmp, "bin", "evo"), filepath.Join(tmp, "bin", "cloudru-vm")}, keys(files)...) {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("--uninstall left %s\n%s", path, out)
		}
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("--uninstall removed a file that is not evo's: %v", err)
	}
	if !strings.Contains(out, "evo.fish") {
		t.Errorf("--uninstall does not say what it removed:\n%s", out)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// zshrc_unblock keeps a <rc>.evo-backup that is already there: the copy it
// makes before it rewrites rc goes to a name of its own, and only that copy
// is removed after a good write. An existing copy may be the only saved
// version of the person's settings, left by a failed run of install.sh or
// made by hand (Astra #2103: a second install and --uninstall overwrote it
// and then deleted it).
func TestInstallSh_ZshrcUnblockKeepsAnExistingBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh here")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// The functions of install.sh without its last line, the call of main.
	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "functions.sh"), strings.Join(lines[:len(lines)-1], "\n")+"\n", 0o644)
	rc := filepath.Join(dir, "sample.zshrc")
	saved := "# unique saved settings\n"
	writeFile(t, rc+".evo-backup", saved, 0o600)
	cmd := exec.Command("sh", "-c", `. "$1/functions.sh"
printf '# current\n%s\nold evo block\n%s\n' "$ZSHRC_BEGIN" "$ZSHRC_END" >"$2"
zshrc_unblock "$2"`, "probe", dir, rc)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zshrc_unblock: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(rc + ".evo-backup"); err != nil || string(got) != saved {
		t.Errorf("the existing backup is %q, %v; want it kept as it was", got, err)
	}
	if got, _ := os.ReadFile(rc); string(got) != "# current\n" {
		t.Errorf("rc after unblock: %q, want the block removed", got)
	}
	left, _ := filepath.Glob(rc + ".evo-backup?*")
	if len(left) != 0 {
		t.Errorf("unblock left its own copy after a good write: %v", left)
	}
}

func TestInstallSh_BashrcConsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no Bash")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := strings.TrimSuffix(strings.TrimSpace(string(src)), `main "$@"`)
	script += `
export HOME="$1" XDG_DATA_HOME="$1/xdg ' space" SHELL=/bin/bash
f="$XDG_DATA_HOME/bash-completion/completions/evo"
mkdir -p "$(dirname "$f")"
echo "complete -W 'vm version' evo" > "$f"
printf '# user\n\n\n' > "$HOME/.bashrc"
cp "$HOME/.bashrc" "$HOME/original"
EVO_BASHRC=0 bashrc_setup
(EVO_BASHRC=1 SHELL=/bin/zsh bashrc_setup)
cmp "$HOME/original" "$HOME/.bashrc"
EVO_BASHRC=1 bashrc_setup
cp "$HOME/.bashrc" "$HOME/once"
EVO_BASHRC=1 bashrc_setup
cmp "$HOME/.bashrc" "$HOME/once"
"$2" --noprofile --rcfile "$HOME/.bashrc" -ic 'complete -p evo' | grep 'vm version'
printf '# tail' >> "$HOME/.bashrc"
printf '# tail' >> "$HOME/original"
bashrc_remove
bashrc_remove
cmp "$HOME/original" "$HOME/.bashrc"
`
	cmd := exec.Command("sh", "-c", script, "sh", dir, bash)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
