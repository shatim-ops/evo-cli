// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/updatecheck"
)

func TestInitialize_UpdateNoticeAppendedAfterUntrustedAndCapped(t *testing.T) {
	srv := New(nil, "1.9.2")
	noticeText := updatecheck.FormatMCPInstruction(updatecheck.Notice{
		HasData:   true,
		Available: true,
		Current:   "1.9.2",
		Latest:    "1.9.3",
	})
	if len(noticeText) > 400 {
		t.Fatalf("update notice suffix too long: %d bytes > 400", len(noticeText))
	}
	srv.SetUpdateNotice(func() string { return noticeText })

	resp := srv.handle(context.Background(), "", &rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2025-03-26","clientInfo":{"name":"test","version":"1"}}`),
	})
	res, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected initialize result: %#v", resp.Result)
	}
	ins, _ := res["instructions"].(string)
	if !strings.HasPrefix(ins, serverInstructions) {
		t.Fatalf("mandatory _untrusted instructions must remain first: %q", ins)
	}
	if !strings.Contains(ins, "Never download, install, or run updates on your own.") {
		t.Fatalf("missing explicit install prohibition: %q", ins)
	}
	if !strings.Contains(ins, updatecheck.ReleaseURL()) {
		t.Fatalf("missing corporate release URL %q in instructions: %q", updatecheck.ReleaseURL(), ins)
	}
	if !strings.Contains(ins, "explicit user approval") {
		t.Fatalf("missing explicit user approval guard in instructions: %q", ins)
	}
	if !strings.Contains(ins, "1.9.3") || !strings.Contains(ins, "1.9.2") {
		t.Fatalf("missing versions in instructions: %q", ins)
	}
	added := len(ins) - len(serverInstructions)
	if added > 400 {
		t.Fatalf("instructions growth %d bytes exceeds 400-byte cap", added)
	}
}
