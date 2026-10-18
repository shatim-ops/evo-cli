// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// R12: сервер без поля total — читается только первая страница.
func TestQAr3_ListAll_NoTotalField(t *testing.T) {
	const total = 130
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		var items []map[string]any
		for i := offset; i < total && i < offset+100; i++ {
			items = append(items, map[string]any{"id": fmt.Sprintf("x%d", i)})
		}
		if items == nil {
			items = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	})
	defer srv.Close()
	l, err := a.ListFloatingIPs(context.Background(), "p")
	if err != nil || len(l) != total {
		t.Fatalf("got %d, %v; want %d", len(l), err, total)
	}
}
