// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func TestIsLegacyAlias(t *testing.T) {
	legacy := config.LegacyAppName
	for argv0, want := range map[string]bool{
		legacy:                              true,
		filepath.Join("usr", "bin", legacy): true,
		legacy + ".exe":                     true,
		legacy + ".EXE":                     true,
		filepath.Join("C:", "bin", legacy+".exe"): true,
		"evo":                        false,
		"evo.exe":                    false,
		legacy + "-dev":              false,
		filepath.Join(legacy, "evo"): false, // каталог со старым именем
		"":                           false,
	} {
		if got := isLegacyAlias(argv0); got != want {
			t.Errorf("isLegacyAlias(%q) = %v, want %v", argv0, got, want)
		}
	}
}

func TestAliasNotice(t *testing.T) {
	want := "cloudru-vm переименован в evo, alias будет удалён в v1.1"
	if aliasNotice != want {
		t.Fatalf("aliasNotice = %q", aliasNotice)
	}
	var buf bytes.Buffer
	printAliasNotice(&buf, []string{"version"})
	if buf.String() != want+"\n" {
		t.Errorf("text: %q", buf.String())
	}
	buf.Reset()
	printAliasNotice(&buf, []string{"status", "--json"})
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || got["warning"] != want {
		t.Errorf("json: %q, %v", buf.String(), err)
	}
}

// legacyProject — проект, развёрнутый старым бинарём: legacy-каталог
// ./.cloudru-vm/ со state и legacy-файл ./.cloudru-vm.yaml.
func legacyProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv(config.EnvConfigDir, filepath.Join(dir, "config"))
	for _, k := range []string{"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_API_KEY", "CLOUDRU_PROJECT_ID"} {
		t.Setenv(k, "")
	}
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "dep-1", VMID: "vm-legacy-1", VMIP: "192.0.2.10", Status: "running"})
	legacyDir := filepath.Join(dir, config.LegacyDataDirName)
	if err := state.Write(filepath.Join(legacyDir, "state.json"), sf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.LegacyProjectFileName), []byte("flavor: lowcost10-1-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// cd old-project && evo status: читает legacy ./.cloudru-vm/state.json с
// предупреждением и не заводит ./.evo/.
func TestRun_StatusInLegacyProject(t *testing.T) {
	dir := legacyProject(t)
	newProcess()

	code, out, errOut := runCLI(t, "", "status")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "vm-legacy-1") {
		t.Errorf("stdout has no VM from the legacy state:\n%s", out)
	}
	if !strings.Contains(errOut, config.LegacyDataDirName) || !strings.Contains(errOut, "rename it to "+config.DataDirName) {
		t.Errorf("no warning about the legacy directory:\n%s", errOut)
	}
	if !strings.Contains(errOut, config.LegacyProjectFileName) {
		t.Errorf("no warning about the legacy project file:\n%s", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, config.DataDirName)); !os.IsNotExist(err) {
		t.Errorf("%s created in a legacy project: %v", config.DataDirName, err)
	}

	// --json: предупреждения — JSON-строки в stderr. Новый каталог: в
	// одном процессе каждое предупреждение печатается один раз.
	legacyProject(t)
	newProcess()
	code, out, errOut = runCLI(t, "", "status", "--json")
	if code != 0 || !strings.Contains(out, "vm-legacy-1") {
		t.Fatalf("exit %d, stdout: %s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if len(lines) < 2 {
		t.Errorf("want two warnings, stderr: %q", errOut)
	}
	for _, line := range lines {
		var w map[string]string
		if err := json.Unmarshal([]byte(line), &w); err != nil || w["warning"] == "" {
			t.Errorf("stderr line is not a JSON warning: %q", line)
		}
	}
}

// deploy в старом проекте берёт старый ключ: ВМ из state принимает только его.
func TestSSHKeyInLegacyProject(t *testing.T) {
	dir := legacyProject(t)
	legacyDir := filepath.Join(dir, config.LegacyDataDirName)
	priv, pub, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Ключ создан там же, где state, — в старом каталоге.
	got, err := os.ReadFile(filepath.Join(legacyDir, "id_ed25519"))
	if err != nil || !bytes.Equal(got, priv) {
		t.Fatalf("key not in %s: %v", legacyDir, err)
	}
	priv2, pub2, err := loadOrGenerateSSHKey(dir)
	if err != nil || !bytes.Equal(priv, priv2) || pub != pub2 {
		t.Fatalf("second load gave another key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.DataDirName)); !os.IsNotExist(err) {
		t.Errorf("%s created in a legacy project: %v", config.DataDirName, err)
	}
}

// Новый проект — ./.evo/.
func TestSSHKeyInNewProject(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := loadOrGenerateSSHKey(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.DataDirName, "id_ed25519")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.LegacyDataDirName)); !os.IsNotExist(err) {
		t.Errorf("legacy dir created: %v", err)
	}
}

// Первый запуск evo копирует профили из старого каталога конфигурации.
func TestRun_MigratesLegacyConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// os.UserConfigDir: XDG_CONFIG_HOME (Linux), HOME (macOS), AppData (Windows).
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv(config.EnvConfigDir, "")
	for _, k := range []string{"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_API_KEY", "CLOUDRU_PROJECT_ID", "CLOUDRU_PROFILE"} {
		t.Setenv(k, "")
	}
	base, err := os.UserConfigDir()
	if err != nil || !strings.HasPrefix(base, dir) {
		t.Skipf("user config dir %q outside the test dir: %v", base, err)
	}
	legacy := filepath.Join(base, config.LegacyAppName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"config.yaml":      "profiles:\n  default:\n    project_id: 00000000-0000-0000-0000-00000000aaaa\n",
		"credentials.yaml": "profiles:\n  default:\n    key_id: " + fakeKeyID + "\n    secret: " + fakeSecret + "\n",
	} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	newProcess()

	code, out, errOut := runCLI(t, "", "configure", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "profiles copied from "+legacy) {
		t.Errorf("no migration message: %q", errOut)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("migrated profile not listed:\n%s", out)
	}
	if config.Dir() != filepath.Join(base, config.AppName) {
		t.Errorf("Dir() = %s", config.Dir())
	}
	// Второй запуск — без сообщения.
	newProcess()
	if _, _, errOut := runCLI(t, "", "configure", "list"); strings.Contains(errOut, "copied") {
		t.Errorf("migrated twice: %q", errOut)
	}
}
