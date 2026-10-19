// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// QA «путь новичка»: что видит человек, который впервые запускает evo, —
// без ключа, без проекта, с ключом без роли, за корпоративным прокси.
// Облако подменено транспортом: IAM выдаёт токен (или отказывает с
// iamStatus), остальные сервисы отвечают status.

const newcomerProject = "00000000-0000-4000-8000-000000000001"

type newcomerRT struct {
	iamStatus int   // 0: a token
	status    int   // the answer of every other service; 0: empty lists
	err       error // the transport error of every service but IAM

	mu   sync.Mutex
	reqs []string // the requests other than IAM
}

func (c *newcomerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	body, st := `{"items":[],"total":0}`, http.StatusOK
	if strings.HasSuffix(r.URL.Path, "/iam/token") {
		if c.iamStatus != 0 {
			st, body = c.iamStatus, `{"message":"bad key"}`
		} else {
			body = `{"access_token":"tok","expires_in":3600}`
		}
	} else {
		c.mu.Lock()
		c.reqs = append(c.reqs, r.Method+" "+r.URL.Host+r.URL.Path)
		c.mu.Unlock()
		if c.err != nil {
			return nil, c.err
		}
		switch {
		case c.status != 0:
			st, body = c.status, `{"code":"denied","message":"denied by test"}`
		case strings.Contains(r.URL.Path, "availability-zones"):
			body = `[]` // an array, not a page
		}
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Retry-After", "0")
	return &http.Response{StatusCode: st, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func (c *newcomerRT) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.reqs...)
}

// newcomer isolates a run (setupCLI), answers with rt and makes the proxy
// of every request "none" whatever HTTPS_PROXY the test runs with.
func newcomer(t *testing.T, rt *newcomerRT) {
	t.Helper()
	setupCLI(t)
	oldRT, oldProxy := http.DefaultTransport, clerrors.ProxyFunc
	http.DefaultTransport = rt
	clerrors.ProxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
	t.Cleanup(func() { http.DefaultTransport, clerrors.ProxyFunc = oldRT, oldProxy })
}

type jsonErr struct {
	Code                  int
	Reason, Message, Hint string
}

// lastJSONError is the {"error": …} line of a --json run's stderr.
func lastJSONError(t *testing.T, stderr string) jsonErr {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	var got struct {
		Error jsonErr `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatalf("stderr is not a JSON error: %v\n%s", err, stderr)
	}
	return got.Error
}

func ru(t *testing.T) {
	t.Helper()
	old := i18n.Lang()
	i18n.SetLang("ru")
	t.Cleanup(func() { i18n.SetLang(old) })
}

// Ни ключа, ни проекта: 'evo vm list' говорил «не задан проект», и после
// 'evo project use' человек получал второй отказ — «не задан ключ». Первым
// называется ключ (первый шаг, 'evo configure' задаёт и проект); код тот
// же — 2, в облако ничего не уходит.
func TestNewcomer_NoKeyNoProject_KeyFirst(t *testing.T) {
	ru(t)
	rt := &newcomerRT{}
	newcomer(t, rt)
	for _, args := range [][]string{{"vm", "list"}, {"k8s", "list"}, {"tools", "call", "dns_zones"}} {
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, args...)...)
		e := lastJSONError(t, stderr)
		if code != clerrors.ExitAuth || e.Reason != clerrors.CodeAuthRequired || !strings.Contains(e.Hint, "evo configure") {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
	}
	if reqs := rt.requests(); len(reqs) > 0 {
		t.Errorf("requests without a key: %v", reqs)
	}
}

// Ключ есть, проекта нет: команды без проекта работают, команды проекта
// отказывают с подсказкой про 'evo project use'. 'evo deploy' отказывал
// текстом «не задан CLOUDRU_PROJECT_ID» без 'evo project use' и
// '--project-id', и только после запросов каталога флейворов.
func TestNewcomer_KeyWithoutProject(t *testing.T) {
	ru(t)
	rt := &newcomerRT{}
	newcomer(t, rt)
	saveProfile(t, "default", "")
	for _, args := range [][]string{{"whoami"}, {"project", "list"}, {"image", "list"}, {"vm", "zones"}, {"vm", "flavors"}} {
		if code, stdout, stderr := runCLI(t, "", args...); code != 0 {
			t.Errorf("%v without a project: exit %d\n%s%s", args, code, stdout, stderr)
		}
	}
	// whoami gives the hint the refusing commands give.
	_, _, stderr := runCLI(t, "", "whoami")
	if !strings.Contains(stderr, clerrors.ProjectRequired().Hint) {
		t.Errorf("whoami hint: %s", stderr)
	}

	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(rt.requests())
	code, _, stderr := runCLI(t, "", "--json", "deploy", "--dry-run")
	e := lastJSONError(t, stderr)
	want := clerrors.ProjectRequired()
	if code != clerrors.ExitAuth || e.Reason != clerrors.CodeConfigInvalid || e.Message != want.Message || e.Hint != want.Hint {
		t.Errorf("deploy without a project: exit %d, %+v", code, e)
	}
	if reqs := rt.requests()[before:]; len(reqs) > 0 {
		t.Errorf("deploy asked the cloud before refusing: %v", reqs)
	}
}

// Ключ отвергнут IAM (401/403): AUTH_FAILED, код 2, подсказка о том, что
// ключ мог истечь или быть удалён.
func TestNewcomer_IAMRejectsKey(t *testing.T) {
	ru(t)
	for _, st := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		newcomer(t, &newcomerRT{iamStatus: st})
		saveProfile(t, "default", newcomerProject)
		code, _, stderr := runCLI(t, "", "--json", "whoami")
		e := lastJSONError(t, stderr)
		if code != clerrors.ExitAuth || e.Reason != clerrors.CodeAuthFailed ||
			!strings.HasPrefix(e.Hint, "Проверьте Key ID и секрет") || !strings.Contains(e.Hint, "ключ мог быть удалён или истечь") {
			t.Errorf("IAM %d: exit %d, %+v", st, code, e)
		}
	}
}

// Токен выдан, а сервис отказывает. 401 и 403 значат разное: ключ (отозван,
// истёк) или роль сервисного аккаунта в проекте. У compute — AUTH_EXPIRED /
// AUTH_FAILED (код 2), у остальных сервисов код прежний — 5 (API_ERROR,
// docs/exit-codes.md), но подсказка теперь есть: раньше «mk8s API 403: …»
// приходило без единого слова о том, что делать.
func TestNewcomer_ServiceRejectsToken(t *testing.T) {
	ru(t)
	for _, tt := range []struct {
		status int
		args   []string
		exit   int
		reason string
		hint   string
	}{
		{http.StatusUnauthorized, []string{"vm", "list"}, clerrors.ExitAuth, clerrors.CodeAuthExpired, clerrors.TokenRejectedHint()},
		{http.StatusForbidden, []string{"vm", "list"}, clerrors.ExitAuth, clerrors.CodeAuthFailed, clerrors.NoRoleHint()},
		{http.StatusUnauthorized, []string{"k8s", "list"}, clerrors.ExitAPI, clerrors.CodeAPIError, clerrors.TokenRejectedHint()},
		{http.StatusForbidden, []string{"k8s", "list"}, clerrors.ExitAPI, clerrors.CodeAPIError, clerrors.NoRoleHint()},
		{http.StatusForbidden, []string{"pg", "list"}, clerrors.ExitAPI, clerrors.CodeAPIError, clerrors.NoRoleHint()},
		{http.StatusForbidden, []string{"secret", "list"}, clerrors.ExitAPI, clerrors.CodeAPIError, clerrors.NoRoleHint()},
		{http.StatusForbidden, []string{"tools", "call", "dns_zones"}, clerrors.ExitAPI, clerrors.CodeAPIError, clerrors.NoRoleHint()},
	} {
		newcomer(t, &newcomerRT{status: tt.status})
		saveProfile(t, "default", newcomerProject)
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, tt.args...)...)
		e := lastJSONError(t, stderr)
		if code != tt.exit || e.Reason != tt.reason || e.Hint != tt.hint {
			t.Errorf("%d %v: exit %d, %+v", tt.status, tt.args, code, e)
		}
	}
	// The two hints name different things to fix.
	if !strings.Contains(clerrors.NoRoleHint(), "«Сервисные аккаунты»") || !strings.Contains(clerrors.NoRoleHint(), "docs/auth.md") ||
		!strings.Contains(clerrors.TokenRejectedHint(), "истечь") {
		t.Errorf("hints: %q / %q", clerrors.NoRoleHint(), clerrors.TokenRejectedHint())
	}
}

// Корпоративный прокси с подменой сертификата: и compute, и прочие
// сервисы дают API_UNREACHABLE (код 1, как раньше) с подсказкой про
// SSL_CERT_FILE. Раньше compute писал «проверьте сеть», а mk8s — голый
// текст Go без подсказки.
func TestNewcomer_UntrustedCertificate(t *testing.T) {
	ru(t)
	for _, args := range [][]string{{"vm", "list"}, {"k8s", "list"}, {"tools", "call", "dns_zones"}} {
		rt := &newcomerRT{err: x509.UnknownAuthorityError{}}
		newcomer(t, rt)
		saveProfile(t, "default", newcomerProject)
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, args...)...)
		e := lastJSONError(t, stderr)
		if code != clerrors.ExitGeneral || e.Reason != clerrors.CodeAPIUnreachable ||
			!strings.HasPrefix(e.Hint, "Сертификат TLS у ") || !strings.Contains(e.Hint, "SSL_CERT_FILE") {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
		// Not retried: the certificate stays untrusted.
		if reqs := rt.requests(); len(reqs) != 1 {
			t.Errorf("%v: %d requests, want 1: %v", args, len(reqs), reqs)
		}
	}
}

// evo init — обычно вторая команда новичка: код ошибки не попадает в текст
// («список зон: AUTH_EXPIRED: …» было).
func TestNewcomer_InitKeepsCodeOutOfTheMessage(t *testing.T) {
	ru(t)
	newcomer(t, &newcomerRT{status: http.StatusUnauthorized})
	saveProfile(t, "default", newcomerProject)
	code, _, stderr := runCLI(t, "", "--json", "init", "--non-interactive")
	e := lastJSONError(t, stderr)
	if code != clerrors.ExitAuth || e.Reason != clerrors.CodeAuthExpired || e.Message != "список зон: API Cloud.ru отклонил токен (401)" {
		t.Errorf("exit %d, %+v", code, e)
	}
}

// CLOUDRU_CONFIG_DIR указывает на файл (например, на config.yaml): было
// «open …/config.yaml: not a directory» и «проверьте права на файл».
func TestNewcomer_ConfigDirIsAFile(t *testing.T) {
	ru(t)
	newcomer(t, &newcomerRT{})
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("project_id: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfigDir, file)
	code, _, stderr := runCLI(t, "", "--json", "whoami")
	e := lastJSONError(t, stderr)
	if code != clerrors.ExitAuth || e.Reason != clerrors.CodeConfigInvalid ||
		e.Message != file+" — файл, а не каталог" || !strings.HasPrefix(e.Hint, "CLOUDRU_CONFIG_DIR задаёт каталог профилей") {
		t.Errorf("exit %d, %+v", code, e)
	}
	// A directory under the file: the file is named, not the directory.
	t.Setenv(config.EnvConfigDir, filepath.Join(file, "evo"))
	if _, _, stderr := runCLI(t, "", "--json", "whoami"); lastJSONError(t, stderr).Message != file+" — файл, а не каталог" {
		t.Errorf("nested: %s", stderr)
	}
	t.Setenv(config.EnvConfigDir, file)
	_, stdout, _ := runCLI(t, "", "--json", "doctor")
	if strings.Contains(stdout, "not a directory") || !strings.Contains(stdout, "файл, а не каталог") {
		t.Errorf("doctor: %s", stdout)
	}
}
