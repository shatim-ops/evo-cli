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

const fixtureVerifier = `#!/bin/sh
test "$#" = 8 && test "$1" = verify-blob && test "$2" = --bundle &&
test "$(cat "$3")" = fixture && test "$4" = --certificate-identity &&
test "$5" = https://github.com/shatim-ops/evo-cli/.github/workflows/release.yml@refs/tags/v9.9.9 &&
test "$6" = --certificate-oidc-issuer && test "$7" = https://token.actions.githubusercontent.com && test -s "$8"
`

func installerPath(t *testing.T, names string) string {
	t.Helper()
	shims := t.TempDir()
	// Expose only installer dependencies. Optional programs on the host,
	// including cosign and fish, must not affect the synthetic release.
	for _, name := range strings.Fields(names) {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(shims, name)); err != nil {
			t.Fatal(err)
		}
	}
	hash := "sha256sum"
	if _, err := exec.LookPath(hash); err != nil {
		hash = "shasum"
	}
	path, err := exec.LookPath(hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(shims, hash)); err != nil {
		t.Fatal(err)
	}
	return shims
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
	if err := os.MkdirAll(filepath.Join(home, ".config", "fish"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".zfunc"), 0o755); err != nil {
		t.Fatal(err)
	}
	shims := installerPath(t, "sh tar gzip curl bash awk cut uname mkdir mktemp chmod mv cp rm dirname grep sed cat")
	writeFile(t, filepath.Join(shims, "zsh"), "#!/bin/sh\necho \"$HOME/.zfunc\"\n", 0o755)
	foreign := filepath.Join(home, ".config", "fish", "completions", "other.fish")
	writeFile(t, foreign, "# not evo's\n", 0o644)

	run := func(success bool, args ...string) string {
		t.Helper()
		// umask 0: the modes of the files are install.sh's, not the umask's.
		cmd := exec.Command("sh", "-c", `umask 0; exec sh "$0" "$@"`, filepath.Join("..", "..", "install.sh"))
		cmd.Args = append(cmd.Args, args...)
		cmd.Env = []string{
			"HOME=" + home, "XDG_DATA_HOME=", "XDG_CONFIG_HOME=",
			"INSTALL_DIR=" + filepath.Join(tmp, "bin"),
			"CLOUDRU_VM_VERSION=v9.9.9", "CLOUDRU_VM_BASE_URL=" + releaseURL.String(),
			"PATH=" + shims, "SHELL=/bin/bash", "EVO_BASHRC=0", "EVO_ZSHRC=0"}
		out, err := cmd.CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("install.sh %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	files := map[string]string{
		filepath.Join(home, ".config", "fish", "completions", "evo.fish"):               "fish",
		filepath.Join(home, ".local", "share", "bash-completion", "completions", "evo"): "bash",
		filepath.Join(home, ".zfunc", "_evo"):                                           "zsh",
	}
	run(true)
	fishFile := filepath.Join(home, ".config", "fish", "completions", "evo.fish")
	if _, err := os.Stat(fishFile); !os.IsNotExist(err) {
		t.Fatal("fish completion with no fish executable", err)
	}
	writeFile(t, filepath.Join(shims, "fish"), "#!/bin/sh\nexit 0\n", 0o755)
	verifier := filepath.Join(shims, "cosign")
	writeFile(t, filepath.Join(rel, "v9.9.9", "checksums.txt.sigstore.json"), "fixture", 0o644)
	oldBinary := filepath.Join(tmp, "bin", "evo")
	writeFile(t, oldBinary, "# previous installation", 0o755)
	writeFile(t, verifier, "#!/bin/sh\nexit 1\n", 0o755)
	if out := run(false); !strings.Contains(out, "подпись") {
		t.Fatal(out)
	}
	if got, err := os.ReadFile(oldBinary); err != nil || string(got) != "# previous installation" {
		t.Fatalf("failed verification changed existing evo: %q, %v", got, err)
	}
	// Model only the verifier interface. Pin its exact issuer and workflow.
	writeFile(t, verifier, fixtureVerifier, 0o755)
	for i := 0; i < 2; i++ { // a second run overwrites its own files
		out := run(true)
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

	if err := os.Remove(filepath.Join(shims, "fish")); err != nil {
		t.Fatal(err)
	}
	out := run(true, "--uninstall")
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

// Execute the published POSIX instructions. The download shim serves only
// local fixtures and the verifier models a trusted tool with a pinned identity.
func agentInstallSnippet(t *testing.T, language string) string {
	t.Helper()
	doc, err := os.ReadFile("../../docs/agent-setup.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, found := strings.Cut(string(doc), "    ```"+language+"\n")
	snippet, _, ended := strings.Cut(after, "    ```")
	if !found || !ended {
		t.Fatalf("missing %s installation instructions", language)
	}
	return snippet
}

func TestAgentInstall_VerifiesBeforeReplacingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX instructions")
	}
	snippet := agentInstallSnippet(t, "sh")

	for _, test := range []string{"no-verifier", "missing-signature", "invalid-signature", "corrupt-archive", "verified"} {
		t.Run(test, func(t *testing.T) {
			root := t.TempDir()
			rel := filepath.Join(root, "release")
			fakeRelease(t, rel)
			shims := installerPath(t, "sh tar gzip awk uname mkdir mktemp install mv cp rm cat")
			writeFile(t, filepath.Join(shims, "curl"), `#!/bin/sh
test "$1 $2 $3 $4 $5" = "--proto =https --tlsv1.2 -fsSL -o" || exit 1
case "$6" in
/dev/null) printf '%s' https://github.com/shatim-ops/evo-cli/releases/tag/v9.9.9 ;;
*) test "$7" = "https://github.com/shatim-ops/evo-cli/releases/download/v9.9.9/$6" && cp "$RELEASE/v9.9.9/$6" "$6" ;;
esac
`, 0o755)
			if test != "no-verifier" {
				writeFile(t, filepath.Join(shims, "cosign"), fixtureVerifier, 0o755)
			}
			if test != "missing-signature" {
				bundle := "fixture"
				if test == "invalid-signature" {
					bundle = "untrusted"
				}
				writeFile(t, filepath.Join(rel, "v9.9.9", "checksums.txt.sigstore.json"), bundle, 0o644)
			}
			if test == "corrupt-archive" {
				archive := "evo_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
				writeFile(t, filepath.Join(rel, "v9.9.9", archive), "corrupt", 0o644)
			}
			dest := filepath.Join(root, "home", ".local", "bin")
			writeFile(t, filepath.Join(dest, "evo"), "old", 0o755)
			cmd := exec.Command("sh", "-c", snippet)
			cmd.Env = []string{"PATH=" + shims, "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + root, "RELEASE=" + rel}
			out, err := cmd.CombinedOutput()
			if (err == nil) != (test == "verified") {
				t.Fatalf("exit %v: %s", err, out)
			}
			want := "old"
			if test == "verified" {
				want = fakeEvo
			}
			if b, err := os.ReadFile(filepath.Join(dest, "evo")); err != nil || string(b) != want {
				t.Fatalf("installed evo: %q, %v", b, err)
			}
			_, err = os.Stat(filepath.Join(dest, "cloudru-vm"))
			if test == "verified" && err != nil || test != "verified" && !os.IsNotExist(err) {
				t.Fatalf("alias: %v", err)
			}
		})
	}
}

func TestAgentInstall_WindowsSignedRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell instructions")
	}
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	snippet := agentInstallSnippet(t, "powershell")
	// Keep the existing PATH entry for this disposable install so the guide
	// never needs to write the user's persistent PATH during this test.
	setup := `
$ErrorActionPreference = "Stop"
$env:TEMP = $env:FIXTURE
$env:LOCALAPPDATA = $env:FIXTURE
$d = "$env:FIXTURE\Programs\evo"
$env:PATH = "$env:FIXTURE;$d;$env:SystemRoot\System32"
New-Item -ItemType Directory -Force $d | Out-Null
Set-Content "$d\evo.exe" -Value "old"
Set-Content "$env:FIXTURE\evo.exe" -Value "verified"
$archive = "evo_9.9.9_windows_amd64.zip"
Compress-Archive "$env:FIXTURE\evo.exe" "$env:FIXTURE\$archive"
$sum = (Get-FileHash "$env:FIXTURE\$archive").Hash.ToLower()
if ($env:SCENARIO -eq "corrupt-archive") { $sum = "0" * 64 }
Set-Content "$env:FIXTURE\checksums.txt" "$sum  $archive"
if ($env:SCENARIO -ne "missing-signature") { Set-Content "$env:FIXTURE\checksums.txt.sigstore.json" "fixture" }
function Invoke-RestMethod($url) { if ($url -ne 'https://api.github.com/repos/shatim-ops/evo-cli/releases/latest') {throw "URL"}; @{tag_name="v9.9.9"} }
function Invoke-WebRequest($url, $OutFile) {
    $name = Split-Path -Leaf $OutFile
    if ($url -ne "https://github.com/shatim-ops/evo-cli/releases/download/v9.9.9/$name") {throw "URL"}
    Copy-Item -LiteralPath "$env:FIXTURE\$name" -Destination $OutFile
}
`
	verifier := `@echo off
if not "%~1"=="verify-blob" exit /b 1
if not "%~2"=="--bundle" exit /b 1
if not exist "%~3" exit /b 1
if not "%~4"=="--certificate-identity" exit /b 1
if not "%~5"=="https://github.com/shatim-ops/evo-cli/.github/workflows/release.yml@refs/tags/v9.9.9" exit /b 1
if not "%~6"=="--certificate-oidc-issuer" exit /b 1
if not "%~7"=="https://token.actions.githubusercontent.com" exit /b 1
if not exist "%~8" exit /b 1
if "%SCENARIO%"=="invalid-signature" exit /b 1
exit /b 0
`
	for _, scenario := range []string{"no-verifier", "missing-signature", "invalid-signature", "corrupt-archive", "verified"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			if scenario != "no-verifier" {
				writeFile(t, filepath.Join(root, "cosign.cmd"), strings.ReplaceAll(verifier, "\n", "\r\n"), 0600)
			}
			script := filepath.Join(root, "test.ps1")
			writeFile(t, script, setup+"\n"+snippet, 0600)
			cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
			cmd.Env = append(os.Environ(), "FIXTURE="+root, "SCENARIO="+scenario)
			out, err := cmd.CombinedOutput()
			if (err == nil) != (scenario == "verified") {
				t.Fatalf("exit %v: %s", err, out)
			}
			want := "old"
			if scenario == "verified" {
				want = "verified"
			}
			got, err := os.ReadFile(filepath.Join(root, "Programs", "evo", "evo.exe"))
			if err != nil || strings.TrimSpace(string(got)) != want {
				t.Fatalf("installed binary: %q, %v", got, err)
			}
		})
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
