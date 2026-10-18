// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package stack

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Значения секретов и тела объектов S3 из манифеста нужны, только пока шаг
// не выполнен. После этого они живут в Secret Manager / Object Storage, а на
// диске (~/.evo/stack-jobs/*.json) оставались открытым текстом
// навсегда — в том числе после stack_destroy.

const (
	secretValue = "s3cr3t-db-password-0123456789"
	s3Body      = "API_TOKEN=tok-0123456789abcdef"
)

func secJob(t *testing.T) *Job {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return Plan(Manifest{
		Name:    "app",
		Secrets: []SecretSpec{{Path: "app/db", Value: secretValue}},
		S3:      []S3Spec{{Bucket: "b", Key: ".env", Content: s3Body}},
	})
}

func jobFile(t *testing.T, j *Job) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(jobsDir(), j.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func stepByKey(j *Job, key string) *Step {
	for _, s := range j.Steps {
		if s.Key == key {
			return s
		}
	}
	return nil
}

func TestJobSave_PendingSecretKeptForApply(t *testing.T) {
	j := secJob(t)
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := stepByKey(got, "secret:0").Args["value"].(string); v != secretValue {
		t.Fatalf("значение ещё не созданного секрета потеряно: %q", v)
	}
	if v, _ := stepByKey(got, "s3:0").Args["content"].(string); v != s3Body {
		t.Fatalf("тело ещё не загруженного объекта потеряно: %q", v)
	}
}

func TestJobSave_DoneStepsDropSecretValues(t *testing.T) {
	j := secJob(t)
	stepByKey(j, "secret:0").Status = "done"
	stepByKey(j, "s3:0").Status = "done"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	data := jobFile(t, j)
	for _, leak := range []string{secretValue, s3Body} {
		if strings.Contains(data, leak) {
			t.Fatalf("на диске осталось %q после выполнения шага:\n%s", leak, data)
		}
	}
	// Путь секрета и ключ объекта нужны stack_destroy.
	got, err := LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Secrets[0].Path != "app/db" || got.Manifest.S3[0].Key != ".env" {
		t.Fatalf("stack_destroy не найдёт что удалять: %+v %+v", got.Manifest.Secrets, got.Manifest.S3)
	}
}

func TestJobSave_DestroyedJobDropsEverything(t *testing.T) {
	j := secJob(t)
	j.Status = "destroyed"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	data := jobFile(t, j)
	for _, leak := range []string{secretValue, s3Body} {
		if strings.Contains(data, leak) {
			t.Fatalf("на диске осталось %q после stack_destroy", leak)
		}
	}
}

func TestJobSave_PrivateModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права Unix")
	}
	j := secJob(t)
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(jobsDir())
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m&0o077 != 0 {
		t.Fatalf("каталог stack-jobs с правами %04o: имена и манифесты видны другим", m)
	}
	fst, err := os.Stat(filepath.Join(jobsDir(), j.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if m := fst.Mode().Perm(); m != 0o600 {
		t.Fatalf("job с правами %04o", m)
	}
}
