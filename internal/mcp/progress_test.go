// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A long call shows its stages to an MCP client that asked for them with a
// progressToken: notifications/progress, the lines the CLI prints on
// stderr, before the answer (owner's decision 23.10: provision and
// deploy_apply were silent for up to 25 minutes over MCP). A client that
// did not ask gets none. All IDs and addresses are made up.

// progressCall is a provision of web, with a progressToken when token is
// set.
func progressCall(token string) string {
	meta := ""
	if token != "" {
		meta = `,"_meta":{"progressToken":` + token + `}`
	}
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"provision","arguments":{"name":"web","confirmed":true}` + meta + `}}`
}

// rpcMessage is a message the server sent: a notification or the answer.
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		ProgressToken json.RawMessage `json:"progressToken"`
		Progress      float64         `json:"progress"`
		Message       string          `json:"message"`
	} `json:"params"`
	Result json.RawMessage `json:"result"`
}

// checkProgress checks the messages of one call: notifications/progress
// with the token, rising progress and evo's stages, then the answer, last;
// no message carries the generated password.
func checkProgress(t *testing.T, msgs []rpcMessage, token, password string) {
	t.Helper()
	if len(msgs) < 3 {
		t.Fatalf("want progress and the answer, got %d messages: %+v", len(msgs), msgs)
	}
	last := msgs[len(msgs)-1]
	if string(last.ID) != "1" || len(last.Result) == 0 {
		t.Fatalf("the answer is not last: %+v", last)
	}
	var stages []string
	prev := 0.0
	for _, m := range msgs[:len(msgs)-1] {
		if m.Method != "notifications/progress" || string(m.Params.ProgressToken) != token || m.Params.Progress <= prev || m.Params.Message == "" {
			t.Errorf("not a progress notification of %s: %+v", token, m)
		}
		prev = m.Params.Progress
		stages = append(stages, m.Params.Message)
		if password != "" && strings.Contains(m.Params.Message, password) {
			t.Errorf("progress carries the password: %q", m.Params.Message)
		}
	}
	if !strings.Contains(strings.Join(stages, "\n"), "Creating VM web") {
		t.Errorf("no stage of provision in %q", stages)
	}
}

func decodeLines(t *testing.T, out string) []rpcMessage {
	t.Helper()
	var msgs []rpcMessage
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m rpcMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestProgress_Stdio(t *testing.T) {
	for _, token := range []string{`"p-1"`, `7`} {
		cloud := &provisionCloud{}
		var out bytes.Buffer
		if err := provisionServer(t, cloud).Run(strings.NewReader(progressCall(token)+"\n"), &out); err != nil {
			t.Fatal(err)
		}
		checkProgress(t, decodeLines(t, out.String()), token, cloud.password())
	}

	var out bytes.Buffer
	if err := provisionServer(t, &provisionCloud{}).Run(strings.NewReader(progressCall("")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if msgs := decodeLines(t, out.String()); len(msgs) != 1 || string(msgs[0].ID) != "1" {
		t.Errorf("no progressToken: want the answer only, got %+v", msgs)
	}
}

// Over HTTP the stages go on the SSE stream of the call; a client that
// takes plain JSON gets the answer only, as there is nowhere to send them.
// The password H7 masks in the answer is in no stage either.
func TestProgress_HTTP(t *testing.T) {
	cloud := &provisionCloud{}
	s := provisionServer(t, cloud)
	s.restrictForHTTP(false)
	h := NewHTTPHandler(s, HTTPOptions{BearerToken: testToken, KeepAlive: time.Hour})
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})

	resp := post(t, ts, progressCall(`"p-http"`), map[string]string{"Accept": "application/json, text/event-stream"})
	var msgs []rpcMessage
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var m rpcMessage
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatalf("event %q: %v", data, err)
			}
			msgs = append(msgs, m)
		}
	}
	checkProgress(t, msgs, `"p-http"`, cloud.password())

	resp = post(t, ts, progressCall(`"p-json"`), map[string]string{"Accept": "application/json"})
	var m rpcMessage
	decodeBody(t, resp, &m)
	if ct := resp.Header.Get("Content-Type"); string(m.ID) != "1" || len(m.Result) == 0 || !strings.HasPrefix(ct, "application/json") {
		t.Errorf("plain JSON (%s): %+v", ct, m)
	}
}
