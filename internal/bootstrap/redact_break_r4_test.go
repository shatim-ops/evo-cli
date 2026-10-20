// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// redactScript is .github/scripts/redact.sed. That directory is not a Go
// package (go test ./... skips names starting with a dot), so the check
// lives next to the other shell-script tests and calls the script by path.
func redactScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, ".github", "scripts", "redact.sed")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func redactLine(t *testing.T, line string) string {
	t.Helper()
	cmd := exec.Command("sed", "-E", "-f", redactScript(t))
	cmd.Stdin = strings.NewReader(line + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sed: %v", err)
	}
	return strings.TrimRight(string(out), "\n")
}

// Маска пропускает адрес, хост ресурса и токен длины openssl rand -hex 32.
func TestBreakR4_R4_13_RedactMisses(t *testing.T) {
	cases := []struct {
		in, leaked string
	}{
		{"addr 2001:0:0:0:0:0:0:1 end", "2001:0:0:0:0:0:0:1"},
		{"app MyApp.Containers.Cloud.RU ready", "MyApp.Containers.Cloud.RU"},
		{"bucket my_bucket.s3.cloud.ru", "my_"},
		{"token 0000000000000000000000000000000000000000000000000000000000000000", "0000000000000000000000000000000000000000000000000000000000000000"},
	}
	for _, c := range cases {
		got := redactLine(t, c.in)
		if strings.Contains(got, c.leaked) {
			t.Errorf("input %q left %q in %q", c.in, c.leaked, got)
		}
	}
}

// Публичный хост в другом регистре маска превращает в <host>, хотя
// строчные id/console/s3/api она сохраняет.
func TestBreakR4_R4_14_RedactCorruptsPublicHost(t *testing.T) {
	for _, host := range []string{"Console.cloud.ru", "S3.cloud.ru", "IAM.API.cloud.ru", "Auth.IAM.cloud.ru"} {
		got := redactLine(t, "via "+host+" ok")
		if !strings.Contains(got, host) {
			t.Errorf("%s became %q", host, got)
		}
	}
}
