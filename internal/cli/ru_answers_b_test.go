// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// cyrillic finds a Russian letter; toolNamedRU, a tool named as a call or
// a read to make (pg_operation(…), stack_status, pg_backups cluster_id=).
var (
	cyrillic    = regexp.MustCompile(`[А-Яа-яЁё]`)
	toolNamedRU = regexp.MustCompile(`\b(pg|kafka|redis|k8s|stack)_(operation|status|apply|destroy|list|get|backups|kubectl|helm|delete)\b`)
)

// The answers of the creates and the stack of Kubernetes and the databases
// say their own words in the language of the CLI: in Russian, the note,
// next, warning and auth of the answer are Russian, not the English of the
// tool (Астра, #1835; release plan 1.9, #1838). The world of
// TestParity_JSONEqualsTool answers each create with its operation.
func TestRussian_AnswersOfTheDatabasesKubernetesAndStacks(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	want := map[string]bool{"pg_create": true, "pg_backup_create": true, "kafka_create": true, "redis_create": true,
		"k8s_add_addon": true, "k8s_kubeconfig": true, "stack_plan": true}
	seen := map[string]bool{}
	for _, c := range cases {
		if !want[c.tool.Name] || seen[c.tool.Name] {
			continue
		}
		r := runJSONCase(t, c, w, fake)
		var answer map[string]any
		if r.isErr || json.Unmarshal([]byte(r.text), &answer) != nil {
			continue
		}
		seen[c.tool.Name] = true
		for _, k := range []string{"note", "next", "warning", "auth"} {
			if v, ok := answer[k].(string); ok && v != "" && !cyrillic.MatchString(v) {
				t.Errorf("%s: %s is English in Russian: %s", c.tool.Name, k, v)
			}
			// The CLI names its command, not the tool (the boundary of D6,
			// #1834: «poll <tool>» in the CLI is the command evo …).
			if v, _ := answer[k].(string); toolNamedRU.MatchString(v) {
				t.Errorf("%s: %s names a tool, not the command of the CLI: %s", c.tool.Name, k, v)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s: the world gave no answer to read", name)
		}
	}
}
