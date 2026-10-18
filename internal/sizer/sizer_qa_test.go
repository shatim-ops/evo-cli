// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sizer

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A limit on one dimension only must not zero the other: postgres with
// cpus: 2 still needs its memory.
func TestEstimateFromServices_PartialLimitsKeepDefaults(t *testing.T) {
	got := EstimateFromServices([]compose.Service{
		{Name: "db", Image: "postgres:15", Resources: compose.Resources{CPUCores: 2}},
		{Name: "cache", Image: "redis:7", Resources: compose.Resources{MemoryMB: 1024}},
	})
	want := compose.Resources{CPUCores: 2 + 0.5, MemoryMB: 512 + 1024}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBaseImage_RegistryPortAndDigest(t *testing.T) {
	cases := map[string]string{
		"postgres":                             "postgres",
		"postgres:15":                          "postgres",
		"library/redis:7-alpine":               "redis",
		"localhost:5000/postgres":              "postgres",
		"localhost:5000/postgres:15":           "postgres",
		"registry.example.com:443/org/mysql:8": "mysql",
		"postgres@sha256:0123abcd":             "postgres",
		"postgres:15@sha256:0123abcd":          "postgres",
	}
	for in, want := range cases {
		if got := baseImage(in); got != want {
			t.Errorf("baseImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch_NoFlavorsOrNoneBigEnough(t *testing.T) {
	if m := Match(compose.Resources{CPUCores: 1, MemoryMB: 512}, nil); m != nil {
		t.Errorf("match from no flavors: %+v", m)
	}
	small := []provision.Flavor{{ID: "s", VCPUs: 1, RAMGB: 1}}
	if m := Match(compose.Resources{CPUCores: 64, MemoryMB: 512}, small); m != nil {
		t.Errorf("64 vCPU matched %+v", m.Flavor)
	}
}
