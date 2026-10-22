// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// oddAPI answers every request of every service with the same status and
// body: what the fuzzer made, not what the API documents.
type oddAPI struct {
	status int
	body   []byte
}

func (a *oddAPI) RoundTrip(*http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(a.status)
	_, _ = rec.Write(a.body)
	return rec.Result(), nil
}

// The statuses the fuzzer picks from: 429 and 5xx are left out, they are
// retried with the production backoff and make a run slow, not wrong.
var oddStatuses = []int{http.StatusOK, http.StatusCreated, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict}

// readToolsNoArgs are the tools an agent may call with no arguments and
// without consent: the reads of every service, the compute adapter's
// included.
func readToolsNoArgs(r *Registry) []string {
	var names []string
	for _, tl := range r.Tools() {
		if !tl.Mutating && len(schemaRequired(tl)) == 0 && len(tl.RequiredOneOf) == 0 {
			names = append(names, tl.Name)
		}
	}
	return names
}

// A read tool on an API answer of an odd shape — null for a list, a number
// for a name, an object for an array, a truncated body — answers JSON and,
// when it fails, the error envelope with its reason. It does not panic
// (process audit 1.2.1, second wave: API answers as a fuzz target).
func FuzzReadToolsOnOddAPIAnswers(f *testing.F) {
	home := f.TempDir()
	f.Setenv("HOME", home)
	f.Setenv("USERPROFILE", home)
	api := &oddAPI{}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(f, api, cloud, nil)
	names := readToolsNoArgs(r)
	if len(names) == 0 {
		f.Fatal("no read tool without arguments")
	}
	for i := range names {
		for _, body := range []string{`{}`, `[]`, `null`, `"x"`, `{"items":null}`, `{"items":[{}]}`,
			`{"items":[1,null,"x"]}`, `{"items":[{"id":1,"name":{},"status":[]}],"total":"x"}`, `{"data":`} {
			f.Add(uint8(0), uint8(i), []byte(body))
		}
	}
	f.Fuzz(func(t *testing.T, status, tool uint8, body []byte) {
		api.status = oddStatuses[int(status)%len(oddStatuses)]
		api.body = body
		name := names[int(tool)%len(names)]
		m, text, isErr := callNamed(t, r, name, map[string]any{})
		if reason, _ := m["reason"].(string); isErr && reason == "" {
			t.Errorf("%s on %d %q: error without reason: %s", name, api.status, body, text)
		}
	})
}

// A stack manifest is the agent's text, and stack_plan reads all of it
// before anything exists in the cloud. Whatever the manifest, the answer is
// JSON; a refused plan leaves no job behind, an accepted one names its job.
func FuzzStackPlanManifest(f *testing.F) {
	home := f.TempDir()
	f.Setenv("HOME", home)
	f.Setenv("USERPROFILE", home)
	r := raRegistry(f, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	jobs := filepath.Join(home, ".evo", "stack-jobs")
	for _, m := range []string{
		`{"name":"s","postgres":{"name":"p"}}`,
		`{"name":"s","vpc":{"name":"v"},"kafka":{"name":"k","brokers":3},"redis":{"name":"r"}}`,
		`{"name":"s","k8s":{"name":"k","master_count":3},"registry":{"name":"a"}}`,
		`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1"},"nlb":{"name":"l"},"dns":{"zone_id":"z","name":"w"}}`,
		`{"name":"s","secrets":[{"name":"x","value":"v"}],"s3":[{"bucket":"b"}],"snat":{"name":"n"}}`,
		`{"name":"","redis":{"name":"r","replicas":3}}`, `{"name":"s","kafka":{"name":"k","brokers":-1}}`,
		`{"name":["s"]}`, `{}`,
	} {
		f.Add([]byte(m))
	}
	f.Fuzz(func(t *testing.T, manifest []byte) {
		before, _ := os.ReadDir(jobs)
		m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": jsonValue(manifest)})
		after, _ := os.ReadDir(jobs)
		if isErr && len(after) != len(before) {
			t.Errorf("refused plan left a job: %s", text)
		}
		if !isErr && m["job_id"] == nil {
			t.Errorf("accepted plan names no job: %s", text)
		}
	})
}

// jsonValue is what an MCP client sends for these bytes: the decoded value,
// or the bytes as a string when they are not JSON.
func jsonValue(b []byte) any {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	return v
}
