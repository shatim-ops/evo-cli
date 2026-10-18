// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"net/http"
	"testing"
)

// A POST body larger than maxMessageSize is refused with 413 before it is
// parsed: without MaxBytesReader a client could make the server buffer any
// amount of memory (the batch limit only counts messages that were decoded).
func TestAudit_HTTPBodySizeLimited(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})

	// A valid single message padded with spaces past the limit: still one
	// message for the batch counter, too big for the body reader.
	body := make([]byte, 0, httpMaxBody+64)
	body = append(body, `{"jsonrpc":"2.0","id":1,"method":"ping"}`...)
	body = append(body, bytes.Repeat([]byte(" "), httpMaxBody+1-len(body))...)

	resp := post(t, ts, string(body), nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("body of %d bytes: status %d, want 413", len(body), resp.StatusCode)
	}

	// Just under the limit is served normally.
	small := append([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), bytes.Repeat([]byte(" "), 1024)...)
	resp = post(t, ts, string(small), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("small body: status %d, want 200", resp.StatusCode)
	}
}
