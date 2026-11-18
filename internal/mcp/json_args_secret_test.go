// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"strings"
	"testing"
)

// A tools/call whose arguments cannot be decoded, or that names arguments
// the tool does not take, is refused without the caller's text: a number
// out of range is quoted by encoding/json (Астра, #2384), a key may be a
// value in the wrong place (Астра, #2386). Both transports share this
// handler.
func TestToolCall_RefusalDoesNotRepeatTheArguments(t *testing.T) {
	const digits = "27183650904512"
	big := "1" + strings.Repeat("0", 323) + digits
	for name, args := range map[string]string{
		"exponent out of range": `{"path":"synthetic","value":` + digits + `e999}`,
		"decimal out of range":  `{"path":"synthetic","value":` + big + `}`,
		"unknown key":           `{"path":"synthetic","s3cr3tKey` + digits + `":"x"}`,
	} {
		s := newTestServer(t)
		out := &bytes.Buffer{}
		req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"secret_create","arguments":` + args + `}}`
		if err := s.Run(strings.NewReader(initLine+"\n"+req+"\n"), out); err != nil {
			t.Fatal(err)
		}
		_, answer, _ := strings.Cut(out.String(), "\n")
		if !strings.Contains(answer, `"id":7`) {
			t.Fatalf("%s: no answer: %s", name, out.String())
		}
		if strings.Contains(answer, digits) || strings.Contains(answer, "s3cr3t") {
			t.Errorf("%s: the refusal repeats the arguments: %s", name, answer)
		}
	}
}
