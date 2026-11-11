// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Gate rc.1 of 1.8, Grok F1 (2 of 2, clean main c888c95): «Выдать доступ»
// and «Отмена» were refused. The pages carried <meta name="referrer"
// content="no-referrer">, and a document's policy from meta overrides the
// Referrer-Policy header (same-origin, L23): the form posted with
// «Origin: null», and fromPage answered 403 tForeign. A page of the
// loopback names no referrer policy of its own, or the one the header
// names — one policy, the header's.
func TestPages_ReferrerPolicyIsTheHeaders(t *testing.T) {
	l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://"+l.host+"/nothing", nil)
	r.Host = l.host
	l.ServeHTTP(w, r)
	header := strings.ToLower(w.Header().Get("Referrer-Policy"))
	if header == "" {
		t.Fatal("the loopback sets no Referrer-Policy")
	}
	meta := regexp.MustCompile(`(?is)<meta\s+[^>]*name\s*=\s*"referrer"[^>]*content\s*=\s*"([^"]*)"`)
	// The source of every page: they share the head.
	for _, m := range meta.FindAllStringSubmatch(pagesHTML, -1) {
		if strings.ToLower(strings.TrimSpace(m[1])) != header {
			t.Errorf("pages.html: <meta name=\"referrer\" content=%q> overrides the header %q: under no-referrer a form posts «Origin: null» and fromPage refuses it (Grok F1)", m[1], header)
		}
	}
}
