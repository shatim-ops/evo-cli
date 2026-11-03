// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Ц-21, Ц-24 (axis 4): where a PostgreSQL cluster is reachable from bears
// on its security and its use, and the create API takes no parameter of
// it — the cluster lives in its subnets. The preview of pg_create and the
// answer of pg_get say so, rather than leave an agent to guess a public
// address.
func TestPG_AccessIsNamed(t *testing.T) {
	p := pgCreatePreview(config.Config{}, map[string]any{"name": "db", "subnet_id": contractID})
	if !slices.Contains(p.Params, engine.PreviewParam{Key: "access", Value: pgAccess()}) {
		t.Errorf("the preview of pg_create does not say where the cluster is reachable from: %+v", p.Params)
	}
	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	tl, _ := reg.Lookup("pg_get")
	if text, _, _ := contractCall(tl, contractArgs(tl), contractCallLimit); !strings.Contains(text, pgAccess()) {
		t.Errorf("pg_get does not say where the cluster is reachable from: %.400s", text)
	}
}

// FB160-2 (audit of Fable 1.6): the access of PostgreSQL is evo's own text,
// so it follows the language of the CLI (D6) — in the preview of pg_create
// and in pg_get — and its line has a label of its own.
func TestPG_AccessFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	i18n.SetLang(i18n.RU)
	p := pgCreatePreview(config.Config{}, map[string]any{"name": "db", "subnet_id": contractID})
	var got string
	for _, prm := range p.Params {
		if prm.Key == "access" {
			got, _ = prm.Value.(string)
		}
	}
	if got == "" || !strings.ContainsAny(got, "абвгдеёжзийклмнопрстуфхцчшщыэюя") {
		t.Errorf("the access of the Russian preview is not in Russian: %q", got)
	}
	if l := engine.ParamLabel("access"); l != "доступ" {
		t.Errorf("label of access: %q, want «доступ»", l)
	}
}
