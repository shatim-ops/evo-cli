// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func lookup(t testing.TB, name string) tools.Tool {
	t.Helper()
	tl, ok := tools.New(nil).Lookup(name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// --args-json и --args-file проверяются по схеме так же, как --arg:
// опечатка в имени и строка вместо числа раньше доходили до обработчика,
// который молча брал значение по умолчанию (disk_create с "size_gb":"50"
// отвечал «size_gb required» с кодом 5, а необязательный аргумент с
// опечаткой просто терялся).
func TestBuildToolArgs_JSONIsCheckedAgainstSchema(t *testing.T) {
	disk := lookup(t, "disk_create")
	ports := lookup(t, "open_ports")
	bad := []struct {
		tool tools.Tool
		json string
		want string
	}{
		{disk, `{"name":"d","size_gb":"50"}`, "size_gb"},
		{disk, `{"name":"d","size_gb":50.5}`, "size_gb"},
		{disk, `{"name":"d","size_gb":50,"atach_to":"web"}`, "atach_to"},
		{disk, `{"name":7,"size_gb":50}`, "name"},
		{ports, `{"security_group":"sg","ports":[80,"x"]}`, "ports"},
	}
	for _, c := range bad {
		_, err := buildToolArgs(c.tool, "", c.json, nil)
		if clerrors.ExitCode(err) != clerrors.ExitGeneral || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: err %v, want a usage error naming %q", c.tool.Name, c.json, err, c.want)
		}
	}
	// An array may come as a string, as MCP takes it ("80,443": the tool's
	// own check, tools.CheckArgs, docs/mcp.md); its items, when it is a
	// JSON array, the CLI checks as it always did (checkItems).
	if args, err := buildToolArgs(ports, "", `{"security_group":"sg","ports":"80"}`, nil); err != nil || args["ports"] != "80" {
		t.Errorf(`ports "80": %v %v`, args, err)
	}

	// Валидное проходит как есть; null — то же, что отсутствие.
	got, err := buildToolArgs(disk, "", `{"name":"d","size_gb":50,"attach_to":null}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"name": "d", "size_gb": float64(50)}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
	if _, err := buildToolArgs(disk, "", `{"name":"d","size_gb":null}`, nil); err == nil ||
		!strings.Contains(err.Error(), "missing required") {
		t.Errorf("null required argument: %v", err)
	}

	// Тот же контроль для --args-file.
	file := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(file, []byte(`{"name":"d","size_gb":"big"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildToolArgs(disk, file, "", nil); err == nil || !strings.Contains(err.Error(), "size_gb") {
		t.Errorf("--args-file: %v", err)
	}
}

// Пустая строка для обязательного аргумента — то же, что его отсутствие:
// иначе обработчик отвечает «required» с кодом 5 (ошибка API).
func TestBuildToolArgs_EmptyRequiredIsMissing(t *testing.T) {
	disk := lookup(t, "disk_create")
	for _, src := range []struct {
		json  string
		pairs []string
	}{
		{"", []string{"name=", "size_gb=5"}},
		{`{"name":"","size_gb":5}`, nil},
	} {
		_, err := buildToolArgs(disk, "", src.json, src.pairs)
		if err == nil || !strings.Contains(err.Error(), "missing required argument(s): name") {
			t.Errorf("%v %s: %v", src.pairs, src.json, err)
		}
	}
	// Необязательный пустой аргумент передаётся как есть.
	if _, err := buildToolArgs(disk, "", "", []string{"name=d", "size_gb=5", "attach_to="}); err != nil {
		t.Errorf("empty optional: %v", err)
	}
}

func TestCoerceArg_Edges(t *testing.T) {
	// Массив объектов: одно значение — один объект, а не обрезки по запятым.
	got, err := coerceArg(param{Type: "array", ItemType: "object"}, `{"ip":"10.0.0.5","port":80}`)
	if want := []any{map[string]any{"ip": "10.0.0.5", "port": float64(80)}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("array of objects: %#v, %v", got, err)
	}
	// JSON-массив проверяется по типу элементов.
	if _, err := coerceArg(param{Type: "array", ItemType: "integer"}, `[80,"x"]`); err == nil {
		t.Error(`[80,"x"] accepted for an array of integers`)
	}
	// NaN и Inf не кодируются в JSON: обработчик получил бы аргумент,
	// который нельзя ни показать в превью, ни отправить в API.
	for _, v := range []string{"NaN", "Inf", "-Inf", "+Inf", "infinity"} {
		if _, err := coerceArg(param{Type: "number"}, v); err == nil {
			t.Errorf("number %q accepted", v)
		}
	}
	// '=' и unicode в значении сохраняются.
	args, err := buildToolArgs(lookup(t, "vm_get"), "", "", []string{"name=a=b=c", "name=веб-сервер ✓"})
	if err != nil || args["name"] != "веб-сервер ✓" {
		t.Errorf("later --arg must win, value kept as is: %v %v", args, err)
	}
	args, _ = buildToolArgs(lookup(t, "vm_get"), "", "", []string{"name=a=b=c"})
	if args["name"] != "a=b=c" {
		t.Errorf("'=' in the value: %v", args["name"])
	}
}

// Пустая строка вместо обязательного позиционного аргумента дерева.
func TestTrees_EmptyPositionalIsUsageError(t *testing.T) {
	_, errOut, code := execCLI(t, "disk", "delete", "")
	if code != clerrors.ExitGeneral || !strings.Contains(errOut, "empty") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

// FuzzCoerceArg: разбор --arg не паникует, а то, что он вернул, кодируется
// в JSON и соответствует типу схемы.
func FuzzCoerceArg(f *testing.F) {
	for _, s := range []string{"", "0", "-1", "1.5", "1e3", "NaN", "Inf", "true", "T", "yes",
		"a,b", ",,", " , ", "[1,2]", `["a","b"]`, "[", `{"k":"v"}`, `{"a":1},{"b":2}`, "é,ж", "\xff\xfe",
		"9223372036854775808", "a=b", "[1,\"x\"]", "[{}]", "null"} {
		for _, typ := range []string{"string", "integer", "number", "boolean", "array", "object", ""} {
			f.Add(typ, "integer", s)
		}
		f.Add("array", "object", s)
		f.Add("array", "string", s)
	}
	f.Fuzz(func(t *testing.T, typ, itemType, value string) {
		p := param{Type: typ, ItemType: itemType}
		v, err := coerceArg(p, value)
		if err != nil {
			return
		}
		if _, merr := json.Marshal(v); merr != nil {
			t.Fatalf("coerceArg(%s/%s, %q) = %#v: not JSON-encodable: %v", typ, itemType, value, v, merr)
		}
		if cerr := matchesSchema(p, v); cerr != nil {
			t.Fatalf("coerceArg(%s/%s, %q) = %#v does not match the schema: %v", typ, itemType, value, v, cerr)
		}
	})
}

// matchesSchema: v passes the tool's check of an argument of p's type
// (tools.CheckArgs), and is the form coerceArg gives: an array is a JSON
// array of items of p's item type, an object a JSON object.
func matchesSchema(p param, v any) error {
	one := tools.Tool{Name: "fuzz", InputSchema: map[string]any{"type": "object",
		"properties": map[string]any{"v": map[string]any{"type": p.Type}}}}
	if err := tools.CheckArgs(one, map[string]any{"v": v}); err != nil {
		return err
	}
	switch p.Type {
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%T is not a JSON array", v)
		}
		return checkItems(p, a)
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("%T is not a JSON object", v)
		}
	}
	return nil
}

// FuzzBuildToolArgs: --arg/--args-json любой формы дают либо аргументы по
// схеме, либо ошибку использования (код 1) — никогда панику или confirmed.
func FuzzBuildToolArgs(f *testing.F) {
	f.Add("disk_create", `{"name":"d","size_gb":5}`, "attach_to=web")
	f.Add("open_ports", `{"security_group":"sg"}`, "ports=80, 443")
	f.Add("tg_create", `{}`, `targets={"ip":"1.2.3.4","port":80}`)
	f.Add("vm_get", `null`, "name==")
	f.Add("disk_delete", `{"disk_id":"x","confirmed":true}`, "")
	f.Add("stack_plan", `{"manifest":{"a":[1,2]}}`, "manifest={}")
	f.Add("deploy_plan", `{"ports":[1e400]}`, "ports=[1,2")
	reg := tools.New(nil)
	f.Fuzz(func(t *testing.T, name, rawJSON, pair string) {
		tl, ok := reg.Lookup(name)
		if !ok {
			return
		}
		var pairs []string
		if pair != "" {
			pairs = strings.Split(pair, "\n")
		}
		args, err := buildToolArgs(tl, "", rawJSON, pairs)
		if err != nil {
			if code := clerrors.ExitCode(err); code != clerrors.ExitGeneral {
				t.Fatalf("exit code %d for %v", code, err)
			}
			return
		}
		if _, ok := args["confirmed"]; ok {
			t.Fatal("confirmed passed through")
		}
		// What passes is what the tool's own check passes (MCP's rule),
		// with the items of each JSON array of the item type (checkItems).
		if err := tools.CheckArgs(tl, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		params := map[string]param{}
		for _, p := range schemaParams(tl) {
			params[p.Name] = p
		}
		for k, v := range args {
			if items, ok := v.([]any); ok {
				if err := checkItems(params[k], items); err != nil {
					t.Fatalf("argument %q: %v", k, err)
				}
			}
		}
		if _, err := json.Marshal(args); err != nil {
			t.Fatalf("not JSON-encodable: %v", err)
		}
	})
}
