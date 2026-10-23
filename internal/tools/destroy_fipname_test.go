// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// destroy без записи о FIP ищет его по имени, которое дал provision. Префикс
// «fip-web-» — начало и имени FIP ВМ «web-2»: при двух ВМ FIP ВМ «web» не
// находился и оставался платным (ревью Fable, A6). ID выдуманы.

type fipNameCloud struct {
	targetCloud
	fips []*provision.FloatingIP
}

func (c *fipNameCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return c.fips, nil
}
func (c *fipNameCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.releasedFIP = id
	return nil
}

func TestDestroyTargeted_FIPByExactBootName(t *testing.T) {
	stamp := time.Unix(1792000000, 0)
	other := time.Unix(1792000100, 0)
	for name, tc := range map[string]struct {
		fips []*provision.FloatingIP
		want string
	}{
		"web и web-2": {[]*provision.FloatingIP{
			{ID: "fip-w2", Name: provision.BootFIPName("web-2", stamp)},
			{ID: "fip-w", Name: provision.BootFIPName("web", stamp)},
		}, "fip-w"},
		"два FIP с именем ВМ — привязанный к ней": {[]*provision.FloatingIP{
			{ID: "fip-old", Name: provision.BootFIPName("web", stamp)},
			{ID: "fip-w", Name: provision.BootFIPName("web", other), VM: json.RawMessage(`{"id":"0000a1a1-0000-4000-8000-000000000001","name":"web"}`)},
		}, "fip-w"},
		"два FIP, ни один не привязан — не угадываем": {[]*provision.FloatingIP{
			{ID: "fip-a", Name: provision.BootFIPName("web", stamp)},
			{ID: "fip-b", Name: provision.BootFIPName("web", other)},
		}, ""},
		"только FIP ВМ web-2": {[]*provision.FloatingIP{
			{ID: "fip-w2", Name: provision.BootFIPName("web-2", stamp)},
		}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &fipNameCloud{fips: tc.fips}
			r := New(func(map[string]any) (*engine.Engine, error) {
				var buf bytes.Buffer
				return &engine.Engine{
					Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"},
					Auth:   staticToken{}, Cloud: cloud,
					Printer:  output.NewWithWriter(&buf, output.ModeText, false),
					StateDir: t.TempDir(),
					Timings:  engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
				}, nil
			})
			text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"name": "web", "confirmed": true})
			if isErr {
				t.Fatalf("destroy: %s", text)
			}
			if cloud.releasedFIP != tc.want {
				t.Errorf("released %q, want %q (%s)", cloud.releasedFIP, tc.want, text)
			}
		})
	}
}
