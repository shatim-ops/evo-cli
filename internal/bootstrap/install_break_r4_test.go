// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runBreakInstall is runInstall with its own stubs and files created by
// prep (symlinks). The shared stubs map is left alone.
func runBreakInstall(t *testing.T, osRelease string, env map[string]string, stubs map[string]string, prep func(root string)) run {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install script runs on a Linux VM")
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
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte(osRelease), 0o644); err != nil {
		t.Fatal(err)
	}
	if prep != nil {
		prep(root)
	}
	script := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, sh, script, me.Username)
	cmd.Env = append(os.Environ(), "EVO_ROOT="+root, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for k, val := range env {
		cmd.Env = append(cmd.Env, k+"="+val)
	}
	out, err := cmd.CombinedOutput()
	r := run{root: root, out: string(out), ok: err == nil && ctx.Err() == nil}
	if b, rerr := os.ReadFile(filepath.Join(root, "calls")); rerr == nil {
		r.calls = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	return r
}

func breakStubs(cloudInit string) map[string]string {
	s := map[string]string{}
	for k, v := range stubs {
		s[k] = v
	}
	if cloudInit != "" {
		s["cloud-init"] = cloudInit
	}
	return s
}

// Без keyring запасной sources.list пишется без signed-by, и apt-get install
// всё равно считается успехом. Комментарий скрипта обещает обратное.
func TestBreakR4_R4_15_FallbackWithoutKeyringIsSigned(t *testing.T) {
	r := runBreakInstall(t, ubuntu2204, map[string]string{"STUB_IMAGE": "down"}, breakStubs(""), func(root string) {
		// marker of a finished mirror setup, and no distribution keyring
		p := filepath.Join(root, "var/lib/cloud/instance/sem")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "configure-apt-mirrors"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	// Решение: без keyring запасного зеркала нет — отказ с NoKeyringLine,
	// apt с запасными источниками не запускается.
	if r.ok || !strings.Contains(r.out, NoKeyringLine) || !Permanent(r.out) {
		t.Errorf("want a refusal with %q, ok=%v:\n%s", NoKeyringLine, r.ok, r.out)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "Dir::Etc::SourceList=") {
			t.Errorf("apt ran with the fallback sources: %s", c)
		}
	}
	srcPath := filepath.Join(r.root, "var/lib/evo/apt/sources.list")
	b, err := os.ReadFile(srcPath)
	if err != nil {
		if r.ok {
			t.Fatalf("no fallback sources and install succeeded:\n%s", r.out)
		}
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(line, "deb ") && !strings.Contains(line, "signed-by=") {
			t.Errorf("unsigned fallback source %q (install ok=%v)", line, r.ok)
		}
	}
}

// docker.list — символическая ссылка на sources.list образа. Заголовок
// скрипта обещает, что источники образа не меняются.
func TestBreakR4_R4_16_DockerListSymlinkKeepsImageSources(t *testing.T) {
	const original = "deb https://archive.example.test/ubuntu jammy main\n"
	r := runBreakInstall(t, ubuntu2204, nil, breakStubs(""), func(root string) {
		if err := os.MkdirAll(filepath.Join(root, "etc/apt/sources.list.d"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc/apt/sources.list"), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../sources.list", filepath.Join(root, "etc/apt/sources.list.d/docker.list")); err != nil {
			t.Fatal(err)
		}
		for _, f := range cloudruUbuntu {
			p := filepath.Join(root, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	})
	got, err := os.ReadFile(filepath.Join(r.root, "etc/apt/sources.list"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("image sources.list changed to %q\n%s", got, r.out)
	}
	// Ссылка заменена обычным файлом с репозиторием Docker.
	st, err := os.Lstat(filepath.Join(r.root, "etc/apt/sources.list.d/docker.list"))
	if err != nil || !st.Mode().IsRegular() {
		t.Fatalf("docker.list is not a regular file (%v):\n%s", err, r.out)
	}
	if !r.ok || !strings.Contains(r.file(t, "etc/apt/sources.list.d/docker.list"), "https://download.docker.com/linux/ubuntu jammy stable") {
		t.Errorf("docker.list not written, ok=%v:\n%s", r.ok, r.out)
	}
}

// Так же — ключ Docker и жёсткая ссылка: файл, на который они ведут, не
// меняется, на месте ссылки — новый файл.
func TestDockerListAndKeyLinksAreReplaced(t *testing.T) {
	const original = "deb https://archive.example.test/ubuntu jammy main\n"
	const keyring = "IMAGE KEYRING\n"
	r := runBreakInstall(t, ubuntu2204, nil, breakStubs(""), func(root string) {
		for _, d := range []string{"etc/apt/sources.list.d", "etc/apt/keyrings", "etc/apt/trusted.gpg.d"} {
			if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "etc/apt/sources.list"), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc/apt/trusted.gpg.d/image.asc"), []byte(keyring), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(root, "etc/apt/sources.list"), filepath.Join(root, "etc/apt/sources.list.d/docker.list")); err != nil {
			t.Skipf("link: %v", err)
		}
		if err := os.Symlink("../trusted.gpg.d/image.asc", filepath.Join(root, "etc/apt/keyrings/docker.asc")); err != nil {
			t.Fatal(err)
		}
		for _, f := range cloudruUbuntu {
			p := filepath.Join(root, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !r.ok {
		t.Fatalf("install failed:\n%s", r.out)
	}
	if got := r.file(t, "etc/apt/sources.list"); got != original {
		t.Errorf("image sources.list changed through a hard link: %q", got)
	}
	if got := r.file(t, "etc/apt/trusted.gpg.d/image.asc"); got != keyring {
		t.Errorf("image keyring changed through a symlink: %q", got)
	}
	if st, err := os.Lstat(filepath.Join(r.root, "etc/apt/keyrings/docker.asc")); err != nil || !st.Mode().IsRegular() {
		t.Errorf("docker.asc is not a regular file: %v", err)
	}
	if got := r.file(t, "etc/apt/keyrings/docker.asc"); got != strings.ReplaceAll(dockerKey, "\r\n", "\n") {
		t.Error("docker.asc is not Docker's key")
	}
}

// Не Ubuntu и не Debian: пакет ставится через curl | sh с get.docker.com,
// без проверки подписи.
func TestBreakR4_R4_17_OtherDistroDoesNotPipeDockerScript(t *testing.T) {
	r := runBreakInstall(t, "NAME=\"Alpine\"\nID=alpine\n", nil, breakStubs(""), nil)
	for _, c := range r.calls {
		if strings.Contains(c, "get.docker.com") {
			t.Fatalf("unsigned installer was fetched: %s\n%s", c, r.out)
		}
	}
	// Решение: отказ с понятным текстом, который движок узнаёт и не
	// повторяет установку до своего срока.
	if r.ok || !strings.Contains(r.out, UnsupportedOSLine) || !strings.Contains(r.out, "not on alpine") || !Permanent(r.out) {
		t.Errorf("want a refusal with %q, ok=%v:\n%s", UnsupportedOSLine, r.ok, r.out)
	}
}

// RPM-дистрибутивы — через get.docker.com, как CentOS (SEC-8): по ID или
// по ID_LIKE с rhel/fedora. Прочие с ID_LIKE (suse, arch) — отказ.
func TestInstallRun_RPMDistributionsUseGetDocker(t *testing.T) {
	for _, tc := range []struct {
		osRelease string
		getDocker bool
	}{
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", true},
		{"ID=\"almalinux\"\nID_LIKE=\"rhel centos fedora\"\n", true},
		{"ID=fedora\n", true},
		{"ID=\"rhel\"\nID_LIKE=\"fedora\"\n", true},
		{"ID=\"sberlinux\"\nID_LIKE=\"rhel fedora\"\n", true},
		{"ID=\"opensuse-leap\"\nID_LIKE=\"suse opensuse\"\n", false},
		{"ID=arch\n", false},
		{"ID=\"rhel-clone\"\nID_LIKE=\"rhelish\"\n", false},
	} {
		r := runBreakInstall(t, tc.osRelease, nil, breakStubs(""), nil)
		fetched := len(r.called("curl -fsSL https://get.docker.com")) == 1
		if fetched != tc.getDocker || strings.Contains(r.out, UnsupportedOSLine) == tc.getDocker {
			t.Errorf("%q: get.docker.com=%v, want %v:\n%s", tc.osRelease, fetched, tc.getDocker, r.out)
		}
		if r.ok {
			t.Errorf("%q: ok with a failing curl", tc.osRelease)
		}
	}
}

// cloud-init status --wait без своего предела: зависший stub не отпускает
// скрипт. Скрипт должен закончиться сам.
func TestBreakR4_R4_18_CloudInitWaitIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the install script runs on a Linux VM")
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
	body := breakStubs(`echo "cloud-init $*" >>"$EVO_ROOT/calls"
case " $* " in
*" status "*) sleep 30 ;;
esac
`)
	// Общая заглушка timeout предел не соблюдает (на macOS нет timeout(1)),
	// а 600 с в тесте не ждать: здесь предел для cloud-init «истекает»
	// сразу — код 124, как у настоящего timeout. Если скрипт зовёт
	// cloud-init status без timeout, заглушка cloud-init висит 30 с.
	body["timeout"] = `echo "timeout $*" >>"$EVO_ROOT/calls"
if [ "$2" = cloud-init ] && [ "$3" = status ]; then exit 124; fi
shift; exec "$@"
`
	for name, b := range body {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+b), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte(ubuntu2204), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, script, me.Username)
	cmd.Env = append(os.Environ(), "EVO_ROOT="+root, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("cloud-init status --wait is not bounded (killed after 3s): %v\n%s", err, out)
	}
	if err != nil {
		t.Fatalf("install failed after the cloud-init limit: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if !strings.Contains(string(calls), "timeout 600 cloud-init status --wait") || !strings.Contains(string(calls), "apt-get") {
		t.Errorf("want cloud-init waited for 600s and the install going on:\n%s", calls)
	}
	if !strings.Contains(string(out), "cloud-init is still running after 600s") {
		t.Errorf("no note about the limit:\n%s", out)
	}
}
