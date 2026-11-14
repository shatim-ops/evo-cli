// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Completion of argument values by Tab (release plan 1.5, dogfood 28.10):
// an argument that names a resource of the project or an entry of a
// catalogue is completed from the read tool that lists them. One place
// decides which list completes which argument (completionOf): Decl.Completes
// of the tool, valueRefs by the argument's name (a name or an id of the
// list), then the kinds of Decl.IDArgs by their listTool, with a tool's
// exceptions in Decl.NoCompletion. The CLI asks Complete for every
// command alike. Nothing here writes, asks for consent or goes to the
// journal; a secret's value is never listed.

// completion is the list an argument's values come from.
type completion struct {
	// list is a read tool that takes no required argument.
	list string
	// field is the item's field that is the value ("id", "name", "path");
	// the item's name, when the value is another field, describes it.
	field string
	// byName: the argument takes a name as well as an id (vm_get, D:
	// ownerKind.byName), so names are offered too.
	byName bool
}

// valueRefs complete an argument by its name, in every tool that has it,
// unless Decl.NoCompletion makes an exception; the rest of the ids come from
// the kinds of Decl.IDArgs.
var valueRefs = map[string]completion{
	"region":               {list: "list_zones", field: "name"},
	"zone":                 {list: "list_zones", field: "name"},
	"availability_zone_id": {list: "list_zones", field: "id"},
	"flavor":               {list: "list_flavors", field: "name"},
	"image":                {list: "image_list", field: "name"},
	"image_id":             {list: "image_list", field: "id"},
	"bucket":               {list: "s3_buckets", field: "name"},
	"path":                 {list: "secret_list", field: "path"},
	"project_id":           {list: "org_projects", field: "id"},
	"vm_id":                {list: "vm_list", field: "id", byName: true},
	"subnet_id":            {list: "list_subnets", field: "id"},
	"vpc_id":               {list: "vpc_list", field: "id"},
	"disk_id":              {list: "disk_list", field: "id"},
	"security_group_id":    {list: "list_security_groups", field: "id"},
}

// completionOf is how (tool, arg) is completed, if it is; an argument of
// Decl.RefArgs takes a name (D57), so names are offered too.
func (r *Registry) completionOf(tool, arg string) (completion, bool) {
	c, ok := r.completionDeclared(tool, arg)
	if ok && takesName(tool, arg) {
		c.byName = true
	}
	return c, ok
}

func (r *Registry) completionDeclared(tool, arg string) (completion, bool) {
	d := decls[tool]
	if _, no := d.NoCompletion[arg]; no {
		return completion{}, false
	}
	// The tool's own declaration decides: its list or none, never the
	// list valueRefs has for the same name in another product.
	if c, ok := d.Completes[arg]; ok {
		return c, r.listable(c.list)
	}
	if c, ok := valueRefs[arg]; ok && r.listable(c.list) {
		return c, true
	}
	for _, a := range d.IDArgs {
		if a.name == arg && a.kind != nil && a.kind.listTool != "" && r.listable(a.kind.listTool) {
			return completion{list: a.kind.listTool, field: "id", byName: a.kind.byName}, true
		}
	}
	return completion{}, false
}

// listable: the tool is a read the registry has, with no required argument.
func (r *Registry) listable(name string) bool {
	t, ok := r.Lookup(name)
	if !ok || t.Mutating || IsSensitive(t) {
		return false
	}
	req, _ := t.InputSchema["required"].([]string)
	return len(req) == 0
}

// Completes reports whether Complete has values for (tool, arg).
func (r *Registry) Completes(tool, arg string) bool {
	_, ok := r.completionOf(tool, arg)
	return ok
}

// CompletionTimeout bounds a Tab: an empty answer beats a shell that hangs.
var CompletionTimeout = 1500 * time.Millisecond

// completionTTL is how long the values of a list are kept: Tab after Tab
// asks the cloud once.
const completionTTL = 3 * time.Minute

// Candidate is a value Tab offers and what it is (a name beside an id).
type Candidate struct {
	Value, Note string
}

// Complete is the values of (tool, arg) that start with prefix (case
// insensitive), from its list (completionOf): the cache of the last
// minutes, else one read bounded by CompletionTimeout. Without a key or a
// project, or on any error, there are none — Tab stays silent.
func (r *Registry) Complete(ctx context.Context, tool, arg, prefix string) []Candidate {
	c, ok := r.completionOf(tool, arg)
	if !ok {
		return nil
	}
	eng, err := r.newEngine(nil)
	if err != nil || eng.Config.KeyID == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(c.list + "\x00" + eng.Config.ProjectID + "\x00" + eng.Config.KeyID))
	path := ""
	if dir := config.CacheDir(); dir != "" {
		path = filepath.Join(dir, "completion", hex.EncodeToString(sum[:12])+".json")
	}
	items, fresh := readCompletionCache(path)
	if !fresh {
		ctx, cancel := context.WithTimeout(stepOf(ctx), CompletionTimeout)
		defer cancel()
		text, isErr := r.unchecked[c.list](ctx, map[string]any{})
		if isErr {
			return nil
		}
		items = completionItems(text, c.field)
		writeCompletionCache(path, items)
	}
	var out []Candidate
	low := strings.ToLower(prefix)
	add := func(v, note string) {
		if v != "" && strings.HasPrefix(strings.ToLower(v), low) {
			out = append(out, Candidate{Value: v, Note: note})
		}
	}
	for _, it := range items {
		v := it[c.field]
		note := it["name"]
		if c.field == "name" {
			note = ""
		}
		add(v, note)
		if c.byName && c.field != "name" {
			add(it["name"], v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

// completionItems are the id, name and path of each item of the list in a
// list tool's answer whose items carry field (the first such, by key): only
// those, never another field (a secret's value is not in secret_list, and
// would not be kept either). Another list beside it (notes, warnings) is
// not taken for it.
func completionItems(text, field string) []map[string]string {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rows, ok := m[k].([]any)
		if !ok {
			continue
		}
		var items []map[string]string
		for _, row := range rows {
			o, ok := row.(map[string]any)
			if !ok {
				continue
			}
			it := map[string]string{}
			for _, f := range []string{"id", "name", "path"} {
				if s, ok := o[f].(string); ok {
					it[f] = s
				}
			}
			items = append(items, it)
		}
		for _, it := range items {
			if it[field] != "" {
				return items
			}
		}
	}
	return nil
}

type completionCache struct {
	At    time.Time           `json:"at"`
	Items []map[string]string `json:"items"`
}

// readCompletionCache is the items kept at path and whether they are
// fresh (younger than completionTTL).
func readCompletionCache(path string) ([]map[string]string, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c completionCache
	// The age of the cache is a decision on the wall clock (a TTL).
	if json.Unmarshal(data, &c) != nil || time.Since(c.At) > completionTTL {
		return nil, false
	}
	return c.Items, true
}

// writeCompletionCache keeps items at path, readable by the user alone.
func writeCompletionCache(path string, items []map[string]string) {
	if path == "" {
		return
	}
	data, err := json.Marshal(completionCache{At: time.Now(), Items: items})
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = config.WriteFileAtomic(path, data, 0o600) // a link at path is replaced, not written through (R-67)
}
