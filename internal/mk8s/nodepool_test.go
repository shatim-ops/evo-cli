// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// GET /v3/nodePools: clusterId обязателен (openapi-v3.yaml mk8s,
// NodePoolService_List), список читается по страницам nextPageToken.
func TestListNodePools_ClusterRequiredAndPaged(t *testing.T) {
	const cluster = "00000000-0000-0000-0000-0000000000c1"
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v3/nodePools" || q.Get("clusterId") != cluster || q.Get("pageSize") != "100" {
			http.Error(w, `{"message":"clusterId is required"}`, 400)
			return
		}
		switch q.Get("pageToken") {
		case "":
			_, _ = io.WriteString(w, `{"nodePools":[{"id":"np1","name":"a"}],"nextPageToken":"p2"}`)
		case "p2":
			_, _ = io.WriteString(w, `{"nodePools":[{"id":"np2","name":"b"}],"nextPageToken":""}`)
		default:
			http.Error(w, `{"message":"bad token"}`, 400)
		}
	})
	nps, err := cli.ListNodePools(context.Background(), cluster)
	if err != nil {
		t.Fatal(err)
	}
	if len(nps) != 2 {
		t.Fatalf("node pools %d, want 2 (two pages)", len(nps))
	}
	if got := len(rec.reqs); got != 2 {
		t.Errorf("requests %d, want 2", got)
	}

	if _, err := cli.ListNodePools(context.Background(), ""); err == nil {
		t.Error("an empty cluster ID must be refused before the request")
	}
	if got := len(rec.reqs); got != 2 {
		t.Errorf("an empty cluster ID sent a request (%d total)", got)
	}
}

// Повтор токена — список не полон: ошибка, а не конец списка (break r4,
// R4-6; раньше повтор молча заканчивал список).
func TestListNodePools_RepeatedTokenIsError(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"nodePools":[{"id":"np1"}],"nextPageToken":"p2"}`)
	})
	if nps, err := cli.ListNodePools(context.Background(), "00000000-0000-0000-0000-0000000000c1"); err == nil {
		t.Fatalf("repeated token: %d node pools and no error", len(nps))
	}
	if got := len(rec.reqs); got != 2 {
		t.Errorf("requests %d, want 2", got)
	}
}
