// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// answerLine is a line of the file of EVO_RECORD_ANSWERS.
type answerLine struct {
	Request string            `json:"request"`
	Status  int               `json:"status"`
	Form    string            `json:"form"`
	Paths   map[string]string `json:"paths"`
}

func readAnswers(t *testing.T, path string) []answerLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []answerLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l answerLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("a line that is not JSON: %q", sc.Text())
		}
		out = append(out, l)
	}
	return out
}

// План 1.9, строка «Шов записи в транспорте» («миры из живых ответов»,
// шаг 3): с EVO_RECORD_ANSWERS каждый ответ API, который пришёл по сети,
// дописывает в файл свою форму — запрос без значений (UUID и числа пути —
// {id}, без query), статус, пути полей тела с видами значений. Значений в
// файле нет: ни строк, ни ключей объектов не формы идентификатора, ни
// заголовков. Клиент получает ответ целиком, как без записи.
func TestRecordAnswers_FormsWithoutValues(t *testing.T) {
	const id = "3f2a1c0e-0000-4000-8000-00000000a1b2"
	bodies := []struct {
		status int
		ctype  string
		body   string
	}{
		{http.StatusServiceUnavailable, "application/json", `{"code":14,"message":"busy now"}`},
		{http.StatusOK, "application/json", `{"id":"` + id + `","name":"web-secret-name","labels":{"team":"x","my label":"y"},"n":2,"zones":[]}`},
		{http.StatusOK, "text/html", `<html>gateway</html>`},
		{http.StatusNoContent, "", ``},
	}
	i := 0
	var got []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b := bodies[i]
		i++
		h := http.Header{"Location": {"https://x.invalid/" + id}}
		if b.ctype != "" {
			h.Set("Content-Type", b.ctype)
		}
		return &http.Response{StatusCode: b.status, Header: h, Body: io.NopCloser(strings.NewReader(b.body)), Request: r}, nil
	})
	path := filepath.Join(t.TempDir(), "answers.jsonl")
	t.Setenv(EnvRecordAnswers, path)
	// The compute client: the retrying Transport over UserAgent — one line
	// per answer that came over the network, not one per layer.
	tr := &Transport{Base: UserAgent(base), Backoff: time.Millisecond}
	// The HTML page goes to a POST: the 502 evo makes of it is not repeated.
	for _, u := range []string{
		"GET https://compute.api.cloud.ru/api/v1/vms/" + id + "?project_id=" + id,
		"POST https://s3.cloud.ru/bucket-x?list-type=2",
		"GET https://dns.api.cloud.ru/v1/zones/42/records",
	} {
		method, u, _ := strings.Cut(u, " ")
		req, _ := http.NewRequest(method, u, nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got = append(got, string(b))
	}
	if got[0] != bodies[1].body || got[2] != "" {
		t.Errorf("the client got another body than the API's: %q", got)
	}

	lines := readAnswers(t, path)
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4 (the 503, its repeat, S3, DNS): %+v", len(lines), lines)
	}
	want := []answerLine{
		{Request: "GET https://compute.api.cloud.ru/api/v1/vms/{id}", Status: 503, Form: "json", Paths: map[string]string{"code": "number", "message": "name"}},
		{Request: "GET https://compute.api.cloud.ru/api/v1/vms/{id}", Status: 200, Form: "json", Paths: map[string]string{
			"id": "uuid", "name": "name", "labels.team": "name", "labels.*": "name", "n": "number", "zones": "array"}},
		// The gateway's page as it came, not the 502 evo makes of it.
		{Request: "POST https://s3.cloud.ru/bucket-x", Status: 200, Form: "not-json", Paths: map[string]string{}},
		{Request: "GET https://dns.api.cloud.ru/v1/zones/{id}/records", Status: 204, Form: "empty", Paths: map[string]string{}},
	}
	for k, w := range want {
		g := lines[k]
		if g.Request != w.Request || g.Status != w.Status || g.Form != w.Form || len(g.Paths) != len(w.Paths) {
			t.Errorf("line %d: %+v, want %+v", k, g, w)
			continue
		}
		for p, kind := range w.Paths {
			if g.Paths[p] != kind {
				t.Errorf("line %d: %s is %q, want %q", k, p, g.Paths[p], kind)
			}
		}
	}
	raw, _ := os.ReadFile(path)
	for _, v := range []string{id, "web-secret-name", "busy now", "my label", "gateway", "Location", "project_id", "list-type"} {
		if strings.Contains(string(raw), v) {
			t.Errorf("a value in the file: %q", v)
		}
	}
	if st, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("the file has mode %04o, want 0600", st.Mode().Perm())
	}
}

// Без переменной ничего не пишется; файл, который не открыть, не роняет
// вызов и не меняет ответ.
func TestRecordAnswers_OffAndUnwritable(t *testing.T) {
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"a":1}`)), Request: r}, nil
	})
	dir := t.TempDir()
	t.Setenv(EnvRecordAnswers, "")
	for _, rec := range []string{"", filepath.Join(dir, "no", "such", "dir", "a.jsonl")} {
		t.Setenv(EnvRecordAnswers, rec)
		req, _ := http.NewRequest(http.MethodGet, "https://vpc.api.cloud.ru/v1/vpcs", nil)
		resp, err := UserAgent(base).RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != `{"a":1}` {
			t.Errorf("record %q: the body changed: %q", rec, b)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

// Ссылка на месте файла — не путь записи в её цель (R-67).
func TestRecordAnswers_NotThroughALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "answers.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvRecordAnswers, link)
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"a":1}`)), Request: r}, nil
	})
	req, _ := http.NewRequest(http.MethodGet, "https://vpc.api.cloud.ru/v1/vpcs", nil)
	resp, err := UserAgent(base).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if b, _ := os.ReadFile(target); len(b) != 0 {
		t.Errorf("written through the link: %q", b)
	}
}
