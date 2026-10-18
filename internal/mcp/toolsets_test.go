// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// --toolsets (ревью r5 M3): tools/list показывает только выбранные группы,
// tools/call инструмента вне них — ошибка инструмента, а не его вызов;
// «all» и пустой список — все инструменты; неизвестная группа — ошибка.
func TestSetToolsets(t *testing.T) {
	s := newTestServer()
	all := len(s.ListTools())

	if err := s.SetToolsets([]string{"deploy", " VM ", "vpc"}); err != nil {
		t.Fatal(err)
	}
	got := s.ListTools()
	if len(got) == 0 || len(got) >= all {
		t.Fatalf("deploy,vm,vpc: %d tools of %d", len(got), all)
	}
	listed := map[string]bool{}
	for _, tool := range got {
		listed[tool.Name] = true
		if g := s.groupOf(tool.Name); g != "deploy" && g != "vm" && g != "vpc" {
			t.Errorf("%s (group %s) is listed", tool.Name, g)
		}
	}
	for _, name := range []string{"deploy", "vm_list", "list_subnets"} {
		if !listed[name] {
			t.Errorf("%s is not listed", name)
		}
	}

	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), `"pg_list"`) {
		t.Errorf("tools/list advertises pg_list outside the toolsets")
	}
	resp = roundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pg_list","arguments":{}}}`)
	// Вне --toolsets инструмента для клиента нет: -32602, как у
	// неизвестного (релизный аудит, R-07).
	rpcErr, _ := resp["error"].(map[string]any)
	b, _ = json.Marshal(resp)
	if rpcErr == nil || rpcErr["code"].(float64) != codeInvalidParams || !strings.Contains(string(b), "not in this server's --toolsets") {
		t.Errorf("pg_list outside the toolsets: %s", b)
	}

	for _, reset := range [][]string{{"all"}, nil, {""}} {
		if err := s.SetToolsets(reset); err != nil || len(s.ListTools()) != all {
			t.Errorf("SetToolsets(%q): %d tools, %v; want all %d", reset, len(s.ListTools()), err, all)
		}
	}
	if err := s.SetToolsets([]string{"vm", "nosuch"}); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("unknown toolset: %v", err)
	}
}

func (s *Server) groupOf(name string) string {
	t, _ := s.reg.Lookup(name)
	return t.Group
}
