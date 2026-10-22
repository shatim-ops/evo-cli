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

	// The HTTP limit is 4 MiB, below the 16 MiB stdio line (owner's
	// decision В13): 32 calls at once then hold at most 128 MiB of bodies.
	// A literal, not httpMaxBody, so that raising the constant fails here.
	const limit = 4 << 20
	if httpMaxBody >= maxMessageSize {
		t.Fatalf("httpMaxBody %d is not below the stdio limit %d", httpMaxBody, maxMessageSize)
	}

	// A valid single message padded with spaces: still one message for the
	// batch counter, so only the body reader decides.
	padded := func(n int) string {
		b := make([]byte, 0, n)
		b = append(b, `{"jsonrpc":"2.0","id":1,"method":"ping"}`...)
		return string(append(b, bytes.Repeat([]byte(" "), n-len(b))...))
	}

	if resp := post(t, ts, padded(limit+1), nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("body of %d bytes: status %d, want 413", limit+1, resp.StatusCode)
	}
	if resp := post(t, ts, padded(limit), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("body of %d bytes: status %d, want 200", limit, resp.StatusCode)
	}
}
