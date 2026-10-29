// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// accessCredential names, by group of tools, the credential a refusal of
// rights (403) is about, as words one of which the hint of the refusal
// says. A group not here is reached with the key of the service account,
// and its hint names the account's role. A group with a key of its own
// (Object Storage: the S3 keys of the profile, tenant_id) is declared
// here, or its 403 sends the agent to grant a role that changes nothing.
var accessCredential = map[string][]string{
	"s3": {"tenant_id", "CLOUDRU_S3"},
}

// accessRoleWords are what the hint of a 403 says for the key of the
// service account.
var accessRoleWords = []string{"role", "роль", "роли"}

// accessHintMisses are the groups known to give a 403 another hint: group →
// the work item of the release plan that fixes it. The list only shrinks: a
// group not here fails the test, and so does a line whose group's 403
// names its credential.
var accessHintMisses = map[string]string{
	"s3": "release plan 1.6, «Три формы одним PR» (Ц-3 агентного аудита rc.1): «разработка»",
}

// Ц-3 of the agent's audit of rc.1: `evo s3 buckets` refused with Access
// Denied answered «the service account lacks a role … or project_id is
// another project», though S3 is reached with keys of its own and no
// project: the cause was the tenant_id. Every tool's 403 (access_denied,
// A10), in both languages, has a hint that names the credential of its
// group: accessCredential, else the role of the service account.
func TestAccessDenied_HintNamesTheCredentialOfTheService(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	group := map[string]string{}
	for _, tl := range contractReg(t, &contractRT{kind: "403"}).Tools() {
		group[tl.Name] = tl.Group
	}
	var mu sync.Mutex
	found := map[string]string{}
	seen := map[string]bool{}
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		walkAnswers(t, "403", nil, func(c langCall, m map[string]any) {
			if m["access_denied"] != true {
				return
			}
			g := group[c.tool]
			words, own := accessCredential[g]
			if !own {
				words = accessRoleWords
			}
			hint, _ := m["hint"].(string)
			mu.Lock()
			defer mu.Unlock()
			seen[g] = true
			if !containsAny(hint, words) {
				if _, ok := found[g]; !ok {
					found[g] = c.tool + " (" + lang + "): " + clip(hint) + " — names none of " + strings.Join(words, ", ")
				}
			}
		})
	}
	for g := range accessCredential {
		if !seen[g] {
			t.Errorf("accessCredential names group %s, whose tools gave no 403 in the world: the check sees nothing", g)
		}
	}
	groups := make([]string, 0, len(found))
	for g := range found {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		if accessHintMisses[g] == "" {
			t.Errorf("group %s: %s — name the credential of the service, or, while the plan moves it, add the group to accessHintMisses with the work item", g, found[g])
		}
	}
	for g := range accessHintMisses {
		if found[g] == "" {
			t.Errorf("accessHintMisses names group %s, whose 403 names its credential now: remove the line", g)
		}
	}
}

func containsAny(s string, words []string) bool {
	low := strings.ToLower(s)
	for _, w := range words {
		if strings.Contains(low, strings.ToLower(w)) {
			return true
		}
	}
	return false
}
