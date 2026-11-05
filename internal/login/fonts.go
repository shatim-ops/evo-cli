// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"embed"
	"net/http"
	"strings"
)

// fontFiles are the four faces of SB Sans the pages use (the macket of
// «Дизайн», handoff/fonts). SB Sans is a typeface of the Sber ecosystem,
// used in evo by permission of Cloud.ru (the owner's decision 05.11) and
// not under the Apache License 2.0: fonts/NOTICE.md and NOTICE.
//
//go:embed fonts/*.woff2
var fontFiles embed.FS

// fontCache is the cache of a face: the one answer of the loopback that is
// not no-store.
const fontCache = "private, max-age=86400"

// serveFont answers GET /fonts/<face>.woff2 from the binary: the pages
// load nothing from another origin (CSP font-src 'self'). It reports
// whether the request was for a font. It runs before the loopback's lock:
// a face is static and touches no state of the login — a font that ever
// reads the session moves under the lock. A face is cached (private, a
// day) on purpose: it carries no state and no token; a refusal stays
// no-store, as every other answer.
func serveFont(w http.ResponseWriter, r *http.Request) bool {
	name, ok := strings.CutPrefix(r.URL.Path, "/fonts/")
	if !ok || r.Method != http.MethodGet {
		return false
	}
	b, err := fontFiles.ReadFile("fonts/" + name)
	if err != nil || strings.Contains(name, "/") || !strings.HasSuffix(name, ".woff2") {
		refuse(w, http.StatusNotFound)
		return true
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", fontCache)
	_, _ = w.Write(b)
	return true
}
