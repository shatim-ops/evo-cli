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
	// A service that echoes the template in the image it answers: the
	// success goes out without it (image_create drops it).
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/images" {
			echoes++
			body, _ := io.ReadAll(r.Body)
			var sent map[string]any
			_ = json.Unmarshal(body, &sent)
			sent["id"] = parityVM
			reply, _ := json.Marshal(sent)
			return httpFault(200, string(reply))(r)
		}
		return original(r)
	}
	forgetRun(w)
	args["confirmed"] = true
	check("mcp stdio, a success", string(stdioRaw(t, srv.stdio, "image_create", args)))
	if echoes < 4 {
		t.Fatalf("the service echoed %d requests, want one per surface (4): the world did not reach the write", echoes)
	}
}

// platformWords are the words of a refusal of the platform that a call
// with no secret in it shows as they are.
const platformWords = "PLATFORM-WORDS-2419"

// A call that sends the cloud a value that may be a secret — the env of an
// app, a password or any value the caller gave in the metadata of a VM's
// image — does not carry it into evo's answer when the service echoes the
// body of the request in its refusal: over MCP (stdio, HTTP) and in the
// CLI, human and --json (the cause evo names by the code of a refusal stays:
// TestSecretWriteAnswer_ACallThatSentASecret). A VM whose metadata holds only evo's public key, login
// and hostname keeps the platform's words (аудит 1.9 rc.1, A19-2, #2419;
// the class of #2257; «Риск»: a refusal of the most frequent paid tool
// keeps what tells the agent what to do next). Not a list of arguments:
// the client that sends such a value marks the call.
func TestSecretValues_NotEchoedInTheCloudsError(t *testing.T) {
	for _, c := range []struct {
		name, tool, path string
		args             map[string]any
		secret           bool
	}{
		{"the env of an app", "ca_deploy", "/v2/containers", map[string]any{"name": "app", "image": "nginx:1.27", "env": map[string]any{"DB_PASSWORD": secretMarker}}, true},
		{"a value the caller gave", "provision", "/api/v1.1/vms", map[string]any{"name": "app", "image_metadata": map[string]any{"public_key": secretMarker}}, true},
		{"evo's key and login only", "provision", "/api/v1.1/vms", map[string]any{"name": "app"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			srv := parityServers(t)
			original := w.rt.extra
			echoes := 0
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && r.URL.Path == c.path {
					echoes++
					body, _ := io.ReadAll(r.Body)
					reply, _ := json.Marshal(map[string]string{"code": "vm_quota_exceeded", "message": platformWords + ": no room for: " + string(body)})
					return httpFault(400, string(reply))(r)
				}
				return original(r)
			}
			check := func(where, text string) {
				t.Helper()
				if c.secret && strings.Contains(text, secretMarker) {
					t.Errorf("%s: the value sent is in the answer: %s", where, text)
				}
				if !c.secret && !strings.Contains(text, platformWords) {
					t.Errorf("%s: the platform's words are lost with no secret sent: %s", where, text)
				}
			}
			args := map[string]any{"confirmed": true}
			for k, v := range c.args {
				args[k] = v
			}
			check("mcp stdio", string(stdioRaw(t, srv.stdio, c.tool, args)))
			forgetRun(w)
			h, _ := json.Marshal(httpDetails(t, srv.http, c.tool, args))
			check("mcp http", string(h))
			delete(args, "confirmed")
			raw, _ := json.Marshal(args)
			for _, extra := range [][]string{nil, {"--json"}} {
				forgetRun(w)
				_, out, errOut := runCLI(t, "", append([]string{"tools", "call", c.tool, "--args-json", string(raw), "--yes"}, extra...)...)
				check("cli "+strings.Join(extra, " "), out+errOut)
			}
			if echoes < 4 {
				t.Fatalf("the service echoed %d requests, want one per surface (4): the world did not reach the write", echoes)
			}
		})
	}
}
