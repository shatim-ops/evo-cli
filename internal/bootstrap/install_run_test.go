// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The install script runs here with stub apt-get, docker, dpkg, cloud-init
// and friends in PATH and EVO_ROOT standing for /: the stub apt-get answers
// as the image's mirror or, with Dir::Etc::SourceList, as the official
// mirror, each "up" or failing the way the live runs saw (register OPS-2).

// stubs are the commands the script calls on a VM. Each logs its argv to
// $EVO_ROOT/calls.
var stubs = map[string]string{
	"apt-get": `echo "apt-get $*" >>"$EVO_ROOT/calls"
case " $* " in
*" Dir::Etc::SourceList="*) st="${STUB_OFFICIAL:-up}" host=archive.example.test ;;
*) st="${STUB_IMAGE:-up}" host=repo.mirror.example.test ;;
esac
case " $* " in
*" update "*)
	case "$st" in
	down)
		echo "Err:1 https://$host/ubuntu jammy InRelease"
		echo "W: Failed to fetch https://$host/ubuntu/dists/jammy/InRelease  Could not connect to $host:443, connection timed out"
		echo "W: Some index files failed to download. They have been ignored, or old ones used instead."
		;;
	dockerdown)
		echo "W: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/InRelease  Could not connect to download.docker.com:443"
		;;
	dockerbadsig)
		echo "W: GPG error: https://download.docker.com/linux/ubuntu jammy InRelease: The following signatures couldn't be verified because the public key is not available: NO_PUBKEY 7EA0A9C3F273FCD8"
		echo "E: The repository 'https://download.docker.com/linux/ubuntu jammy InRelease' is not signed."
		exit 100
		;;
	locked)
		echo "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 1234 (apt-get)"
		exit 100
		;;
	dockernorelease)
		echo "Err:5 https://download.docker.com/linux/ubuntu jammy Release"
		echo "  404  Not Found [IP: 192.0.2.1 443]"
		echo "E: The repository 'https://download.docker.com/linux/ubuntu jammy Release' does not have a Release file."
		exit 100
		;;
	esac
	exit 0
	;;
*" install "*)
	case "$st" in
	down | pooldown)
		echo "E: Failed to fetch https://$host/ubuntu/pool/main/p/pigz/pigz_2.6-1_amd64.deb  Connection timed out"
		exit 100
		;;
	esac
	: >"$EVO_ROOT/docker-installed"
	;;
esac
`,
	"docker": `[ -f "$EVO_ROOT/docker-installed" ] || exit 1
echo "Docker Compose version v2.40.0"
`,
	"dpkg": `echo "dpkg $*" >>"$EVO_ROOT/calls"
[ "$1" = --print-architecture ] && echo "${STUB_ARCH:-amd64}"
exit 0
`,
	// macOS has no timeout(1); on a VM the real one bounds apt-get.
	"timeout":    `shift; exec "$@"`,
	"cloud-init": `echo "cloud-init $*" >>"$EVO_ROOT/calls"`,
	"systemctl":  `echo "systemctl $*" >>"$EVO_ROOT/calls"`,
	"usermod":    `echo "usermod $*" >>"$EVO_ROOT/calls"`,
	"curl":       `echo "curl $*" >>"$EVO_ROOT/calls"; exit 7`,
}

type vm struct {
	osRelease string            // /etc/os-release
	files     []string          // files that exist under the root beforehand
	env       map[string]string // STUB_IMAGE, STUB_OFFICIAL, STUB_ARCH
}

type run struct {
	root, out string
	ok        bool
	calls     []string
}

func (r run) file(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func (r run) exists(path string) bool {
	_, err := os.Stat(filepath.Join(r.root, path))
	return err == nil
}

func (r run) called(prefix string) []string {
	var got []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			got = append(got, c)
		}
	}
	return got
}

const ubuntu2204 = "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"22.04\"\nVERSION_CODENAME=jammy\nUBUNTU_CODENAME=jammy\n"

// The image's apt mirror setup succeeded at first boot (its marker), and
// the distribution's keyring is in place.
var cloudruUbuntu = []string{"var/lib/cloud/instance/sem/configure-apt-mirrors", "usr/share/keyrings/ubuntu-archive-keyring.gpg"}

func runInstall(t *testing.T, v vm) run {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install script runs on a Linux VM; no POSIX sh here")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	root, bin := t.TempDir(), t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range append([]string{"etc/os-release"}, v.files...) {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte(v.osRelease), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(sh, script, me.Username)
	cmd.Env = append(os.Environ(), "EVO_ROOT="+root, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for k, val := range v.env {
		cmd.Env = append(cmd.Env, k+"="+val)
	}
	out, err := cmd.CombinedOutput()
	r := run{root: root, out: string(out), ok: err == nil}
	if b, rerr := os.ReadFile(filepath.Join(root, "calls")); rerr == nil {
		r.calls = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	return r
}

// A working image mirror: Docker from Docker's repository with the
// embedded key, the image's sources only, no fallback, no get.docker.com.
func TestInstallRun_ImageMirrorWorks(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	wantList := "deb [arch=amd64 signed-by=" + r.root + "/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu jammy stable\n"
	if got := r.file(t, "etc/apt/sources.list.d/docker.list"); got != wantList {
		t.Errorf("docker.list = %q, want %q", got, wantList)
	}
	if got := r.file(t, "etc/apt/keyrings/docker.asc"); got != strings.ReplaceAll(dockerKey, "\r\n", "\n") {
		t.Errorf("docker.asc is not the embedded key:\n%s", got)
	}
	apt := r.called("apt-get ")
	if len(apt) != 2 || !strings.HasSuffix(apt[0], " update") || !strings.Contains(apt[1], " install -y ") {
		t.Fatalf("apt-get calls = %q, want update then install", apt)
	}
	for _, c := range apt {
		if strings.Contains(c, "Dir::Etc::SourceList") {
			t.Errorf("fallback sources used with a working mirror: %s", c)
		}
		if !strings.Contains(c, "-o Acquire::http::Timeout=20") || !strings.Contains(c, "-o Acquire::Retries=2") {
			t.Errorf("apt-get without the short timeouts: %s", c)
		}
	}
	if !strings.Contains(apt[1], "docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin") {
		t.Errorf("install = %s", apt[1])
	}
	if r.exists("var/lib/evo/apt-fallback") || r.exists("var/lib/evo/apt/sources.list") {
		t.Error("fallback set up with a working mirror")
	}
	if len(r.called("curl")) > 0 {
		t.Error("get.docker.com used on Ubuntu")
	}
	if len(r.called("usermod -aG docker ")) != 1 || !r.exists("opt/app") {
		t.Errorf("user not prepared: %q", r.calls)
	}
}

// The image's mirror does not answer (OPS-2): apt goes to the official
// Ubuntu mirror over HTTPS, signed by the Ubuntu keyring, and Docker's
// repository; the marker sends the next attempt there at once.
func TestInstallRun_MirrorDownFallsBackToOfficialMirror(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "down"}})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	if !strings.Contains(r.out, "evo: switching apt to the official mirror for this install: https://archive.ubuntu.com/ubuntu") {
		t.Errorf("no note of the switch:\n%s", r.out)
	}
	if !strings.Contains(r.out, "W: Failed to fetch https://repo.mirror.example.test/") {
		t.Errorf("the image mirror's error is not shown:\n%s", r.out)
	}
	kr := r.root + "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	want := "deb [signed-by=" + kr + "] https://archive.ubuntu.com/ubuntu jammy main universe\n" +
		"deb [signed-by=" + kr + "] https://archive.ubuntu.com/ubuntu jammy-updates main universe\n" +
		"deb [signed-by=" + kr + "] https://security.ubuntu.com/ubuntu jammy-security main universe\n"
	if got := r.file(t, "var/lib/evo/apt/sources.list"); got != want {
		t.Errorf("fallback sources.list =\n%s\nwant\n%s", got, want)
	}
	if r.file(t, "var/lib/evo/apt/sources.list.d/docker.list") != r.file(t, "etc/apt/sources.list.d/docker.list") {
		t.Error("the fallback lacks Docker's repository")
	}
	if !r.exists("var/lib/evo/apt-fallback") {
		t.Error("no fallback marker for the next attempt")
	}
	apt := r.called("apt-get ")
	if len(apt) != 3 || strings.Contains(apt[0], "SourceList") || !strings.Contains(apt[1], " update") || !strings.Contains(apt[2], " install ") {
		t.Fatalf("apt-get calls = %q, want image update, fallback update, fallback install", apt)
	}
	for _, c := range apt[1:] {
		if !strings.Contains(c, "-o Dir::Etc::SourceList="+r.root+"/var/lib/evo/apt/sources.list -o Dir::Etc::SourceParts="+r.root+"/var/lib/evo/apt/sources.list.d") {
			t.Errorf("not on the fallback sources: %s", c)
		}
	}
	// The platform's own mirror setup had succeeded: nothing to re-run.
	if len(r.called("cloud-init single")) > 0 {
		t.Error("bootcmd re-run although its marker is there")
	}
}

// The platform's first-boot mirror setup did not finish (no marker): it is
// re-run first, and only then, if apt still fails, the official mirror.
func TestInstallRun_MirrorSetupRerunBeforeFallback(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: []string{"usr/share/keyrings/ubuntu-archive-keyring.gpg"},
		env: map[string]string{"STUB_IMAGE": "down"}})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	var order []string
	for _, c := range r.calls {
		switch {
		case strings.HasPrefix(c, "cloud-init single --name bootcmd --frequency always"):
			order = append(order, "bootcmd")
		case strings.HasPrefix(c, "apt-get ") && strings.HasSuffix(c, " update"):
			if strings.Contains(c, "SourceList") {
				order = append(order, "update-official")
			} else {
				order = append(order, "update-image")
			}
		}
	}
	if strings.Join(order, ",") != "update-image,bootcmd,update-image,update-official" {
		t.Errorf("order = %v", order)
	}
}

// The index downloads, the packages do not (E: Failed to fetch …/pool/…):
// the install, too, goes to the official mirror.
func TestInstallRun_PackagesFailFromMirror(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "pooldown"}})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	apt := r.called("apt-get ")
	if len(apt) != 4 || !strings.Contains(apt[3], " install ") || !strings.Contains(apt[3], "SourceList") {
		t.Fatalf("apt-get calls = %q, want the second install on the fallback", apt)
	}
	if !strings.Contains(r.out, "evo: apt-get install failed with the image's apt sources:") ||
		!strings.Contains(r.out, "E: Failed to fetch https://repo.mirror.example.test/ubuntu/pool/") {
		t.Errorf("output:\n%s", r.out)
	}
}

// Both mirrors fail: the script fails with the line evo's diagnosis keys
// on, and the official mirror's errors.
func TestInstallRun_BothMirrorsDown(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "down", "STUB_OFFICIAL": "down"}})
	if r.ok {
		t.Fatalf("install succeeded:\n%s", r.out)
	}
	if !strings.Contains(r.out, FallbackFailedLine+" (https://archive.ubuntu.com/ubuntu):") ||
		!strings.Contains(r.out, "W: Failed to fetch https://archive.example.test/") {
		t.Errorf("output:\n%s", r.out)
	}
	if len(r.called("apt-get ")) != 2 || len(r.called("usermod")) != 0 {
		t.Errorf("calls after the failure: %q", r.calls)
	}
}

// Only Docker's repository fails: another Ubuntu mirror would not help, so
// no fallback, and the script says which repository.
func TestInstallRun_DockerRepoDown(t *testing.T) {
	for mode, want := range map[string]string{
		"dockerdown":   DockerRepoFailedLine + ": no answer",
		"dockerbadsig": DockerRepoFailedLine + ": its signature does not verify",
	} {
		r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": mode}})
		if r.ok {
			t.Fatalf("%s: install succeeded:\n%s", mode, r.out)
		}
		if !strings.Contains(r.out, want) {
			t.Errorf("%s: output lacks %q:\n%s", mode, want, r.out)
		}
		if r.exists("var/lib/evo/apt-fallback") {
			t.Errorf("%s: fallback for a Docker repository failure", mode)
		}
	}
}

// Another apt holds the lock: not a mirror problem, no fallback; evo
// retries the script.
func TestInstallRun_LockIsNotAMirrorFailure(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "locked"}})
	if r.ok {
		t.Fatalf("install succeeded:\n%s", r.out)
	}
	if !strings.Contains(r.out, "evo: apt-get update failed:") || !strings.Contains(r.out, "Could not get lock") {
		t.Errorf("output:\n%s", r.out)
	}
	if r.exists("var/lib/evo/apt-fallback") {
		t.Error("fallback for a held lock")
	}
}

// An earlier attempt switched to the official mirror: the next one does not
// wait on the image's mirror again.
func TestInstallRun_MarkerGoesStraightToFallback(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: append([]string{"var/lib/evo/apt-fallback"}, cloudruUbuntu...),
		env: map[string]string{"STUB_IMAGE": "down"}})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	for _, c := range r.called("apt-get ") {
		if !strings.Contains(c, "SourceList") {
			t.Errorf("image sources used after the marker: %s", c)
		}
	}
}

// Debian: deb.debian.org and its security suite, signed by the Debian
// keyring; arm64 Ubuntu would take ports.ubuntu.com instead.
func TestInstallRun_DebianFallback(t *testing.T) {
	r := runInstall(t, vm{
		osRelease: "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\n",
		files:     []string{"var/lib/cloud/instance/sem/configure-apt-mirrors", "usr/share/keyrings/debian-archive-keyring.gpg"},
		env:       map[string]string{"STUB_IMAGE": "down"},
	})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	kr := r.root + "/usr/share/keyrings/debian-archive-keyring.gpg"
	want := "deb [signed-by=" + kr + "] https://deb.debian.org/debian bookworm main\n" +
		"deb [signed-by=" + kr + "] https://deb.debian.org/debian bookworm-updates main\n" +
		"deb [signed-by=" + kr + "] https://deb.debian.org/debian-security bookworm-security main\n"
	if got := r.file(t, "var/lib/evo/apt/sources.list"); got != want {
		t.Errorf("fallback sources.list =\n%s\nwant\n%s", got, want)
	}
	if got := r.file(t, "etc/apt/sources.list.d/docker.list"); !strings.Contains(got, "https://download.docker.com/linux/debian bookworm stable") {
		t.Errorf("docker.list = %q", got)
	}
}

func TestInstallRun_UbuntuArm64FallsBackToPorts(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "down", "STUB_ARCH": "arm64"}})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	got := r.file(t, "var/lib/evo/apt/sources.list")
	if strings.Count(got, "https://ports.ubuntu.com/ubuntu-ports ") != 3 || strings.Contains(got, "archive.ubuntu.com") {
		t.Errorf("fallback sources.list =\n%s", got)
	}
}

// A VM that already has Docker (a re-run of deploy): no apt at all, only
// the group and /opt/app.
func TestInstallRun_IdempotentWithDocker(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: append([]string{"docker-installed"}, cloudruUbuntu...)})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	if len(r.called("apt-get")) > 0 || len(r.called("curl")) > 0 || r.exists("etc/apt/sources.list.d/docker.list") {
		t.Errorf("installed again: %q", r.calls)
	}
	if len(r.called("usermod -aG docker ")) != 1 || !r.exists("opt/app") {
		t.Errorf("user not prepared: %q", r.calls)
	}
}

// Not Ubuntu or Debian: get.docker.com as before, no apt repository.
func TestInstallRun_OtherDistributionUsesGetDocker(t *testing.T) {
	r := runInstall(t, vm{osRelease: "NAME=\"CentOS Stream\"\nID=\"centos\"\nVERSION_ID=\"9\"\n"})
	if r.ok {
		t.Fatalf("install succeeded with a failing curl:\n%s", r.out)
	}
	if c := r.called("curl"); len(c) != 1 || c[0] != "curl -fsSL https://get.docker.com" {
		t.Errorf("curl calls = %q", c)
	}
	if len(r.called("apt-get")) > 0 || r.exists("etc/apt/sources.list.d/docker.list") {
		t.Errorf("apt on CentOS: %q", r.calls)
	}
}

// REL-7: у Docker нет пакетов для кодового имени образа (нет файла
// Release): отказ, который повтор не исправит, — Permanent, без запасного
// зеркала (другое зеркало Ubuntu ответит так же).
func TestInstallRun_DockerRepoNoReleaseIsPermanent(t *testing.T) {
	r := runInstall(t, vm{osRelease: ubuntu2204, files: cloudruUbuntu, env: map[string]string{"STUB_IMAGE": "dockernorelease"}})
	if r.ok {
		t.Fatalf("install succeeded:\n%s", r.out)
	}
	if !strings.Contains(r.out, DockerNoReleaseLine+" ubuntu jammy") {
		t.Errorf("output lacks %q:\n%s", DockerNoReleaseLine, r.out)
	}
	if strings.Contains(r.out, DockerRepoFailedLine) {
		t.Errorf("a missing Release file reported as a repository that does not answer:\n%s", r.out)
	}
	if !Permanent(r.out) {
		t.Errorf("not permanent:\n%s", r.out)
	}
	if r.exists("var/lib/evo/apt-fallback") {
		t.Error("fallback for a release Docker does not build")
	}
}

// REL-7: Ubuntu без VERSION_CODENAME в /etc/os-release — репозиторий
// Docker не выбрать; отказ сразу и Permanent.
func TestInstallRun_NoCodenameIsPermanent(t *testing.T) {
	r := runInstall(t, vm{osRelease: "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"22.04\"\n", files: cloudruUbuntu})
	if r.ok {
		t.Fatalf("install succeeded:\n%s", r.out)
	}
	if !strings.Contains(r.out, NoCodenameLine) || !Permanent(r.out) {
		t.Errorf("want %q and a permanent refusal:\n%s", NoCodenameLine, r.out)
	}
	if len(r.called("apt-get")) > 0 {
		t.Errorf("apt without a codename: %q", r.calls)
	}
}

// REL-7: строки, которые скрипт печатает при отказах «навсегда», совпадают
// с константами Permanent (curl нет — на машине теста curl есть, поэтому
// сверяется текст скрипта), а временные сбои Permanent не считает.
func TestPermanent_Lines(t *testing.T) {
	script := InstallScript()
	for _, l := range []string{NoKeyringLine, UnsupportedOSLine, NoCodenameLine, CurlMissingLine, DockerNoReleaseLine} {
		if !strings.Contains(script, `say "`+strings.TrimPrefix(l, "evo: ")) {
			t.Errorf("install.sh does not print %q", l)
		}
		if !Permanent("x\n" + l + " …\n") {
			t.Errorf("Permanent(%q) = false", l)
		}
	}
	for _, l := range []string{FallbackFailedLine, DockerRepoFailedLine + ": no answer", "W: Failed to fetch https://repo.example.test"} {
		if Permanent(l) {
			t.Errorf("Permanent(%q) = true for a failure another attempt may fix", l)
		}
	}
}
