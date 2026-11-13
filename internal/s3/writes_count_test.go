// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// A write of S3 is a write of its call, counted by the client itself
// (httpretry.UserAgent): before 1.9 the S3 client counted nothing, and a
// cut put or delete would have left no record once the layer of intents
// closes the record of a call that sent no write («Риск», conclusion 187,
// У2).
func TestWrites_OfS3AreCountedForTheirCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult><Owner><ID>o</ID></Owner><Buckets></Buckets></ListAllMyBucketsResult>`)
		}
	}))
	defer srv.Close()
	cli, err := New(Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s", Region: "ru-central-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := httpretry.CountWrites(context.Background(), nil)
	if _, err := cli.ListBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n != 0 {
		t.Fatalf("a read counted %d writes", n)
	}
	if _, err := cli.PutObject(ctx, "bkt", "k", []byte("x"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := cli.RemoveObject(ctx, "bkt", "k"); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n < 2 {
		t.Errorf("a put and a delete counted %d writes, want 2 or more", n)
	}
}
