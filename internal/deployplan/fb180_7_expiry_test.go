// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"strings"
	"testing"
	"time"
)

// Audit Fable 1.8, FB180-7 (3), the owner's decision 11.11: a plan with no
// expires_at never expired — Load checked the time only when it was set.
// deploy_plan sets it on every plan it saves (since 1.0), so a plan without
// it came from elsewhere (a repository with .evo/ in it): it is refused as
// expired, as a plan past its time is.
func TestLoad_PlanWithoutExpiryIsExpired(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		expires time.Time
		ok      bool
	}{
		{"in time", time.Now().Add(time.Minute), true},
		{"past its time", time.Now().Add(-time.Minute), false},
		{"without expires_at", time.Time{}, false},
	} {
		p := &Plan{PlanID: "plan-0123456789ab", ExpiresAt: c.expires}
		if err := Save(dir, p); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir, p.PlanID)
		if (err == nil) != c.ok {
			t.Errorf("%s: Load error %v, want ok %v", c.name, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "deploy_plan") {
			t.Errorf("%s: the error does not say to run deploy_plan again: %v", c.name, err)
		}
	}
}
