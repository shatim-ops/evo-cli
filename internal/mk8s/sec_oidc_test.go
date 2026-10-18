// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordTransport struct{ hosts []string }

func (r *recordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hosts = append(r.hosts, req.URL.Scheme+"://"+req.URL.Host)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"id_token":"t"}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// The token URL comes from the kubeconfig the API returns, and the request
// carries the access key secret: it must go to Cloud.ru IAM over https only.
func TestFetchOIDCToken_OnlyTrustedIAMHosts(t *testing.T) {
	rt := &recordTransport{}
	orig := oidcClient
	oidcClient = &http.Client{Transport: rt}
	t.Cleanup(func() { oidcClient = orig })

	for _, u := range []string{
		"",
		"https://id.cloud.ru/auth/system/openid/token",
		"https://auth.iam.cloud.ru/auth/system/openid/token",
		"https://attacker.example/token",
		"http://id.cloud.ru/auth/system/openid/token",
		"https://id.cloud.ru.attacker.example/token",
		"https://id.cloud.ru:8443/token",
		"https://user@id.cloud.ru/token",
		"http://169.254.169.254/latest",
	} {
		rt.hosts = nil
		if _, err := FetchOIDCToken(context.Background(), u, "k", "s"); err != nil {
			t.Fatalf("%q: %v", u, err)
		}
		got := rt.hosts[0]
		if got != "https://id.cloud.ru" && got != "https://auth.iam.cloud.ru" {
			t.Errorf("iam-url %q: the access key went to %s", u, got)
		}
	}
}

// redirectTransport answers the first request with 307 to another host.
type redirectTransport struct{ hosts []string }

func (r *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hosts = append(r.hosts, req.URL.Host)
	h := http.Header{}
	h.Set("Location", "https://attacker.example/token")
	return &http.Response{StatusCode: http.StatusTemporaryRedirect, Body: io.NopCloser(strings.NewReader("")),
		Header: h, Request: req}, nil
}

// 307/308 repeat the POST with the same body — the access key secret — at
// the Location: even the trusted IAM host must not forward it elsewhere.
func TestFetchOIDCToken_RedirectDoesNotResendSecret(t *testing.T) {
	rt := &redirectTransport{}
	orig := oidcClient
	oidcClient = &http.Client{Transport: rt, CheckRedirect: orig.CheckRedirect}
	t.Cleanup(func() { oidcClient = orig })

	_, err := FetchOIDCToken(context.Background(), DefaultOIDCTokenURL, "k", "s")
	if len(rt.hosts) != 1 {
		t.Fatalf("the request followed the redirect: %v", rt.hosts)
	}
	if err == nil {
		t.Fatal("a redirect was taken for a token")
	}
}

// statusTransport answers with the given statuses in turn, then 200.
type statusTransport struct {
	codes []int
	calls int
}

func (s *statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls++
	code, body := 200, `{"id_token":"t"}`
	if s.calls <= len(s.codes) {
		code, body = s.codes[s.calls-1], "<html>Bad Gateway</html>"
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{}, Request: req}, nil
}

// Живой прогон 18.10: k8s_helm упал на «OIDC token HTTP 502: <html>…»,
// хотя обмены до него в том же прогоне проходили. Обмен идемпотентен:
// 5xx повторяется (до oidcAttempts раз), 4xx — сразу ошибка.
func TestFetchOIDCToken_Retries5xx(t *testing.T) {
	orig, origDelay := oidcClient, oidcRetryDelay
	t.Cleanup(func() { oidcClient, oidcRetryDelay = orig, origDelay })
	oidcRetryDelay = 0

	for _, tc := range []struct {
		codes []int
		calls int
		ok    bool
	}{
		{[]int{502}, 2, true},
		{[]int{503, 504}, 3, true},
		{[]int{502, 502, 502}, oidcAttempts, false},
		{[]int{400}, 1, false},
		{[]int{401}, 1, false},
	} {
		rt := &statusTransport{codes: tc.codes}
		oidcClient = &http.Client{Transport: rt, CheckRedirect: orig.CheckRedirect}
		tok, err := FetchOIDCToken(context.Background(), DefaultOIDCTokenURL, "k", "s")
		if rt.calls != tc.calls || (err == nil) != tc.ok || (tc.ok && tok != "t") {
			t.Errorf("%v: %d requests, token %q, err %v; want %d requests, ok=%v", tc.codes, rt.calls, tok, err, tc.calls, tc.ok)
		}
	}
}
