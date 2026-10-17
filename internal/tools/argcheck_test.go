package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// Релизный QA (D-mcp 4, 7): неизвестный аргумент — USAGE до обработчика
// с перечнем аргументов инструмента; confirmed принимает любой; enum —
// без учёта регистра, в написании схемы; чужое значение — USAGE.
func TestCheckedArgs(t *testing.T) {
	var got map[string]any
	h := checkedArgs("dns_zone_create", objSchema(map[string]any{
		"domain": strProp("d"),
		"type":   enumProp("t", "public", "private"),
	}, "domain"), func(_ context.Context, args map[string]any) (string, bool) {
		got = args
		return "ok", false
	})
	for _, tc := range []struct {
		args map[string]any
		err  string
	}{
		{map[string]any{"domain": "x.ru.", "vm": "orphan-1"}, "unknown argument(s) vm"},
		{map[string]any{"domain": "x.ru.", "type": "internal"}, "is not one of public, private"},
	} {
		got = nil
		text, isErr := h(context.Background(), tc.args)
		if !isErr || got != nil || !strings.Contains(text, tc.err) || !strings.Contains(text, `"reason": "USAGE"`) {
			t.Errorf("%v: %s", tc.args, text)
		}
	}
	if _, isErr := h(context.Background(), map[string]any{"domain": "x.ru.", "type": "PRIVATE", "confirmed": true}); isErr || got["type"] != "private" {
		t.Errorf("PRIVATE → %v", got)
	}
}

// Каждый инструмент реестра проверяет аргументы по своей схеме: destroy с
// именем параметра из disk_attach не уходит в «удалить текущее развёртывание».
func TestRegistry_DestroyUnknownArgRefused(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) { t.Fatal("engine built"); return nil, nil })
	tl, _ := r.Lookup("destroy")
	text, isErr := tl.Handler(context.Background(), map[string]any{"vm": "orphan-1"})
	if !isErr || !strings.Contains(text, "unknown argument(s) vm") || !strings.Contains(text, "vm_id") {
		t.Errorf("destroy {vm}: %s", text)
	}
}

// checkedArgs проверяет enum только у строк (ревью Qwen по #87): схема с
// нестроковым enum прошла бы без проверки — такой быть не должно.
func TestSchemas_EnumsAreStrings(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k, p := range props {
			pm, _ := p.(map[string]any)
			enum, _ := pm["enum"].([]any)
			for _, e := range enum {
				if _, ok := e.(string); !ok {
					t.Errorf("%s.%s: enum value %v is not a string — checkedArgs checks only string enums", tl.Name, k, e)
				}
			}
		}
	}
}

// Релизный аудит, R-09/A-013: скаляр не того типа, что в схеме, — USAGE с
// именем поля, а не молча «аргумента нет»: confirmed:"true" давал превью,
// size_gb:"20" — «size_gb is required». Массивы и объекты не проверяются:
// обработчики принимают их в нескольких видах.
func TestCheckedArgs_ScalarTypes(t *testing.T) {
	var got map[string]any
	h := checkedArgs("disk_create", objSchema(map[string]any{
		"name":      strProp("n"),
		"size_gb":   intProp("s"),
		"ratio":     map[string]any{"type": "number"},
		"ports":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		"config":    map[string]any{"type": "object"},
		"confirmed": confirmedProp(),
	}), func(_ context.Context, args map[string]any) (string, bool) {
		got = args
		return "ok", false
	})
	for _, tc := range []struct {
		args map[string]any
		err  string
	}{
		{map[string]any{"confirmed": "true"}, "confirmed must be a boolean (true or false), got a string"},
		{map[string]any{"size_gb": "20"}, "size_gb must be an integer, got a string"},
		{map[string]any{"size_gb": 2.5}, "size_gb must be an integer, got a number 2.5"},
		{map[string]any{"name": float64(7)}, "name must be a string, got a number 7"},
		{map[string]any{"ratio": true}, "ratio must be a number, got a boolean true"},
	} {
		got = nil
		text, isErr := h(context.Background(), tc.args)
		if !isErr || got != nil || !strings.Contains(text, tc.err) || !strings.Contains(text, `"reason": "USAGE"`) {
			t.Errorf("%v: %s", tc.args, text)
		}
	}
	// Верные типы, null, целые Go (оркестратор стеков), json.Number,
	// массив строкой и объект строкой — проходят как раньше.
	for _, args := range []map[string]any{
		{"name": "d1", "size_gb": float64(20), "ratio": 0.5, "confirmed": true},
		{"size_gb": 20, "name": nil},
		{"size_gb": json.Number("20")},
		{"ports": "80,443", "config": `{"a":1}`},
	} {
		got = nil
		if text, isErr := h(context.Background(), args); isErr || got == nil {
			t.Errorf("%v: %s", args, text)
		}
	}
}

// Живой пример аудита: destroy {"name":"x","confirmed":"true"} молча давал
// превью; теперь — USAGE, до обработчика и без движка.
func TestRegistry_ConfirmedAsStringRefused(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) { t.Fatal("engine built"); return nil, nil })
	tl, _ := r.Lookup("destroy")
	text, isErr := tl.Handler(context.Background(), map[string]any{"name": "x", "confirmed": "true"})
	if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, "confirmed must be a boolean") {
		t.Errorf("destroy confirmed=\"true\": %s", text)
	}
}
