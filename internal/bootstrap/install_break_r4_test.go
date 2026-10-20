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
}
