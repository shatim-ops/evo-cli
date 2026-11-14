// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"slices"
	"testing"
)

// The reads of the databases, Kubernetes and the secrets hand over text
// the cloud wrote — names of instances, specifications, addons, flavors,
// versions, databases and users, the state of a secret's version: the
// answer names those fields in "_untrusted" (Астра, the full audit of
// 14.11, finding 05). The world of TestParity_JSONEqualsTool answers each.
func TestUntrusted_ReadsOfTheDatabasesKubernetesAndSecretsMarkTheCloudsText(t *testing.T) {
	want := map[string]string{
		"metrics_history": "series", "pg_specs": "specifications", "k8s_addons": "addons",
		"k8s_addon_versions": "display_name", "secret_add_version": "version", "k8s_plan": "flavor",
		"k8s_nodepool_get": "flavor", "pg_versions": "versions", "kafka_versions": "versions",
		"redis_versions": "versions", "pg_databases": "databases", "pg_users": "users",
	}
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	seen := map[string]bool{}
	for _, c := range cases {
		field, ok := want[c.tool.Name]
		if !ok || seen[c.tool.Name] {
			continue
		}
		r := runJSONCase(t, c, w, fake)
		var m map[string]any
		if r.isErr || json.Unmarshal([]byte(r.text), &m) != nil {
			continue
		}
		seen[c.tool.Name] = true
		if _, has := m[field]; !has {
			t.Errorf("%s: the world's answer has no %s: %s", c.tool.Name, field, r.text)
			continue
		}
		if u, _ := m["_untrusted"].([]any); !slices.Contains(u, any(field)) {
			t.Errorf("%s: %s is the cloud's text, and _untrusted %v does not name it", c.tool.Name, field, m["_untrusted"])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s: the world gave no answer to read", name)
		}
	}
}
