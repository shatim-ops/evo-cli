// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// refusing answers 401 to every token in refused, 200 to any other, and
// records the tokens it was sent.
type refusing struct {
	mu      sync.Mutex
	refused map[string]bool
	sent    []string
}

func (r *refusing) RoundTrip(req *http.Request) (*http.Response, error) {
	tok := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	r.mu.Lock()
	r.sent = append(r.sent, tok)
	refused := r.refused[tok]
	r.mu.Unlock()
	status := http.StatusOK
	if refused {
		status = http.StatusUnauthorized
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}, Request: req}, nil
}

// A token a service refuses after the machine slept goes once more with
// the one its provider renews (process backlog 1.6, priority 1): one
// refusal does not end the request; the renewed token refused too is the
// key's, and the 401 is the answer; a body that cannot be sent again is
// not.
func TestRefusedTokenGoesOnceMoreWithTheRenewedOne(t *testing.T) {
	renew := func(_ context.Context, tok string) (string, bool) { return tok + "-new", true }
	OnTokenRenewed(renew)
	t.Cleanup(func() { OnTokenRenewed(nil) })
	for _, c := range []struct {
		name    string
		refused []string
		body    io.Reader
		replay  bool
		status  int
		sent    string
	}{
		{"one refusal", []string{"old"}, nil, false, 200, "old old-new"},
		{"refused again: the key", []string{"old", "old-new"}, nil, false, 401, "old old-new"},
		{"a write sent again", []string{"old"}, strings.NewReader(`{"a":1}`), true, 200, "old old-new"},
		{"a body that cannot be sent again", []string{"old"}, strings.NewReader(`{"a":1}`), false, 401, "old"},
	} {
		base := &refusing{refused: map[string]bool{}}
		for _, r := range c.refused {
			base.refused[r] = true
		}
		req, _ := http.NewRequest(http.MethodPost, "https://svc.example.invalid/v1/x", nil)
		if c.body != nil {
			req.Body = io.NopCloser(c.body)
			if c.replay {
				req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(`{"a":1}`)), nil }
			}
		}
		req.Header.Set("Authorization", "Bearer old")
		resp, err := (&Transport{Base: base}).RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.status || strings.Join(base.sent, " ") != c.sent {
			t.Errorf("%s: status %d, tokens sent %q; want %d, %q", c.name, resp.StatusCode, base.sent, c.status, c.sent)
		}
	}
}
