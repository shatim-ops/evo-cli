package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(q *http.Request) (*http.Response, error) { return r(q) }

func TestOnlyExactPhysicalDeleteOnce(t *testing.T) {
	for _, code := range []int{200, 401, 429, 0} {
		path := filepath.Join(t.TempDir(), "wire")
		calls := 0
		f := fence{roundTrip(func(q *http.Request) (*http.Response, error) {
			calls++
			if code == 0 {
				return nil, errors.New("lost")
			}
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(""))}, nil
		}), "owned", "project", path, time.Now().Add(time.Minute)}
		for range 2 {
			resp, _ := f.RoundTrip(httptest.NewRequest(http.MethodDelete, "https://containers.api.cloud.ru/v2/containers/owned?projectId=project", nil))
			if resp != nil {
				resp.Body.Close()
			}
		}
		b, err := os.ReadFile(path)
		if calls != 1 || err != nil || !strings.HasPrefix(string(b), "attempt ") {
			t.Errorf("HTTP %d: one physical attempt and its receipt required", code)
		}
	}
}

func TestForeignScopeAndExpiredLeaseNeverReachNetwork(t *testing.T) {
	f := fence{roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request to cloud")
		return nil, nil
	}), "owned", "project", filepath.Join(t.TempDir(), "wire"), time.Now().Add(time.Minute)}
	for _, u := range []string{
		"https://containers.api.cloud.ru/v2/containers/foreign?projectId=project",
		"https://containers.api.cloud.ru/v2/containers/owned?projectId=other",
		"https://containers.api.cloud.ru/v2/containers/owned?projectId=project&projectId=other",
		"https://containers.api.cloud.ru/v2/containers/owned:stop?projectId=project",
		"https://other.invalid/v2/containers/owned?projectId=project",
	} {
		if _, err := f.RoundTrip(httptest.NewRequest(http.MethodDelete, u, nil)); err == nil {
			t.Fatal("accepted out-of-scope mutation")
		}
	}
	f.until = time.Now().Add(-time.Second)
	if _, err := f.RoundTrip(httptest.NewRequest(http.MethodDelete, "https://containers.api.cloud.ru/v2/containers/owned?projectId=project", nil)); err == nil {
		t.Fatal("accepted expired authorization")
	}
}
