// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sizer

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

var testFlavors = []provision.Flavor{
	{Name: "free", VCPUs: 2, RAMGB: 4, DiskGB: 30},
	{Name: "m1.small", VCPUs: 2, RAMGB: 8, DiskGB: 50},
	{Name: "m1.medium", VCPUs: 4, RAMGB: 16, DiskGB: 100},
	{Name: "m1.large", VCPUs: 8, RAMGB: 32, DiskGB: 200},
}

func TestMatch_ExactFit(t *testing.T) {
	// 2 vCPU, 3072 MB -> with headroom ceil(2.6)=3, 3994 MB.
	// Needs 3 vCPU and ~3994 MB -> m1.medium (4 vCPU, 16 GB).
	// But let's pick something that lands exactly on free tier:
	// We need resources that after headroom match exactly 2 vCPU, 4 GB.
	// ceil(cpu*1.3) <= 2 -> cpu <= 1.53...; ceil(mem*1.3) <= 4096 -> mem <= 3150
	res := compose.Resources{CPUCores: 1.5, MemoryMB: 3000}
	m := Match(res, testFlavors)
	if m == nil {
		t.Fatal("expected a match, got nil")
	}
	if m.Flavor.Name != "free" {
		t.Errorf("expected free, got %s", m.Flavor.Name)
	}
	if m.Headroom != 1.3 {
		t.Errorf("expected headroom 1.3, got %f", m.Headroom)
	}
}

func TestMatch_HeadroomApplied(t *testing.T) {
	// 1 vCPU -> with headroom ceil(1.3)=2, should fit "free" (2 vCPU, 4 GB).
	// 1024 MB -> ceil(1331.2)=1332 MB, free has 4096 MB -> OK.
	res := compose.Resources{CPUCores: 1.0, MemoryMB: 1024}
	m := Match(res, testFlavors)
	if m == nil {
		t.Fatal("expected a match, got nil")
	}
	if m.Flavor.Name != "free" {
		t.Errorf("expected free, got %s", m.Flavor.Name)
	}
}

func TestMatch_NoResources(t *testing.T) {
	// Zero resources -> defaults to 1 vCPU, 1024 MB.
	// With headroom: ceil(1.3)=2 vCPU, ceil(1331.2)=1332 MB.
	// "free" (2 vCPU, 4096 MB) satisfies.
	res := compose.Resources{}
	m := Match(res, testFlavors)
	if m == nil {
		t.Fatal("expected a match, got nil")
	}
	if m.Flavor.Name != "free" {
		t.Errorf("expected free, got %s", m.Flavor.Name)
	}
}

func TestMatch_NoSufficientFlavor(t *testing.T) {
	// Request way more than the largest flavor.
	res := compose.Resources{CPUCores: 100, MemoryMB: 999999}
	m := Match(res, testFlavors)
	if m != nil {
		t.Errorf("expected nil, got %+v", m)
	}
}

func TestMatch_SortsBySize(t *testing.T) {
	// Provide flavors in reverse order to verify sorting works.
	reversed := []provision.Flavor{
		{Name: "m1.large", VCPUs: 8, RAMGB: 32, DiskGB: 200},
		{Name: "m1.medium", VCPUs: 4, RAMGB: 16, DiskGB: 100},
		{Name: "free", VCPUs: 2, RAMGB: 4, DiskGB: 30},
	}
	// 1 vCPU, 1024 MB -> headroom -> 2 vCPU, 1332 MB -> "free" is smallest.
	res := compose.Resources{CPUCores: 1.0, MemoryMB: 1024}
	m := Match(res, reversed)
	if m == nil {
		t.Fatal("expected a match, got nil")
	}
	if m.Flavor.Name != "free" {
		t.Errorf("expected free (smallest sufficient), got %s", m.Flavor.Name)
	}
}

func TestEstimateFromServices_WithLimits(t *testing.T) {
	services := []compose.Service{
		{Name: "app", Image: "myapp:latest", Resources: compose.Resources{CPUCores: 2.0, MemoryMB: 2048}},
		{Name: "worker", Image: "myworker:v1", Resources: compose.Resources{CPUCores: 1.0, MemoryMB: 512}},
	}
	total := EstimateFromServices(services)
	if total.CPUCores != 3.0 {
		t.Errorf("expected 3.0 CPUCores, got %f", total.CPUCores)
	}
	if total.MemoryMB != 2560 {
		t.Errorf("expected 2560 MemoryMB, got %d", total.MemoryMB)
	}
}

func TestEstimateFromServices_ImageDefaults(t *testing.T) {
	services := []compose.Service{
		{Name: "db", Image: "postgres:15"},
	}
	total := EstimateFromServices(services)
	if total.CPUCores != 1.0 {
		t.Errorf("expected 1.0 CPUCores for postgres, got %f", total.CPUCores)
	}
	if total.MemoryMB != 512 {
		t.Errorf("expected 512 MemoryMB for postgres, got %d", total.MemoryMB)
	}
}

func TestEstimateFromServices_UnknownImage(t *testing.T) {
	services := []compose.Service{
		{Name: "custom", Image: "mycustomimage:v3"},
	}
	total := EstimateFromServices(services)
	if total.CPUCores != 0.5 {
		t.Errorf("expected 0.5 CPUCores fallback, got %f", total.CPUCores)
	}
	if total.MemoryMB != 256 {
		t.Errorf("expected 256 MemoryMB fallback, got %d", total.MemoryMB)
	}
}

func TestEstimateFromServices_Mixed(t *testing.T) {
	services := []compose.Service{
		{Name: "api", Image: "myapi:latest", Resources: compose.Resources{CPUCores: 2.0, MemoryMB: 1024}},
		{Name: "cache", Image: "redis:7-alpine"},
		{Name: "db", Image: "postgres:15"},
	}
	total := EstimateFromServices(services)
	// api: 2.0 + redis: 0.5 + postgres: 1.0 = 3.5
	if total.CPUCores != 3.5 {
		t.Errorf("expected 3.5 CPUCores, got %f", total.CPUCores)
	}
	// api: 1024 + redis: 256 + postgres: 512 = 1792
	if total.MemoryMB != 1792 {
		t.Errorf("expected 1792 MemoryMB, got %d", total.MemoryMB)
	}
}
