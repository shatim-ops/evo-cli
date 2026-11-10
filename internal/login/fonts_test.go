// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// The fonts of the pages come from the binary, by the loopback, and only
// from it: every url() of pages.html is an embedded face, the CSP lets
// fonts come from the loopback alone, and nothing else is served.
func TestFonts_ServedByTheLoopback(t *testing.T) {
	l := &loopback{s: &session{}, host: "127.0.0.1:1"}
	get := func(host, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		r.Host = host
		w := httptest.NewRecorder()
		l.ServeHTTP(w, r)
		return w
	}
	urls := regexp.MustCompile(`url\((/fonts/[^)]+)\)`).FindAllStringSubmatch(pagesHTML, -1)
	if len(urls) != 4 {
		t.Fatalf("pages.html names %d fonts, want the 4 faces", len(urls))
	}
	for _, u := range urls {
		w := get(l.host, u[1])
		if h := w.Header(); w.Code != http.StatusOK || h.Get("Content-Type") != "font/woff2" || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Cache-Control") != fontCache || h.Get("Referrer-Policy") != "same-origin" || !bytes.HasPrefix(w.Body.Bytes(), []byte("wOF2")) {
			t.Errorf("%s: %d %q %q %q", u[1], w.Code, h.Get("Content-Type"), h.Get("X-Content-Type-Options"), h.Get("Cache-Control"))
		}
	}
	for _, p := range []string{"/fonts/missing.woff2", "/fonts/../fonts.go", "/fonts/NOTICE.md"} {
		if w := get(l.host, p); w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	if w := get("evil.example:1", urls[0][1]); w.Code != http.StatusForbidden {
		t.Errorf("another Host: %d", w.Code)
	}
	w := httptest.NewRecorder()
	writePage(w, http.StatusOK, cancelledPage())
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "font-src 'self'") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP %q", csp)
	}
}
