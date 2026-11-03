// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Прогон Grok с LLM на 1.6.0 (G2-6, P9): после обрыва за прокси сервер MCP
// держал мёртвое соединение HTTP/2, и каждый вызов Compute ждал 60 с до
// API_UNREACHABLE, пока сервер не перезапустили; CLI в новом процессе
// работал. Базовый транспорт проверяет соединение HTTP/2 пингом и рвёт его,
// если ответа нет.
func TestBaseTransport_ChecksHTTP2Connections(t *testing.T) {
	tr := NewBaseTransport()
	if tr.HTTP2 == nil || tr.HTTP2.SendPingTimeout <= 0 || tr.HTTP2.PingTimeout <= 0 {
		t.Fatalf("HTTP/2 health check is off: %+v", tr.HTTP2)
	}
	// What http.DefaultTransport gave stays: the proxy from the environment
	// and HTTP/2.
	if tr.Proxy == nil || !tr.ForceAttemptHTTP2 {
		t.Errorf("the base transport lost the proxy or HTTP/2 of http.DefaultTransport")
	}
	if NewBaseTransport() == tr {
		t.Error("NewBaseTransport hands out one transport: a caller that changes its TLS would change everyone's")
	}
}

// Every client of the package goes over one base, read at the call: the
// retrying Transport and UserAgent alike, so a test (or anything else) that
// answers for the cloud replaces DefaultBase alone.
func TestDefaultBase_UnderTransportAndUserAgent(t *testing.T) {
	var calls int
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})
	old := DefaultBase
	DefaultBase = rt
	t.Cleanup(func() { DefaultBase = old })

	for _, c := range []*http.Client{NewClient(0), {Transport: UserAgent(nil)}} {
		resp, err := c.Get("https://compute.api.cloud.ru/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if calls != 2 {
		t.Errorf("DefaultBase served %d of 2 requests", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
