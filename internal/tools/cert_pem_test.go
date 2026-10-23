// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// cert_import takes a PEM as it is, not only Base64 of it (Q17, decision
// 21.10; additive): a value that starts with "-----BEGIN" is encoded before
// it goes to Certificate Manager, which takes Base64; Base64 goes as given.
// The PEM here is a placeholder, no real key.
func TestCertImport_RawPEM(t *testing.T) {
	const (
		cert = "-----BEGIN CERTIFICATE-----\nZmFrZS1jZXJ0\n-----END CERTIFICATE-----\n"
		key  = "-----BEGIN PRIVATE KEY-----\nZmFrZS1rZXk=\n-----END PRIVATE KEY-----\n"
	)
	inB64 := base64.StdEncoding.EncodeToString([]byte(cert))
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "certificatemanager.api.cloud.ru" {
			return false
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"certificates":[]}`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"`+auditUUID+`","name":"byo"}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	// Pasted from a terminal: blank space before the PEM is not sent.
	m, text, isErr := callNamed(t, r, "cert_import", map[string]any{"name": "byo", "server_certificate": "  \n" + cert,
		"private_key": key, "intermediate_certificate": inB64, "confirmed": true})
	if isErr || m["status"] != "imported" {
		t.Fatalf("cert_import: %s", text)
	}
	body, _ := f.posted("certificatemanager.api.cloud.ru")
	var sent map[string]string
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	for field, want := range map[string]string{
		"serverCertificate":       base64.StdEncoding.EncodeToString([]byte(cert)),
		"privateKey":              base64.StdEncoding.EncodeToString([]byte(key)),
		"intermediateCertificate": inB64,
	} {
		if sent[field] != want {
			t.Errorf("%s = %q, want %q", field, sent[field], want)
		}
	}
}
