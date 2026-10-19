// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors_test

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

const fallback = "check the network"

// noProxy makes the proxy of every request "none", whatever the environment
// of the test says (HTTPS_PROXY of a CI runner).
func noProxy(t *testing.T) {
	t.Helper()
	old := clierrors.ProxyFunc
	clierrors.ProxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
	t.Cleanup(func() { clierrors.ProxyFunc = old })
}

// Путь новичка в корпоративной сети: прокси подменяет сертификат HTTPS, и
// IAM «недоступен» без объяснения. Настоящая ошибка проверки сертификата
// (TLS-сервер с неизвестным корнем) даёт подсказку про SSL_CERT_FILE на
// обоих языках, и повторять такой запрос незачем.
func TestNetworkHint_UntrustedCertificate(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	noProxy(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	// The default roots, as evo's clients have: the test server's are unknown.
	_, err := (&http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}).Get(srv.URL)
	if err == nil {
		t.Fatal("the test server's certificate was trusted")
	}
	if !clierrors.TLSUntrusted(err) {
		t.Errorf("TLSUntrusted(%v) = false", err)
	}
	for _, tt := range []struct{ lang, want string }{
		{"ru", "Сертификат TLS у 127.0.0.1 не прошёл проверку (x509:"},
		{"en", "The TLS certificate of 127.0.0.1 is not trusted (x509:"},
	} {
		i18n.SetLang(tt.lang)
		hint := clierrors.NetworkHint(err, fallback)
		if !strings.HasPrefix(hint, tt.want) || !strings.Contains(hint, "SSL_CERT_FILE") {
			t.Errorf("%s: hint %q", tt.lang, hint)
		}
	}
}

// Прокси из HTTPS_PROXY не отвечает: подсказка называет прокси (без логина
// и пароля из URL) и NO_PROXY, а не «проверьте сеть».
func TestNetworkHint_ProxyRefused(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens there now
	proxy, _ := url.Parse("http://user:p4ss@" + addr)
	old := clierrors.ProxyFunc
	clierrors.ProxyFunc = http.ProxyURL(proxy)
	t.Cleanup(func() { clierrors.ProxyFunc = old })
	tr := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer tr.CloseIdleConnections()
	_, err = (&http.Client{Transport: tr}).Get("https://iam.api.cloud.ru/")
	if err == nil {
		t.Fatal("no error through a closed proxy")
	}
	hint := clierrors.NetworkHint(err, fallback)
	if !strings.Contains(hint, "прокси "+addr) || !strings.Contains(hint, "NO_PROXY") || strings.Contains(hint, "p4ss") {
		t.Errorf("hint %q", hint)
	}
	if clierrors.TLSUntrusted(err) {
		t.Error("a refused proxy is not a certificate error")
	}
	// The proxy is not known (another client), but the transport says it
	// was one.
	clierrors.ProxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
	if hint := clierrors.NetworkHint(err, fallback); !strings.HasPrefix(hint, "Запрос шёл через прокси (HTTPS_PROXY") {
		t.Errorf("typed proxyconnect: %q", hint)
	}
}

func TestNetworkHint_OtherErrors(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	noProxy(t)
	dns := &url.Error{Op: "Post", URL: "https://iam.api.cloud.ru/api/v1/auth/token",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "iam.api.cloud.ru", Err: "no such host", IsNotFound: true}}}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"dns", dns, "Имя iam.api.cloud.ru не найдено в DNS"},
		{"wrapped dns", fmt.Errorf("mk8s request failed: %w", dns), "Имя iam.api.cloud.ru не найдено в DNS"},
		{"no roots", &url.Error{Op: "Get", URL: "https://x/", Err: x509.SystemRootsError{}}, "В системе не найдены корневые сертификаты"},
		{"reset", &url.Error{Op: "Get", URL: "https://x/", Err: stderrors.New("connection reset by peer")}, fallback},
		{"cancelled", &url.Error{Op: "Get", URL: "https://x/", Err: context.Canceled}, fallback},
		{"nil", nil, fallback},
	} {
		if got := clierrors.NetworkHint(tt.err, fallback); !strings.HasPrefix(got, tt.want) {
			t.Errorf("%s: %q, want prefix %q", tt.name, got, tt.want)
		}
	}
}

// Prefix: контекст перед сообщением, код и подсказка — от исходной ошибки,
// кода в тексте нет («список зон: AUTH_EXPIRED: …» было).
func TestPrefix_KeepsCodeOutOfTheText(t *testing.T) {
	inner := clierrors.New(clierrors.CodeAuthExpired, "token rejected (401)", "run evo doctor")
	err := clierrors.Prefix("list zones: ", inner)
	var ce *clierrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clierrors.CodeAuthExpired || ce.Message != "list zones: token rejected (401)" || ce.Hint != "run evo doctor" {
		t.Fatalf("got %#v", ce)
	}
	if !stderrors.Is(err, inner) || clierrors.ExitCode(err) != clierrors.ExitAuth {
		t.Errorf("chain or exit code lost: %v", err)
	}
	plain := clierrors.Prefix("list zones: ", stderrors.New("boom"))
	if plain.Error() != "list zones: boom" {
		t.Errorf("plain: %q", plain)
	}
}
