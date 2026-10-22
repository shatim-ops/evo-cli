// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deploy"
)

// The engines of the parity worlds reach no host and wait milliseconds. A
// zero field of engine.Timings is the production wait (seconds to minutes
// between polls), and the SSH deployer of buildEngine dials the fake VM's
// address until it gives up: with a cloud that answers at once, both only
// make a test sleep. TestEngineCommands_RepeatedDeployGate spent 450 s of
// the 486 s of the whole suite so (process audit 1.2.1, PR 1). A field
// added to Timings is caught here until the worlds set it.
func TestParityWorlds_NoProductionWaits(t *testing.T) {
	for _, world := range []struct {
		name  string
		build func(t *testing.T)
	}{
		{"withParityWorld", func(t *testing.T) { withParityWorld(t) }},
		{"withJSONWorld", func(t *testing.T) { withJSONWorld(t) }},
	} {
		t.Run(world.name, func(t *testing.T) {
			world.build(t)
			e, err := toolEngine(nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ssh := e.Deployer.(*deploy.SSHDeployer); ssh || e.Deployer == nil {
				t.Errorf("Deployer = %T: a parity world reaches no host over SSH", e.Deployer)
			}
			if e.Probe == nil {
				t.Error("Probe = nil: deploy's health checks would reach the fake VM's address")
			}
			v := reflect.ValueOf(e.Timings)
			for i := range v.NumField() {
				f, d := v.Type().Field(i), v.Field(i).Interface().(time.Duration)
				if d <= 0 || d > time.Second {
					t.Errorf("Timings.%s = %v: a parity world waits at most a second, and zero is the production wait", f.Name, d)
				}
			}
		})
	}
}
