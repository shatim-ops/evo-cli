// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
)

// D82: the step that goes on with an unfinished deployment is one the
// tool takes — its arguments pass the one check of the registry, the
// default source of the ports (0.0.0.0/0) too.
func TestD82_NextArgsPassTheChecks(t *testing.T) {
	tools := map[string]Tool{}
	for _, tl := range New(nil).Tools() {
		tools[tl.Name] = tl
	}
	for tool, args := range map[string]map[string]any{
		"deploy":       {"compose_file": "/srv/app/docker-compose.yml", "cidr": "0.0.0.0/0", "confirmed": true},
		"deploy_apply": {"plan_id": "plan-0123456789ab", "confirmed": true},
	} {
		if err := CheckArgs(tools[tool], args); err != nil {
			t.Errorf("%s %v: %v", tool, args, err)
		}
	}
}
