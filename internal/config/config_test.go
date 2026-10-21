// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// isolate points the config directory at a temp dir and blanks every
// CLOUDRU_* variable of the developer's shell (empty counts as unset).
// All credentials in these tests are fake.
func isolate(t *testing.T) string {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") || strings.HasPrefix(k, "EVOLUTION_") {
			t.Setenv(k, "")
		}
	}
	dir := filepath.Join(t.TempDir(), "evo")
	t.Setenv(EnvConfigDir, dir)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func saveFakeProfile(t *testing.T, name string, p Profile) {
	t.Helper()
	if err := SaveProfile(name, p, Credentials{KeyID: "fake-key-id-" + name, Secret: "fake-secret-" + name}); err != nil {
		t.Fatal(err)
	}
}

func exitCode(err error) int { return clerrors.ExitCode(err) }

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Region != "" || cfg.Flavor != "auto" || cfg.Timeout != DefaultTimeout ||
		cfg.ComposeFile != "docker-compose.yml" || cfg.DiskSize != 10 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestResolve_NothingConfigured(t *testing.T) {
	isolate(t)
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatalf("no files and no env must not be an error: %v", err)
	}
	if cfg.Profile != DefaultProfile || cfg.KeyID != "" || cfg.Flavor != "auto" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestResolve_Precedence(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{
		ProjectID: "proj-profile", Region: "az-profile", Flavor: "flavor-profile",
		ImageID: "img-profile", DiskSize: 20,
	})
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "region: az-project\nflavor: flavor-project\nimage_id: img-project\n")
	t.Setenv("CLOUDRU_FLAVOR", "flavor-env")
	t.Setenv("CLOUDRU_IMAGE_ID", "img-env")

	cfg, sources, err := ResolveWithSources(Options{
		ProjectFile: project,
		Flags:       map[string]any{"ImageID": "img-flag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		// field: value, origin
		"ProjectID": {"proj-profile", OriginProfile},
		"Region":    {"az-project", OriginProject},
		"Flavor":    {"flavor-env", OriginEnv},
		"ImageID":   {"img-flag", OriginFlag},
		"Timeout":   {"1500", OriginDefault},
		"DiskSize":  {"20", OriginProfile},
		"KeyID":     {"fake****ault", OriginProfile},
	}
	for _, s := range sources {
		w, ok := want[s.Key]
		if !ok {
			continue
		}
		if got := toString(s.Value); got != w[0] || s.Origin != w[1] {
			t.Errorf("%s = %q from %s, want %q from %s", s.Key, got, s.Origin, w[0], w[1])
		}
	}
	if cfg.KeyID != "fake-key-id-default" || cfg.Secret != "fake-secret-default" {
		t.Errorf("credentials not taken from the profile: %q", cfg.KeyID)
	}
	for _, s := range sources {
		if s.Key == "Secret" && s.Value != "****" {
			t.Errorf("secret not masked in sources: %v", s.Value)
		}
	}
}

func TestResolve_EnvCredentialsOverrideProfileAsAPair(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})

	t.Setenv("CLOUDRU_KEY_ID", "env-key")
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Only the key ID in env: the profile secret must not be mixed in.
	if cfg.KeyID != "env-key" || cfg.Secret != "" {
		t.Errorf("got key %q secret set=%v, want env-key without secret", cfg.KeyID, cfg.Secret != "")
	}

	t.Setenv("CLOUDRU_SECRET", "env-secret")
	cfg, err = resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "env-key" || cfg.Secret != "env-secret" {
		t.Errorf("env pair not used: %q", cfg.KeyID)
	}
}

// Блок env из MCP-гайда Cloud.ru (CLOUDRU_KEY_SECRET) и переменные, которые
// AI Agents передаёт MCP-серверу (EVOLUTION_*): ключ и проект берутся;
// CLOUDRU_SECRET и CLOUDRU_PROJECT_ID важнее.
func TestResolve_CloudRuEnvAliases(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	t.Setenv("CLOUDRU_KEY_ID", "env-key")
	t.Setenv("CLOUDRU_KEY_SECRET", "guide-secret")
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "env-key" || cfg.Secret != "guide-secret" {
		t.Errorf("CLOUDRU_KEY_SECRET not taken: key %q", cfg.KeyID)
	}
	t.Setenv("CLOUDRU_SECRET", "main-secret")
	if cfg, _ = resolveConfig(Options{}); cfg.Secret != "main-secret" {
		t.Error("CLOUDRU_SECRET must win over CLOUDRU_KEY_SECRET")
	}

	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_ID", "agent-key")
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "agent-secret")
	t.Setenv("EVOLUTION_PROJECT_ID", "00000000-0000-0000-0000-0000000000e1")
	cfg, err = resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "agent-key" || cfg.Secret != "agent-secret" || cfg.ProjectID != "00000000-0000-0000-0000-0000000000e1" {
		t.Errorf("EVOLUTION_* not taken: key %q project %q", cfg.KeyID, cfg.ProjectID)
	}
	t.Setenv("CLOUDRU_KEY_ID", "env-key")
	t.Setenv("CLOUDRU_PROJECT_ID", "00000000-0000-0000-0000-0000000000c1")
	if cfg, _ = resolveConfig(Options{}); cfg.KeyID != "env-key" || cfg.Secret != "" || cfg.ProjectID != "00000000-0000-0000-0000-0000000000c1" {
		t.Errorf("CLOUDRU_* must win, as a pair: key %q secret set=%v project %q", cfg.KeyID, cfg.Secret != "", cfg.ProjectID)
	}
}

// Ревью Grok на #84: CLOUDRU_KEY_SECRET рядом с парой EVOLUTION_* — берётся
// CLOUDRU_* (без смешивания); doctor называет переменные ключа; проект из
// EVOLUTION_PROJECT_ID против профиля — предупреждение с этим именем; ключ
// агента с проектом из другого источника — предупреждение.
func TestResolve_CloudRuEnvAliasSources(t *testing.T) {
	const agentProject = "00000000-0000-0000-0000-0000000000e1"
	const profileProject = "00000000-0000-0000-0000-0000000000a1"

	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	t.Setenv("CLOUDRU_KEY_SECRET", "guide-secret")
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_ID", "agent-key")
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "agent-secret")
	_, sources, err := ResolveWithSources(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s.Key == "KeyID" && (s.Value != "" || s.Origin != "env (CLOUDRU_KEY_ID, CLOUDRU_KEY_SECRET)") {
			t.Errorf("KeyID %q from %q: CLOUDRU_* must win as a pair", s.Value, s.Origin)
		}
	}

	isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: profileProject})
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_ID", "agent-key")
	t.Setenv("EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "agent-secret")
	t.Setenv("EVOLUTION_PROJECT_ID", agentProject)
	cfg, sources, err := ResolveWithSources(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != agentProject || len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "EVOLUTION_PROJECT_ID") {
		t.Errorf("project %q, warnings %q", cfg.ProjectID, cfg.Warnings)
	}
	for _, s := range sources {
		if s.Key == "Secret" && s.Origin != "env (EVOLUTION_SERVICE_ACCOUNT_KEY_ID, EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET)" {
			t.Errorf("Secret origin %q", s.Origin)
		}
	}

	// Ключ агента, проект — из флага: предупреждение называет флаг.
	cfg, err = resolveConfig(Options{Flags: map[string]any{"ProjectID": profileProject}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "--project-id") || !strings.Contains(cfg.Warnings[0], agentProject) {
		t.Errorf("warnings %q", cfg.Warnings)
	}
}

func TestResolve_DeprecatedAPIKey(t *testing.T) {
	isolate(t)
	t.Setenv("CLOUDRU_API_KEY", "legacy-id:legacy-secret")
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "legacy-id" || cfg.Secret != "legacy-secret" {
		t.Errorf("legacy key not split: %q", cfg.KeyID)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "deprecated") ||
		!strings.Contains(cfg.Warnings[0], "CLOUDRU_KEY_ID") {
		t.Errorf("expected a deprecation warning, got %q", cfg.Warnings)
	}

	// The proper variables win and silence the warning.
	t.Setenv("CLOUDRU_KEY_ID", "new-id")
	t.Setenv("CLOUDRU_SECRET", "new-secret")
	cfg, err = resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "new-id" || len(cfg.Warnings) != 0 {
		t.Errorf("got %q, warnings %q", cfg.KeyID, cfg.Warnings)
	}
}

func TestResolve_MalformedAPIKeyIsAuthError(t *testing.T) {
	isolate(t)
	t.Setenv("CLOUDRU_API_KEY", "no-colon-here")
	_, err := resolveConfig(Options{})
	if exitCode(err) != clerrors.ExitAuth {
		t.Errorf("exit code %d for %v, want %d", exitCode(err), err, clerrors.ExitAuth)
	}
}

func TestResolve_ProfileSelection(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "proj-default"})
	saveFakeProfile(t, "staging", Profile{ProjectID: "proj-staging"})

	cfg, err := resolveConfig(Options{Profile: "staging"})
	if err != nil || cfg.ProjectID != "proj-staging" || cfg.KeyID != "fake-key-id-staging" {
		t.Fatalf("flag: %+v %v", cfg, err)
	}

	t.Setenv("CLOUDRU_PROFILE", "staging")
	cfg, err = resolveConfig(Options{})
	if err != nil || cfg.Profile != "staging" || cfg.ProjectID != "proj-staging" {
		t.Fatalf("env: %+v %v", cfg, err)
	}
	// The flag beats the env.
	cfg, err = resolveConfig(Options{Profile: "default"})
	if err != nil || cfg.ProjectID != "proj-default" {
		t.Fatalf("flag over env: %+v %v", cfg, err)
	}
}

func TestResolve_UnknownExplicitProfile(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	_, err := resolveConfig(Options{Profile: "nosuch"})
	if exitCode(err) != clerrors.ExitAuth {
		t.Fatalf("exit code %d for %v, want %d", exitCode(err), err, clerrors.ExitAuth)
	}
	var cliErr *clerrors.CLIError
	if !asCLI(err, &cliErr) || !strings.Contains(cliErr.Hint, "configure --profile nosuch") {
		t.Errorf("hint does not say how to create the profile: %v", err)
	}

	if _, err := resolveConfig(Options{Profile: "../etc"}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("path-like profile name accepted: %v", err)
	}
}

func TestResolve_ProjectFileSecretsIgnored(t *testing.T) {
	isolate(t)
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "project_id: p1\nkey_id: leaked-id\nsecret: leaked-secret\n")
	cfg, err := resolveConfig(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "" || cfg.Secret != "" || cfg.ProjectID != "p1" {
		t.Errorf("secrets taken from the project file: %+v", cfg)
	}
	if len(cfg.Warnings) != 2 {
		t.Errorf("want warnings for key_id and secret, got %q", cfg.Warnings)
	}
}

func TestResolve_S3SectionAndEnvOverride(t *testing.T) {
	isolate(t)
	err := SaveProfile("default", Profile{}, Credentials{
		KeyID: "k", Secret: "s",
		S3: &S3Config{AccessKey: "s3-file-ak", SecretKey: "s3-file-sk", Region: "ru-central-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.AccessKey != "s3-file-ak" || cfg.S3.SecretKey != "s3-file-sk" {
		t.Errorf("s3 section not read: %+v", cfg.S3)
	}
	t.Setenv("CLOUDRU_S3_ACCESS_KEY", "s3-env-ak")
	cfg, err = resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.AccessKey != "s3-env-ak" || cfg.S3.Region != "ru-central-1" {
		t.Errorf("env must override the profile: %+v", cfg.S3)
	}
}

func TestResolve_InvalidInputs(t *testing.T) {
	isolate(t)
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "region: [invalid yaml\n")
	if _, err := resolveConfig(Options{ProjectFile: project}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("invalid YAML: %v", err)
	}
	t.Setenv("CLOUDRU_TIMEOUT", "not-a-number")
	if _, err := resolveConfig(Options{}); err == nil {
		t.Error("non-integer CLOUDRU_TIMEOUT accepted")
	}
	t.Setenv("CLOUDRU_TIMEOUT", "")
	writeFile(t, CredentialsPath(), "profiles: [broken\n")
	if _, err := resolveConfig(Options{}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("broken credentials.yaml: %v", err)
	}
}

func TestResolve_MissingProjectFile(t *testing.T) {
	isolate(t)
	cfg, err := resolveConfig(Options{ProjectFile: filepath.Join(t.TempDir(), "nonexistent.yaml"), Flags: map[string]any{"Verbose": true}})
	if err != nil || !cfg.Verbose || cfg.Flavor != "auto" {
		t.Errorf("got %+v, %v", cfg, err)
	}
}

func TestSaveProfile_FileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not enforced on Windows")
	}
	dir := isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "p"})

	for path, want := range map[string]os.FileMode{
		dir:               0o700,
		SettingsPath():    0o600,
		CredentialsPath(): 0o600,
	} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s: mode %04o, want %04o", path, st.Mode().Perm(), want)
		}
	}
	// Secrets go to credentials.yaml only.
	data, _ := os.ReadFile(SettingsPath())
	if strings.Contains(string(data), "fake-secret") || strings.Contains(string(data), "fake-key-id") {
		t.Errorf("config.yaml contains credentials:\n%s", data)
	}
}

func TestReadCredentials_WarnsOnLooseMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not enforced on Windows")
	}
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	if err := os.Chmod(CredentialsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "chmod 600") {
		t.Errorf("expected a permissions warning, got %q", cfg.Warnings)
	}
}

func TestProfiles_ListAndDelete(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "prod", Profile{ProjectID: "p-prod"})
	saveFakeProfile(t, "default", Profile{ProjectID: "p-default"})
	writeFile(t, TokenCachePath("prod"), `{"key_id":"x"}`)

	list, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "default" || list[1].Name != "prod" ||
		list[1].ProjectID != "p-prod" || !list[1].HasSecret || strings.Contains(list[1].KeyID, "key-id") {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := DeleteProfile("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(TokenCachePath("prod")); !os.IsNotExist(err) {
		t.Errorf("token cache of a deleted profile left behind: %v", err)
	}
	list, _ = Profiles()
	if len(list) != 1 || list[0].Name != "default" {
		t.Errorf("after delete: %+v", list)
	}
	if err := DeleteProfile("prod"); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("deleting a missing profile: %v", err)
	}
}

func TestMaskKeyID(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"short":            "****",
		"abcdef0123456789": "abcd****6789",
	} {
		if got := MaskKeyID(in); got != want {
			t.Errorf("MaskKeyID(%q) = %q, want %q", in, got, want)
		}
	}
}

func asCLI(err error, target **clerrors.CLIError) bool { return stderrors.As(err, target) }

// .evo.yaml or CLOUDRU_PROJECT_ID replacing the profile's project is
// reported; --project-id is explicit and is not (best practice: no silent
// "active project", cf. gcloud/az which take it silently).
func TestResolve_WarnsWhenProjectOverridesProfile(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "proj-profile"})
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "project_id: proj-dir\n")

	warned := func(cfg Config, want string) bool {
		for _, w := range cfg.Warnings {
			if strings.Contains(w, want) {
				return true
			}
		}
		return false
	}
	cfg, _, err := ResolveWithSources(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "proj-dir" || cfg.ProjectOrigin != OriginProject || !warned(cfg, "proj-profile") {
		t.Errorf(".evo.yaml: project %q from %q, warnings %q", cfg.ProjectID, cfg.ProjectOrigin, cfg.Warnings)
	}

	t.Setenv("CLOUDRU_PROJECT_ID", "proj-env")
	cfg, _, _ = ResolveWithSources(Options{})
	if cfg.ProjectOrigin != OriginEnv || !warned(cfg, "CLOUDRU_PROJECT_ID") {
		t.Errorf("env: origin %q, warnings %q", cfg.ProjectOrigin, cfg.Warnings)
	}

	cfg, _, _ = ResolveWithSources(Options{Flags: map[string]any{"ProjectID": "proj-flag"}})
	if cfg.ProjectOrigin != OriginFlag || warned(cfg, "proj-profile") {
		t.Errorf("flag: origin %q, warnings %q", cfg.ProjectOrigin, cfg.Warnings)
	}

	t.Setenv("CLOUDRU_PROJECT_ID", "proj-profile") // same project: nothing to say
	cfg, _, _ = ResolveWithSources(Options{})
	if warned(cfg, "proj-profile") {
		t.Errorf("same project warned: %q", cfg.Warnings)
	}
}

// resolveConfig merges all configuration layers without the sources; the
// commands use ResolveWithSources.
func resolveConfig(opts Options) (Config, error) {
	cfg, _, err := resolve(opts)
	return cfg, err
}

// image и image_id — одна настройка: побеждает слой выше, оба в одном
// слое — ошибка. image_metadata читается из файла проекта.
func TestResolve_ImageAndImageID(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{ImageID: "img-profile"})
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "image: Ubuntu-24.04\nimage_metadata:\n  admin_email: ops@example.test\n  port: 8080\n")
	cfg, _, err := ResolveWithSources(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Image != "Ubuntu-24.04" || cfg.ImageID != "" {
		t.Errorf("image %q, image_id %q: файл проекта должен побеждать профиль", cfg.Image, cfg.ImageID)
	}
	if cfg.ImageMetadata["admin_email"] != "ops@example.test" || cfg.ImageMetadata["port"] != "8080" {
		t.Errorf("image_metadata = %v", cfg.ImageMetadata)
	}

	cfg, _, err = ResolveWithSources(Options{ProjectFile: project, Flags: map[string]any{"ImageID": "img-flag"}})
	if err != nil || cfg.ImageID != "img-flag" || cfg.Image != "" {
		t.Errorf("флаг: image %q, image_id %q, %v", cfg.Image, cfg.ImageID, err)
	}

	writeFile(t, project, "image: Ubuntu-24.04\nimage_id: img-project\n")
	if _, _, err := ResolveWithSources(Options{ProjectFile: project}); err == nil || !strings.Contains(err.Error(), "image_id") {
		t.Errorf("оба в одном файле: %v", err)
	}
}

// Релизный аудит, A-001: security_group: из .evo.yaml доходит до настроек
// (раньше ключ читался и молча отбрасывался — deploy открывал порты в группе
// по умолчанию).
func TestResolve_SecurityGroupFromProjectFile(t *testing.T) {
	isolate(t)
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "security_group: web-sg\n")
	cfg, sources, err := ResolveWithSources(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecurityGroup != "web-sg" {
		t.Errorf("security_group = %q, want web-sg", cfg.SecurityGroup)
	}
	found := false
	for _, s := range sources {
		if s.Key == "SecurityGroup" {
			found = s.Origin == OriginProject && s.Value == "web-sg"
		}
	}
	if !found {
		t.Errorf("source of SecurityGroup: %v", sources)
	}
}
