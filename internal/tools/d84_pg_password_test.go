// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D84 (agent run of 1.7.0, №11; owner's decision 05.11): pg_user_create
// asked, and after yes Managed PostgreSQL answered 400 «validate password:
// at least one special character … is required» — API_ERROR, code 5. The
// rule evo observed (2 of 2) is checked before the preview and the
// question: USAGE, code 1, the rule and its source named, the password
// itself in no answer («Риск», R-9, У-4). Nothing is sent.
func TestPgUserCreate_PasswordWithoutSpecialCharRefusedBeforeTheQuestion(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	const password = "Qwerty123456"
	for _, confirmed := range []bool{false, true} {
		f := &fakeDbaas{}
		r := dbaasRegistry(t, f, "")
		m, text, isErr := callNamed(t, r, "pg_user_create", map[string]any{"cluster_id": cluster, "name": "app", "password": password, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || m["status"] == "confirmation_required" {
			t.Errorf("confirmed=%v: want USAGE before the question: %s", confirmed, text)
		}
		if strings.Contains(text, password) {
			t.Errorf("confirmed=%v: the refusal holds the password: %s", confirmed, text)
		}
		if !strings.Contains(text, "special character") {
			t.Errorf("confirmed=%v: the refusal does not name the rule: %s", confirmed, text)
		}
		if len(f.reqs) != 0 {
			t.Errorf("confirmed=%v: sent %v", confirmed, f.reqs)
		}
	}
	// A password with one of the characters goes to the preview, as before.
	r := dbaasRegistry(t, &fakeDbaas{}, "")
	if m, text, _ := callNamed(t, r, "pg_user_create", map[string]any{"cluster_id": cluster, "name": "app", "password": "Qwerty123456!"}); m["status"] != "confirmation_required" {
		t.Errorf("a password by the rule: %s", text)
	}
}
