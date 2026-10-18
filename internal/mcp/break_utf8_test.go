// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A byte that is not UTF-8 inside a JSON string was not a parse error:
// encoding/json replaced it with U+FFFD and image_list searched for that
// other query, answering success (break r3b, R3B-4). Now the line is a
// parse error and nothing goes to the cloud.
func TestUTF8_InvalidLineNotSent(t *testing.T) {
	old := http.DefaultTransport
	var urls []string
	http.DefaultTransport = roundTripper(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.RequestURI())
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		h.Set("Retry-After", "0")
		body := `{"items":[],"total":0}`
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })

	s := New(func(map[string]any) (*engine.Engine, error) {
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: http.DefaultTransport}, "")
		return &engine.Engine{
			Config: config.Config{ProjectID: "00000000-0000-4000-8000-000000000001"},
			Auth:   breakToken{},
			Cloud:  cloud,
		}, nil
	}, "test")
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"image_list","arguments":{"query":"web`)
	raw = append(raw, 0xff, '"', '}', '}', '}', '\n')
	out := &bytes.Buffer{}
	if err := s.Run(bytes.NewReader(raw), out); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if strings.Contains(u, "EF%BF%BD") || strings.Contains(u, "\uFFFD") {
			t.Fatalf("query byte 0xFF was sent as U+FFFD: %s\n%s", u, out.String())
		}
	}
	if strings.Contains(out.String(), `"isError":false`) && len(urls) > 0 {
		t.Fatalf("success after a substituted query: %s", out.String())
	}
}

type breakToken struct{}

func (breakToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{Value: "fake"}, nil
}
