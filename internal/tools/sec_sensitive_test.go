// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// SEC-4: secret_value отдаёт расшифрованный секрет только с confirmed=true —
// как cert_private_key; без него — confirmation_required, и секрет не
// запрашивается: secret_value не идёт в облако вовсе, cert_private_key
// читает только сам сертификат (W6, D35: сертификата нет — отказ до
// вопроса). message говорит, что будет раскрыто (открытый текст); как дать
// согласие, говорит транспорт: MCP — approval, CLI — --yes (REQ-5),
// поэтому confirmed в message нет.
func TestSensitiveTools_NeedConfirmed(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"secret_value", map[string]any{"path": "app/db"}},
		{"cert_private_key", map[string]any{"certificate_id": "00000000-0000-0000-0000-000000000001"}},
	} {
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Host != "certificatemanager.api.cloud.ru" || strings.Contains(r.URL.Path, "private_key") {
				return false
			}
			_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000001","name":"web"}`))
			return true
		}}
		r := apiFakeRegistry(t, f)
		for _, confirmed := range []any{nil, false} {
			args := map[string]any{}
			for k, v := range c.args {
				args[k] = v
			}
			if confirmed != nil {
				args["confirmed"] = confirmed
			}
			m, text, isErr := callNamed(t, r, c.tool, args)
			msg, _ := m["message"].(string)
			if !isErr || m["status"] != "confirmation_required" || !strings.Contains(msg, "plaintext") || strings.Contains(msg, "confirmed") {
				t.Errorf("%s confirmed=%v: %s", c.tool, confirmed, text)
			}
		}
		for _, call := range f.calls {
			if c.tool == "secret_value" || strings.Contains(call, "private_key") {
				t.Errorf("%s: %s before consent", c.tool, call)
			}
		}
	}
}

func TestSecretValue_ConfirmedReturnsValue(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/v2/version/") {
			return false
		}
		_, _ = w.Write([]byte(`{"payload":"` + base64.StdEncoding.EncodeToString([]byte("fake-value")) + `"}`))
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "secret_value", map[string]any{"path": "app/db", "confirmed": true})
	if isErr || m["value"] != "fake-value" || m["path"] != "app/db" {
		t.Fatalf("secret_value confirmed=true: %s", text)
	}
}

// SEC-4: инструменты, чей ответ несёт написанное рабочей нагрузкой (логи,
// вывод kubectl, заметки helm, объекты S3), предупреждают агента, что это
// недоверенные данные. Остальные описания этой фразы не несут. Признак —
// объявление UntrustedOutput, фраза описания следует из него.
func TestUntrustedOutputNote(t *testing.T) {
	want := map[string]bool{"logs": true, "ca_logs": true, "s3_get": true, "k8s_kubectl": true, "k8s_helm": true}
	for _, tl := range New(nil).Tools() {
		if tl.UntrustedOutput != want[tl.Name] {
			t.Errorf("%s: UntrustedOutput=%v, want %v", tl.Name, tl.UntrustedOutput, want[tl.Name])
		}
		if has := strings.HasSuffix(tl.Description, " "+untrustedOutputNote); has != tl.UntrustedOutput {
			t.Errorf("%s: note=%v, UntrustedOutput=%v", tl.Name, has, tl.UntrustedOutput)
		}
		if n := strings.Count(tl.Description, untrustedOutputNote); n > 1 {
			t.Errorf("%s: the note %d times", tl.Name, n)
		}
		delete(want, tl.Name)
	}
	for name := range want {
		t.Errorf("%s: no such tool", name)
	}
}
