// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// callTool вызывает обработчик напрямую и разбирает его JSON-ответ. Копия
// помощника из internal/tools: тесты гейтов через MCP-сервер остались здесь.
func callTool(t *testing.T, fn func(context.Context, map[string]any) (string, bool), args map[string]any) map[string]any {
	t.Helper()
	out, _ := fn(context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("ответ не JSON: %q", out)
	}
	return m
}
