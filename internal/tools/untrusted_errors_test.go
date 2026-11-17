// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// brokenAfterAPI is hostileBody's world in which the connection breaks
// from the request number from on: what a tool read before has the
// hostile name; an answer after it breaks in its body, an error with no
// answer of the API to mark (the class of a network failure,
// API_UNREACHABLE, that a client does not repeat with its waits).
type brokenAfterAPI struct {
	body []byte
	from int
	mu   sync.Mutex
	n    int
}

// errReader reads as its error.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func (a *brokenAfterAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	a.mu.Lock()
	a.n++
	n := a.n
	a.mu.Unlock()
	if n >= a.from {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(io.MultiReader(strings.NewReader(`{"items":[`), errReader{errors.New("synthetic connection reset")})), Request: r}, nil
	}
	return (&oddAPI{status: http.StatusOK, body: a.body}).RoundTrip(r)
}

// withFlags is args with every optional boolean argument of the tool set:
// a flag reaches a read of its own (list_security_groups rules=true reads
// the rules of each group it listed, Астра #2177).
func withFlags(tl Tool, args map[string]any) map[string]any {
	out := maps.Clone(args)
	props, _ := tl.InputSchema["properties"].(map[string]any)
	for name, p := range props {
		if m, _ := p.(map[string]any); m["type"] == "boolean" && name != "confirmed" {
			if _, set := out[name]; !set {
				out[name] = true
			}
		}
	}
	return out
}

// A read of the cloud that breaks after the reads before it named a
// resource answers an error whose words are evo's: a name the cloud gave
// is in quotes, on one line, never at the start of a line of the message
// (the class of Астра #2177: the name of a security group listed, then
// the read of its rules broken, came into "error" as it was). An error of
// the network has no status of the API, so it is not marked _untrusted
// as an answer of the API is: the quotes are the one defence of its text.
//
// Every tool, with its required arguments and then with its flags too,
// meets a connection that breaks at its second, third, ... sixth request.
func TestUntrustedNames_OneLineInErrorsOfABrokenRead(t *testing.T) {
	faultToolsIsolate(t)
	body := hostileBody()
	reached := 0
	for from := 2; from <= 6; from++ {
		for _, tl := range New(nil).Tools() {
			for _, args := range []map[string]any{requiredArgs(tl), withFlags(tl, requiredArgs(tl))} {
				api := &brokenAfterAPI{body: body, from: from}
				cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
				cloud.SetToken("t")
				cloud.Backoff = 1
				r := raRegistry(t, api, cloud, nil)
				m, text, _ := callNamed(t, r, tl.Name, args)
				if api.n >= from {
					reached++
				}
				var texts []string
				textOf(m, "", &texts)
				for _, s := range texts {
					for _, line := range strings.Split(s, "\n") {
						if strings.HasPrefix(strings.TrimSpace(line), hostileMarker) {
							t.Errorf("%s %v, broken from request %d: a cloud name starts a line of evo's text:\n%s", tl.Name, args, from, text)
						}
					}
				}
			}
		}
	}
	// The world must break reads that follow a read: a tool that reads
	// once never meets it.
	if reached < 100 {
		t.Errorf("the connection broke in %d calls: the world reaches no read after a read", reached)
	}
	t.Logf("calls that met the broken connection: %d", reached)
}
