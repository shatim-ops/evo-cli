// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The delete a person consents to is the one evo sends: the preview of
// secret_delete and of cert_delete names the force the DELETE carries —
// the argument, or the default when it is not given (true for a secret,
// false for a certificate) — and what it means. One value goes to both
// (Астра, the full audit of 14.11, finding 04: the previews of the plain
// and the forced delete were the same, the DELETEs differed).
func TestDeletePreview_NamesTheForceTheDeleteSends(t *testing.T) {
	for _, c := range []struct {
		tool, arg, id, host string
		def                 bool
	}{
		{"secret_delete", "path", "app/db", "secretmanager.api.cloud.ru", true},
		{"cert_delete", "id", "00000000-0000-4000-8000-0000000000c1", "certificatemanager.api.cloud.ru", false},
	} {
		for _, force := range []any{nil, true, false} {
			want := c.def
			if f, ok := force.(bool); ok {
				want = f
			}
			var sent []string
			own := func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != c.host {
					return false
				}
				switch r.Method {
				case http.MethodGet:
					if c.tool == "secret_delete" {
						_, _ = io.WriteString(w, `{"secret":{"id":"s-1","name":"app/db"}}`)
					} else {
						_, _ = io.WriteString(w, `{"id":"`+c.id+`","name":"site"}`)
					}
				case http.MethodDelete:
					sent = append(sent, r.URL.Query().Get("force"))
					_, _ = io.WriteString(w, `{}`)
				default:
					return false
				}
				return true
			}
			r := apiFakeRegistry(t, &apiFake{own: own})
			args := map[string]any{c.arg: c.id}
			if force != nil {
				args["force"] = force
			}
			name := fmt.Sprintf("%s force=%v", c.tool, force)
			m, text, _ := callNamed(t, r, c.tool, args)
			if m["status"] != "confirmation_required" {
				t.Fatalf("%s: preview: %s", name, text)
			}
			if m["force"] != want || !strings.Contains(fmt.Sprint(m["message"]), fmt.Sprintf("force=%v", want)) {
				t.Errorf("%s: the preview names force %v, message %q; want force %v in both", name, m["force"], m["message"], want)
			}
			args["confirmed"] = true
			if _, text, isErr := callNamed(t, r, c.tool, args); isErr {
				t.Fatalf("%s: confirmed: %s", name, text)
			}
			if wantQ := map[bool]string{true: "true", false: ""}[want]; len(sent) != 1 || sent[0] != wantQ {
				t.Errorf("%s: DELETE sent force %q; want %q — the value of the preview", name, sent, wantQ)
			}
		}
	}
}
