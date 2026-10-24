// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// sizedDiskCloud is the fault world with a disk of size GB.
type sizedDiskCloud struct {
	*faultCloud
	size    int
	resized int
}

func (c *sizedDiskCloud) GetDisk(context.Context, string) (map[string]any, error) {
	return map[string]any{"id": faultDisk, "name": "data", "size": float64(c.size)}, nil
}

func (c *sizedDiskCloud) ResizeDisk(_ context.Context, _ string, size int, _ string) (map[string]any, error) {
	c.resized = size
	return map[string]any{"id": faultDisk, "size": float64(size)}, nil
}

func sizedDiskRegistry(t *testing.T, c *sizedDiskCloud) *Registry {
	t.Helper()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   c,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// disk_resize to a smaller or the same size failed after consent — the API
// refused the shrink, the same size changed nothing (G13-3, W5). The plan
// reads the disk before the question: both are USAGE before it, and the
// disk is not touched.
func TestDiskResize_ShrinkAndSameRefusedBeforeTheQuestion(t *testing.T) {
	for _, size := range []float64{50, 100} {
		for _, confirmed := range []bool{false, true} {
			c := &sizedDiskCloud{faultCloud: &faultCloud{}, size: 100}
			m, text, isErr := callNamed(t, sizedDiskRegistry(t, c), "disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": size, "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || c.resized != 0 {
				t.Errorf("size %v confirmed=%v: %s (resized %d)", size, confirmed, text, c.resized)
			}
		}
	}
	c := &sizedDiskCloud{faultCloud: &faultCloud{}, size: 100}
	if m, text, isErr := callNamed(t, sizedDiskRegistry(t, c), "disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": float64(150), "confirmed": true}); isErr || m["status"] != "resized" || c.resized != 150 {
		t.Errorf("a grow: %s", text)
	}
}
