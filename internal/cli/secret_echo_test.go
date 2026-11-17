// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// secretMarker is a password inside a cloud-init template: the value of a
// secret argument (CLISpec.SecretFiles) the call carries.
const secretMarker = "ASTRA2251SECRET42"

// A service that echoes the body of the request in its refusal does not
// carry the value of a secret argument into evo's answer: the template of
// image_create, with a password in it, is masked in the error over MCP
// (stdio, HTTP) and in the CLI, human and --json (Астра #2257).
func TestSecretArgs_NotEchoedInTheCloudsError(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	original := w.rt.extra
	echoes := 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/images" {
			echoes++
			body, _ := io.ReadAll(r.Body)
			reply, _ := json.Marshal(map[string]string{"message": "invalid request body: " + string(body)})
			return httpFault(400, string(reply))(r)
		}
		return original(r)
	}
	args := map[string]any{"name": "img-web", "disk_id": parityVM,
		"user_data_template": "#cloud-config\npassword: " + secretMarker, "confirmed": true}
	check := func(where, text string) {
		t.Helper()
		if strings.Contains(text, secretMarker) {
			t.Errorf("%s: the secret of the template is in the answer: %s", where, text)
		}
	}
	check("mcp stdio", string(stdioRaw(t, srv.stdio, "image_create", args)))
	forgetRun(w)
	h, _ := json.Marshal(httpDetails(t, srv.http, "image_create", args))
	check("mcp http", string(h))
	forgetRun(w)
	delete(args, "confirmed")
	raw, _ := json.Marshal(args)
	for _, extra := range [][]string{nil, {"--json"}} {
		forgetRun(w)
		_, out, errOut := runCLI(t, "", append([]string{"tools", "call", "image_create", "--args-json", string(raw), "--yes"}, extra...)...)
		check("cli "+strings.Join(extra, " "), out+errOut)
	}
	if echoes < 4 {
		t.Fatalf("the service echoed %d requests, want one per surface (4): the world did not reach the write", echoes)
	}
}
