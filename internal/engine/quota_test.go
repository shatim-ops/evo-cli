// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
)

// D60 (FB15-8, P37): the preview of a VM does not keep silent about the
// project's quotas and does not refuse on them — it says they were not
// checked and why: a key of a project reads none (403), the read failed,
// or evo does not match their types yet. The same in the human preview of
// deploy and in the preview an agent reads; without a probe — nothing.
func TestPreview_SaysTheQuotaWasNotChecked(t *testing.T) {
	for _, c := range []struct {
		name   string
		probe  func(context.Context, auth.TokenProvider, string) error
		reason string
		line   string
	}{
		{"project key", func(context.Context, auth.TokenProvider, string) error {
			return fmt.Errorf("403: %w", ErrQuotasForbidden)
		}, QuotaProjectKey, "quota:     not checked (project key)"},
		{"read failed", func(context.Context, auth.TokenProvider, string) error { return errors.New("connection reset") }, QuotaNotRead, "not checked (not read)"},
		{"readable", func(context.Context, auth.TokenProvider, string) error { return nil }, QuotaNotMapped, "not checked (evo does not match quota types yet)"},
		{"no probe", nil, "", ""},
	} {
		eng, _, opts, stdout, _ := newDeployForConfirm(t)
		eng.QuotaProbe = c.probe
		opts.DryRun = true
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("%s: dry run: %v", c.name, err)
		}
		out := stdout.String()
		if c.line == "" {
			if strings.Contains(out, "quota") {
				t.Errorf("%s: the preview names a quota it has no probe for:\n%s", c.name, out)
			}
		} else if !strings.Contains(out, c.line) {
			t.Errorf("%s: the human preview lacks %q:\n%s", c.name, c.line, out)
		}
		eng.Config.ComposeFile, eng.Config.Flavor, eng.Config.Region = opts.ComposeFile, opts.Flavor, opts.Region
		p, err := eng.DeployPreview(context.Background())
		if err != nil || p == nil {
			t.Fatalf("%s: %v %v", c.name, p, err)
		}
		switch {
		case c.reason == "" && p.Quota != nil:
			t.Errorf("%s: preview quota %+v without a probe", c.name, p.Quota)
		case c.reason != "" && (p.Quota == nil || p.Quota.Checked || p.Quota.Reason != c.reason):
			t.Errorf("%s: preview quota %+v, want not checked, %s", c.name, p.Quota, c.reason)
		}
	}
}
