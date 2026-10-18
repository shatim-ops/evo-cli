// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Два configure одновременно (два терминала, параллельные шаги CI) читали
// оба файла, меняли свой профиль и записывали всё целиком: последний
// затирал профиль первого. Воспроизведено бинарём: из 20 параллельных
// «configure --profile pN» сохранилось 8, все 20 ответили успехом.
func TestSaveProfile_ConcurrentWritersKeepEveryProfile(t *testing.T) {
	isolate(t)
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("p%02d", i)
			errs <- SaveProfile(name, Profile{ProjectID: "proj-" + name},
				Credentials{KeyID: "key-" + name, Secret: "secret-" + name})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%02d", i)
		p, c, ok, err := LoadProfile(name)
		if err != nil || !ok || p.ProjectID != "proj-"+name || c.Secret != "secret-"+name {
			t.Errorf("profile %s lost: %+v %+v %v %v", name, p, c, ok, err)
		}
	}
	// The lock file stays, as state.lock does: the lock is the OS's, held
	// on the open file, not the file's existence.
}

// Файл замка, оставшийся от упавшего процесса (или от версии до v1.0.3),
// configure не блокирует; живой замок — ошибка конфигурации с подсказкой,
// а не вечное ожидание. Живой замок не отбирается, сколько бы ему ни было
// лет (R2-4): замок — блокировка ОС, не возраст файла.
func TestProfileLock_LeftoverAndBusy(t *testing.T) {
	isolate(t)
	oldWait := lockWait
	lockWait = 200 * time.Millisecond
	t.Cleanup(func() { lockWait = oldWait })

	lock := filepath.Join(Dir(), lockName)
	writeFile(t, lock, "12345\n")
	if err := SaveProfile("default", Profile{}, Credentials{KeyID: "k", Secret: "s"}); err != nil {
		t.Fatalf("leftover lock file blocks configure: %v", err)
	}

	unlock, err := lockProfiles()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	err = SaveProfile("default", Profile{}, Credentials{KeyID: "k", Secret: "s"})
	if exitCode(err) != clerrors.ExitAuth || !strings.Contains(err.Error(), lock) {
		t.Fatalf("held lock: %v", err)
	}
}

// Имя профиля из файла не проверялось при удалении: профиль
// "../../victim" в credentials.yaml (руками, чужим инструментом) — и
// «configure delete» удалял victim.json вне каталога кэша.
func TestDeleteProfile_NeverRemovesFilesOutsideTheCache(t *testing.T) {
	dir := isolate(t)
	victim := filepath.Join(filepath.Dir(dir), "victim.json")
	writeFile(t, victim, "{}")
	name := "x/../../../victim"
	if p := TokenCachePath(name); filepath.Clean(p) != filepath.Clean(victim) {
		t.Fatalf("test premise: %s != %s", p, victim)
	}
	writeFile(t, CredentialsPath(), fmt.Sprintf("profiles:\n  %q:\n    key_id: k\n    secret: s\n", name))

	if err := DeleteProfile(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("file outside the cache removed: %v", err)
	}
	if _, _, ok, _ := LoadProfile(name); ok {
		t.Error("profile still in the file")
	}
}

// Ключ из .env с CRLF или с пробелами по краям: IAM отвечал 401 «IAM
// rejected the credentials», хотя ключ верный.
func TestResolve_EnvCredentialsAreTrimmed(t *testing.T) {
	isolate(t)
	t.Setenv("CLOUDRU_KEY_ID", " key-id\r")
	t.Setenv("CLOUDRU_SECRET", "secret\r\n")
	cfg, err := resolveConfig(Options{})
	if err != nil || cfg.KeyID != "key-id" || cfg.Secret != "secret" {
		t.Errorf("got %q %q %v", cfg.KeyID, cfg.Secret, err)
	}

	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	for _, v := range []string{" id:sec\r", "id : sec", "id:sec\n"} {
		t.Setenv("CLOUDRU_API_KEY", v)
		cfg, err := resolveConfig(Options{})
		if err != nil || cfg.KeyID != "id" || cfg.Secret != "sec" {
			t.Errorf("CLOUDRU_API_KEY=%q: got %q %q %v", v, cfg.KeyID, cfg.Secret, err)
		}
	}
	for _, v := range []string{"id:", ":sec", " : ", "nocolon"} {
		t.Setenv("CLOUDRU_API_KEY", v)
		if _, err := resolveConfig(Options{}); exitCode(err) != clerrors.ExitAuth {
			t.Errorf("CLOUDRU_API_KEY=%q: %v, want exit 2", v, err)
		}
	}
}

// FuzzProfileFiles: любые config.yaml/credentials.yaml и имя профиля —
// без паники; ошибка всегда код 2; файл кэша токена всегда внутри CacheDir.
func FuzzProfileFiles(f *testing.F) {
	f.Add("profiles:\n  default:\n    project_id: p\n", "profiles:\n  default:\n    key_id: k\n    secret: s\n", "default")
	f.Add("", "", "default")
	f.Add("profiles: [", "profiles: {}", "prod")
	f.Add("profiles:\n  default:\n    disk_size: lots\n", "", "default")
	f.Add("profiles: ~\n", "profiles:\n  \"x/../../../victim\":\n    key_id: k\n", "x/../../../victim")
	f.Add("a: &a [x,x]\nb: [*a,*a]\nprofiles: {}\n", "\xff\xfe", "a.b-c_d")
	f.Add("profiles:\n  default: 5\n", "profiles:\n  default:\n    s3: {access_key: a}\n", "default")
	f.Add("\ufeffprofiles:\r\n  default:\r\n    project_id: p\r\n", "", "default")
	f.Fuzz(func(t *testing.T, settings, creds, profile string) {
		root := t.TempDir()
		dir := filepath.Join(root, "cfg")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		victim := filepath.Join(root, "victim.json")
		if err := os.WriteFile(victim, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvConfigDir, dir)
		t.Setenv("CLOUDRU_PROFILE", "")
		if err := os.WriteFile(filepath.Join(dir, settingsName), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, credentialsName), []byte(creds), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveConfig(Options{Profile: profile}); err != nil && exitCode(err) != clerrors.ExitAuth {
			t.Fatalf("Resolve: exit %d for %v", exitCode(err), err)
		}
		if _, err := Profiles(); err != nil && exitCode(err) != clerrors.ExitAuth {
			t.Fatalf("Profiles: exit %d for %v", exitCode(err), err)
		}
		if ValidateProfileName(profile) == nil {
			rel, err := filepath.Rel(CacheDir(), TokenCachePath(profile))
			if err != nil || strings.HasPrefix(rel, "..") || strings.ContainsAny(rel, `/\`) {
				t.Fatalf("token cache of %q outside the cache dir: %s", profile, rel)
			}
		}
		_ = DeleteProfile(profile)
		if _, err := os.Stat(victim); err != nil {
			t.Fatalf("DeleteProfile(%q) removed a file outside the config dir", profile)
		}
	})
}

// FuzzSaveProfileRoundTrip: что configure сохранил, то и читается обратно.
// Иначе один «неудобный» символ в секрете ломает все команды профиля.
func FuzzSaveProfileRoundTrip(f *testing.F) {
	f.Add("p", "key", "secret", "ru.AZ-1")
	f.Add("", "a:b", "#not-a-comment", "- x")
	f.Add("null", "~", "yes", "0x1F")
	f.Add("\"quoted\"", "'single'", "multi\nline", "tab\there")
	f.Add("\x00", "\xff", "\u2028", "  lead")
	f.Fuzz(func(t *testing.T, project, keyID, secret, region string) {
		for _, v := range []string{project, keyID, secret, region} {
			if strings.TrimSpace(v) != v {
				return // configure trims every answer
			}
		}
		t.Setenv(EnvConfigDir, t.TempDir())
		p := Profile{ProjectID: project, Region: region}
		c := Credentials{KeyID: keyID, Secret: secret}
		if err := SaveProfile("default", p, c); err != nil {
			return // refusing to store is fine; storing garbage is not
		}
		gp, gc, _, err := LoadProfile("default")
		if err != nil {
			t.Fatalf("saved profile does not load: %v", err)
		}
		if gp != p || gc.KeyID != c.KeyID || gc.Secret != c.Secret {
			t.Fatalf("round trip changed the profile: %+v %+v -> %+v %+v", p, c, gp, gc)
		}
	})
}

// Приоритет флаг > env > проект > профиль — для каждого поля, снимая слои
// по одному сверху.
func TestResolve_PrecedenceEveryField(t *testing.T) {
	num := func(base int) func(string) any {
		return func(l string) any {
			return base + map[string]int{"profile": 1, "project": 2, "env": 3, "flag": 4}[l]
		}
	}
	str := func(prefix string) func(string) any { return func(l string) any { return prefix + l } }
	fields := []struct {
		field, env, yamlKey string
		inProfile           bool
		val                 func(layer string) any
	}{
		{"ProjectID", "CLOUDRU_PROJECT_ID", "project_id", true, str("proj-")},
		{"Region", "CLOUDRU_REGION", "region", true, str("az-")},
		{"Flavor", "CLOUDRU_FLAVOR", "flavor", true, str("fl-")},
		{"ImageID", "CLOUDRU_IMAGE_ID", "image_id", true, str("img-")},
		{"DiskSize", "CLOUDRU_DISK_SIZE", "disk_size", true, num(10)},
		{"Timeout", "CLOUDRU_TIMEOUT", "timeout", true, num(20)},
		{"ComposeFile", "CLOUDRU_COMPOSE_FILE", "compose_file", false, str("compose-")},
		{"ProjectName", "CLOUDRU_PROJECT_NAME", "project_name", false, str("name-")},
	}
	layers := []string{"flag", "env", "project", "profile"}
	for _, f := range fields {
		for top := range layers {
			active := map[string]bool{}
			for _, l := range layers[top:] {
				active[l] = true
			}
			want := layers[top]
			if want == "profile" && !f.inProfile {
				continue
			}
			t.Run(f.field+"/"+want, func(t *testing.T) {
				isolate(t)
				if active["profile"] && f.inProfile {
					writeFile(t, SettingsPath(), fmt.Sprintf("profiles:\n  default:\n    %s: %v\n", f.yamlKey, f.val("profile")))
				}
				project := filepath.Join(t.TempDir(), ".evo.yaml")
				if active["project"] {
					writeFile(t, project, fmt.Sprintf("%s: %v\n", f.yamlKey, f.val("project")))
				}
				if active["env"] {
					t.Setenv(f.env, fmt.Sprint(f.val("env")))
				}
				flags := map[string]any{}
				if active["flag"] {
					flags[f.field] = f.val("flag")
				}
				_, sources, err := ResolveWithSources(Options{ProjectFile: project, Flags: flags})
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range sources {
					if s.Key == f.field && (fmt.Sprint(s.Value) != fmt.Sprint(f.val(want)) || s.Origin != want) {
						t.Errorf("%s = %v from %s, want %v from %s", f.field, s.Value, s.Origin, f.val(want), want)
					}
				}
			})
		}
	}
}
