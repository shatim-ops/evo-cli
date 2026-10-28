// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// nameFree (D49): a free name lets the call go on; a taken one is USAGE with
// the existing id; a list that cannot be read is an error, not a free name —
// a create that cannot check the name does not ask for consent.
func TestNameFree(t *testing.T) {
	ctx := context.Background()
	listOf := func(items []map[string]any, err error) func(context.Context) ([]map[string]any, error) {
		return func(context.Context) ([]map[string]any, error) { return items, err }
	}
	taken := []map[string]any{{"id": "00000000-0000-4000-8000-0000000000d1", "name": "data", "status": "available"}}

	if ans, refused := nameFree(ctx, listOf(taken, nil), "disk", "other", "disk_list"); refused || ans != "" {
		t.Errorf("a free name refused: %s", ans)
	}
	ans, refused := nameFree(ctx, listOf(taken, nil), "disk", "data", "disk_list")
	var m map[string]any
	_ = json.Unmarshal([]byte(ans), &m)
	if !refused || m["reason"] != clierrors.CodeUsage || m["existing_id"] != "00000000-0000-4000-8000-0000000000d1" {
		t.Errorf("a taken name: want USAGE with existing_id: %s", ans)
	}
	ans, refused = nameFree(ctx, listOf(nil, errors.New("list down")), "disk", "data", "disk_list")
	if !refused || !strings.Contains(ans, "list down") || strings.Contains(ans, "confirmation_required") {
		t.Errorf("an unread list: want an error naming the cause: %s", ans)
	}
}
