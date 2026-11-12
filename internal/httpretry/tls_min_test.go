// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The minimum of TLS is evo's own declaration, not the default of the Go
// release it is built with (release plan 1.9, «минимум TLS 1.2»): every
// client of evo — the APIs of Cloud.ru, IAM, Object Storage — stands on
// NewBaseTransport.
func TestBaseTransport_DeclaresTLS12(t *testing.T) {
	cfg := NewBaseTransport().TLSClientConfig
	if cfg == nil || cfg.MinVersion != MinTLS || MinTLS != tls.VersionTLS12 {
		t.Fatalf("the base transport declares no minimum of TLS 1.2: %+v", cfg)
	}
	if base, ok := DefaultBase.(*http.Transport); !ok || base.TLSClientConfig == nil || base.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("DefaultBase declares no minimum of TLS 1.2")
	}
}

// A server that speaks nothing above TLS 1.1 is refused at the handshake;
// one that speaks TLS 1.2 is served.
func TestBaseTransport_RefusesTLS11(t *testing.T) {
	for _, c := range []struct {
		name string
		max  uint16
		ok   bool
	}{
		{"TLS 1.1", tls.VersionTLS11, false},
		{"TLS 1.2", tls.VersionTLS12, true},
	} {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		srv.TLS = &tls.Config{MinVersion: tls.VersionTLS10, MaxVersion: c.max} //nolint:gosec // the old server of the test
		srv.StartTLS()
		tr := NewBaseTransport()
		tr.TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
		resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want served = %v", c.name, err, c.ok)
		}
		tr.CloseIdleConnections()
		srv.Close()
	}
}
