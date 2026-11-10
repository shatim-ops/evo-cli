// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import "testing"

// Grok audit R5-18 (06.11, fixtures; release plan 1.8, #1095): a request
// with the id {"a":1} was served, its id echoed. JSON-RPC 2.0 (§4): an id
// is a String, a Number or Null; any other is an Invalid Request (-32600),
// answered with the id null and nothing executed. (A fractional id is a
// «SHOULD NOT» of the client, not an invalid request; the order of
// initialize is the client's «SHOULD» in MCP — neither is held here.)
func TestQA_IDOfWrongTypeIsInvalidRequest(t *testing.T) {
	for _, id := range []string{`{"a":1}`, `[1]`, `true`} {
		got := runLines(t, newTestServer(), `{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`)
		if len(got) != 1 {
			t.Fatalf("id %s: %d answers", id, len(got))
		}
		m, _ := got[0].(map[string]any)
		if rpcCode(m) != codeInvalidReq || m["id"] != nil {
			t.Errorf("id %s: want an Invalid Request (-32600) with id null, got %v", id, m)
		}
	}
}
