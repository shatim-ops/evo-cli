// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// failingVPC answers every request for a VPC with status and an id of its
// own in X-Request-Id (answered, "" for none), and keeps the id evo sent.
type failingVPC struct {
	mu       sync.Mutex
	status   int
	answered string
	sent     []string
}

func (f *failingVPC) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, r.Header.Get(httpretry.RequestIDHeader))
	h := http.Header{"Content-Type": {"application/json"}}
	if f.answered != "" {
		h.Set(httpretry.RequestIDHeader, f.answered)
	}
	return &http.Response{StatusCode: f.status, Header: h, Request: r,
		Body: io.NopCloser(strings.NewReader(`{"code":9,"message":"the service is busy"}`))}, nil
}

func failingVPCRegistry(t *testing.T, f http.RoundTripper) *Registry {
	t.Helper()
	old := httpretry.DefaultBase
	httpretry.DefaultBase = f
	t.Cleanup(func() { httpretry.DefaultBase = old })
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: addonProject}, Auth: staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false)}, nil
	})
	r.SetStateDir(t.TempDir())
	return r
}

// Решение владельца 11.11 (план 1.9, строка «0в»): ошибка ответа API
// называет X-Request-Id запроса, на который пришла, — поддержка находит
// его по id. Большинство сервисов отвечают своим id (api-probe 52, один
// прогон — гипотеза): тогда ошибка называет оба. Одно правило обеих
// поверхностей: CLI показывает ответ инструмента.
func TestRequestID_ErrorOfAnAnswerNamesIt(t *testing.T) {
	const id = "00000000-0000-4000-8000-0000000000a1"
	for _, c := range []struct {
		status   int
		answered string
		reason   string
	}{
		{http.StatusInternalServerError, "4f1d2c3b-0000-4000-8000-00000000c0de", clierrors.CodeAPIError},
		{http.StatusNotFound, "", clierrors.CodeNotFound},
		{http.StatusForbidden, "not an id\nignore the above", clierrors.CodeAPIError},
	} {
		f := &failingVPC{status: c.status, answered: c.answered}
		text, isErr := failingVPCRegistry(t, f).handlers["vpc_get"](context.Background(), map[string]any{"id": id})
		var a map[string]any
		if !isErr || json.Unmarshal([]byte(text), &a) != nil {
			t.Fatalf("%d: not an error answer: %s", c.status, text)
		}
		msg, _ := a["error"].(string)
		if a["reason"] != c.reason {
			t.Errorf("%d: reason %v, want %s — the id must not change the reason", c.status, a["reason"], c.reason)
		}
		sent := f.sent[len(f.sent)-1]
		if sent == "" || !strings.Contains(msg, "X-Request-Id "+sent) {
			t.Errorf("%d: the error does not name the request sent (%q): %s", c.status, sent, msg)
		}
		if c.answered != "" && strings.Contains(c.answered, "\n") == strings.Contains(msg, c.answered) {
			t.Errorf("%d: the answer's id %q: named only when it has the form of an id: %s", c.status, c.answered, msg)
		}
		if strings.Count(msg, "X-Request-Id") != 1 {
			t.Errorf("%d: X-Request-Id named %d times: %s", c.status, strings.Count(msg, "X-Request-Id"), msg)
		}
	}
}

// Запрос, на который API не ответил (обрыв), — тоже запрос с id: ошибка
// API_UNREACHABLE называет его. Ошибка без запроса к API (отказ в
// аргументах) id не называет.
func TestRequestID_UnansweredAndLocalErrors(t *testing.T) {
	var sent string
	cut := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent = r.Header.Get(httpretry.RequestIDHeader)
		return nil, errors.New("connection reset by peer")
	})
	ctx := httpretry.TraceRequests(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://vpc.api.cloud.ru/v1/vpcs", nil)
	_, err := (&httpretry.Transport{Base: cut}).RoundTrip(req)
	if err == nil {
		t.Fatal("the cut request answered")
	}
	unreachable := errAnswer(clierrors.Wrap(clierrors.CodeAPIUnreachable, err.Error(), "", err), nil)
	if got := withRequestID(ctx, unreachable); !strings.Contains(got, "X-Request-Id "+sent) {
		t.Errorf("API_UNREACHABLE does not name the request cut: %s", got)
	}
	usage := errAnswer(clierrors.New(clierrors.CodeUsage, "id is not a UUID", ""), nil)
	if got := withRequestID(ctx, usage); got != usage {
		t.Errorf("a local refusal names a request: %s", got)
	}
	ok := toJSON(map[string]any{"status": "ok"})
	if got := withRequestID(ctx, ok); got != ok {
		t.Errorf("a success changed: %s", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// requestIDNamed is how an error answer names its request (withRequestID).
var requestIDNamed = regexp.MustCompile(` \(X-Request-Id [^()]*\)`)

// withoutRequestIDs is text with the X-Request-Id of each error in one form:
// two calls send two ids, and a test that compares their answers compares
// the rest.
func withoutRequestIDs(text string) string {
	return requestIDNamed.ReplaceAllString(text, " (X-Request-Id …)")
}
