// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// consoleCard is a card of a VM whose console fields carry markers: plain,
// with an escaped quote inside, and nested as escaped JSON in a message.
func consoleCard(marker string) string {
	var b strings.Builder
	b.WriteString(`{"id":"vm-1","name":"web","state":"running"`)
	for i, f := range consoleFields {
		fmt.Fprintf(&b, `,%q:"https://console.example/%d-%s\"x\"%s"`, f, i, marker, marker)
	}
	fmt.Fprintf(&b, `,"detail":"{\"vnc_url\": \"wss://n/%s\", \"state\":\"x\"}"}`, marker)
	return b.String()
}

// The body of a refusal of the platform and the unparsed body of a path of
// Compute reach the text of an error and StatusError.Body; the values of the
// console fields of a VM in them never do — whole or cut at the 64 KiB
// bound of an error body (vm_console, ТЗ §5.1; «Риск», conclusion 240, У1).
func TestConsoleFieldsLeaveNoErrorText(t *testing.T) {
	const marker = "CONSOLEMARK"
	card := consoleCard(marker)
	// A card cut inside the value of a console field at the bound.
	pre := `{"state":"running","vnc_url":"https://c/`
	cut := pre + strings.Repeat("z", maxErrorBody-len(pre)-len(marker)-100) + marker + strings.Repeat("A", 4096) + `"}`
	cases := []struct {
		name   string
		status int
		body   string
		call   func(a *CloudRuAdapter) error
	}{
		{"422 GET /vms/{id}", 422, card, func(a *CloudRuAdapter) error { _, err := a.GetVMDetail(context.Background(), "vm-1"); return err }},
		{"403 GET /vms/{id}", 403, card, func(a *CloudRuAdapter) error { _, err := a.GetVMDetail(context.Background(), "vm-1"); return err }},
		{"422 PUT /vms/{id}", 422, card, func(a *CloudRuAdapter) error { return a.SetVMFlavor(context.Background(), "vm-1", "f-1") }},
		{"403 PUT /vms/{id}", 403, card, func(a *CloudRuAdapter) error { return a.SetVMFlavor(context.Background(), "vm-1", "f-1") }},
		{"500 GET /vms/{id}", 500, card, func(a *CloudRuAdapter) error { _, err := a.GetVMDetail(context.Background(), "vm-1"); return err }},
		{"422 GET /vms/{id}, cut at the bound", 422, cut, func(a *CloudRuAdapter) error { _, err := a.GetVMDetail(context.Background(), "vm-1"); return err }},
		{"201 POST /vms of an unknown shape", 201, `{"vm":` + card + `}`, func(a *CloudRuAdapter) error {
			_, err := a.CreateVM(context.Background(), CreateVMRequest{Name: "web"})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			a := NewCloudRuAdapter(srv.Client(), srv.URL)
			a.SetToken("t")
			a.Backoff = 1
			err := c.call(a)
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), marker) {
				t.Errorf("the text of the error holds a console field: %s", err)
			}
			var se *StatusError
			if errors.As(err, &se) && strings.Contains(se.Body, marker) {
				t.Errorf("StatusError.Body holds a console field: %s", se.Body)
			}
			if !strings.Contains(err.Error(), "running") {
				t.Errorf("the rest of the body is lost: %s", err)
			}
		})
	}
}

// hideConsole hides the value of a console field at any escape depth and
// keeps the text around it; a value it cannot see the end of is hidden to
// the end of the body.
func TestHideConsole(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"vnc_url":"wss://a","state":"running"}`, `{"vnc_url":"hidden","state":"running"}`},
		{`{"vnc_url" : "a\\","x":1}`, `{"vnc_url" : "hidden","x":1}`},
		{`{"vnc_url":"a\"b","x":1}`, `{"vnc_url":"hidden","x":1}`},
		{`{"m":"{\"remote_console_url\":\"wss://a\\\"b\\\\\",\"x\":1}"}`, `{"m":"{\"remote_console_url\":\"hidden\",\"x\":1}"}`},
		{`{"m":"{\\\"vnc_ws\\\":\\\"wss://a\\\"}"}`, `{"m":"{\\\"vnc_ws\\\":\\\"hidden\\\"}"}`},
		{`{"vnc_url":{"u":"wss://a"},"x":1}`, `{"vnc_url":"hidden","x":1}`},
		{`{"vnc_url":null}`, `{"vnc_url":"hidden"}`},
		{`{"vnc_url":"wss://cut`, `{"vnc_url":"hidden"`},
		{`{"vnc_url":`, `{"vnc_url":"hidden"`},
		{`{"vnc_u`, `{"vnc_u`},
		// Text outside JSON: field=value and field: value («Процесс · 1.9
		// (5)», TestSecrets_VMConsoleFieldsOutsideJSON).
		{`not json: vnc_url=wss://a`, `not json: vnc_url=hidden`},
		{`refused: vnc_url=wss://a?t=1&u=2 remote_console_url=https://b state=x`, `refused: vnc_url=hidden remote_console_url=hidden state=x`},
		{`VNC_WS: 'wss://a b'`, `VNC_WS: hidden`},
		{`remote_console_ws = "wss://a", x`, `remote_console_ws = hidden, x`},
		// A key in single quotes (a Python dict in a message; «Риск», 244).
		{`{'vnc_url': 'wss://a', 'x': 1}`, `{'vnc_url': hidden, 'x': 1}`},
		{`{"remote_console_protocol":"vnc","vnc_url":"a"}`, `{"remote_console_protocol":"hidden","vnc_url":"hidden"}`},
		// «Риск», conclusion 244, residuals 1 and 4: a key in another case;
		// an object whose strings hold a bracket and a comma.
		{`{"VNC_URL":"wss://a","x":1}`, `{"VNC_URL":"hidden","x":1}`},
		{`{"vnc_url":{"url":"}x,wss://a"},"x":1}`, `{"vnc_url":"hidden","x":1}`},
		{`{"m":"{\"vnc_url\":{\"u\":\"],wss://a\"},\"x\":1}"}`, `{"m":"{\"vnc_url\":\"hidden\",\"x\":1}"}`},
	} {
		if got := hideConsole([]byte(c.in)); got != c.want {
			t.Errorf("hideConsole(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

// Астра #1946: JSON escaped into a string may put an escaped LF, CR or TAB
// between a key and its colon; the value is hidden there too.
func TestHideConsole_EscapedWhitespaceBeforeTheColon(t *testing.T) {
	for _, ws := range []string{`\n`, `\r`, `\t`, `\n \t`} {
		body := `{"message":"{\"vnc_url\"` + ws + `:` + ws + `\"MARK\",\"detail\":\"KEEP\"}"}`
		got := hideConsole([]byte(body))
		if strings.Contains(got, "MARK") || !strings.Contains(got, "KEEP") {
			t.Errorf("%q: %s", ws, got)
		}
	}
}

// Fail-closed (release plan 1.9, Астра #1945, #1946): a body that still
// names a console field whose value evo could not hide — an escape deeper
// than it reads, a name in \u escapes, the name in free text — does not go
// out at all; the error says why. A body whose every console field is
// hidden goes out as before.
func TestHideConsole_FailsClosed(t *testing.T) {
	deep := `{"vnc_url":"MARK","detail":"KEEP"}`
	for d := 0; d < 8; d++ {
		b, _ := json.Marshal(deep)
		deep = `{"nested":` + string(b) + `}`
	}
	for name, body := range map[string]string{
		"depth 8":        deep,
		"name in \\u":    `{"\u0076nc_url":"MARK"}`,
		"name in text":   `{"message":"bad vnc_url MARK given"}`,
		"upper, in text": `refused: VNC_URL MARK`,
		// «Риск», conclusion 248, У1: \u of the name escaped twice and three
		// times.
		"\\u at depth 2":  `{"m":"{\\\"vnc\\u005furl\\\":\\\"MARK\\\"}"}`,
		"\\u at depth 3":  `{"m":"{\\\\\\\"vnc\\\\u005furl\\\\\\\":\\\\\\\"MARK\\\\\\\"}"}`,
		"escaped unicode": `{"m":"{\"remote_console\u005furl\":\"MARK\"}"}`,
	} {
		got := hideConsole([]byte(body))
		if strings.Contains(got, "MARK") {
			t.Errorf("%s: the value goes out: %s", name, got)
		}
		if got != consoleBodyWithheld() {
			t.Errorf("%s: want the whole body withheld, got %s", name, got)
		}
	}
	// Depth 7 is read and hidden in place: the rest of the body stays.
	seven := `{"vnc_url":"MARK","detail":"KEEP"}`
	for d := 0; d < 7; d++ {
		b, _ := json.Marshal(seven)
		seven = `{"nested":` + string(b) + `}`
	}
	if got := hideConsole([]byte(seven)); strings.Contains(got, "MARK") || !strings.Contains(got, "KEEP") {
		t.Errorf("depth 7: %s", got)
	}
}

// A body of backslashes up to the bound is read in linear time: hideConsole
// runs on every refusal.
func TestHideConsole_LongRunOfBackslashes(t *testing.T) {
	body := []byte(`{"m":"` + strings.Repeat(`\`, maxErrorBody) + `"}`)
	done := make(chan struct{})
	go func() { hideConsole(body); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second): // wall-clock: a guard against a quadratic read, far above a linear one
		t.Fatal("hideConsole of 64 KiB of backslashes did not end in 5 s")
	}
}
