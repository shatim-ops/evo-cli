// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"unicode"
)

// The log of the HTTP server never holds a token: the server's own, nor
// one a client presented, in any form a line could carry it — whatever
// Authorization says, a valid token, a wrong one, a malformed header, and
// a tool call behind a valid one (Астра #2383: a mutant that logged the
// presented token without "Bearer " went past every test). The logger is
// a plain one, with no filter of evo's log: the server itself must not
// hand a token to it.
func TestHTTP_LogHoldsNoToken(t *testing.T) {
	const presented = "s3cr3t-presented-token-0123456789"
	logs := &syncBuffer{}
	ts, _ := newHTTPTest(t, HTTPOptions{
		BearerToken: testToken,
		Logger:      slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"ca_start","arguments":{"name":"app-sentinel-arg"}}}`
	refused := 0
	for _, auth := range []string{
		"", "Bearer " + presented, "bearer " + presented, "BEARER " + presented, presented,
		"Basic " + presented, "Bearer" + presented, "Bearer  " + presented + " ", "Bearer " + strings.ToUpper(testToken),
		"Bearer " + testToken, "bearer " + testToken, "Bearer  " + testToken,
	} {
		for _, body := range []string{initBody, call} {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				refused++
			}
		}
	}
	log := logs.String()
	// The check reads a log the server wrote: a refusal and a tool call.
	if refused == 0 || !strings.Contains(log, "unauthorized request") || !strings.Contains(log, "tool call") {
		t.Fatalf("the log has no refusal or no tool call (refused %d): the check reads nothing\n%s", refused, log)
	}
	for _, secret := range []string{testToken, presented} {
		for _, form := range tokenForms(secret) {
			if strings.Contains(log, form) {
				t.Errorf("the log holds %q, a form of a token:\n%s", form, log)
			}
		}
	}
}

// tokenForms are the forms a line could carry a token in: as it is, in
// another case, in Base64 and in hex (of the token and of its header), and
// any 8 characters of it in a row with a letter among them — a part of the
// token, not a run of digits a time or a port could hold («Риск», 367).
func tokenForms(s string) []string {
	forms := []string{s, strings.ToLower(s), strings.ToUpper(s),
		base64.StdEncoding.EncodeToString([]byte(s)),
		hex.EncodeToString([]byte(s)), hex.EncodeToString([]byte("Bearer " + s))}
	for i := 0; i+8 <= len(s); i++ {
		if w := s[i : i+8]; strings.IndexFunc(w, unicode.IsLetter) >= 0 {
			forms = append(forms, w)
		}
	}
	return forms
}
