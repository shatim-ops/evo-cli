// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// minio makes its request anew for each attempt: the attempts of one
// operation of S3 carry one X-Request-Id, as the attempts of every other
// client do, and two operations carry two (Astra, #1934). The world
// repeats: the first answer of each operation is 503.
func TestRequestID_OneForTheAttemptsOfAnOperation(t *testing.T) {
	retry, unit := minio.MaxRetry, minio.DefaultRetryUnit
	minio.MaxRetry, minio.DefaultRetryUnit = 2, 0
	t.Cleanup(func() { minio.MaxRetry, minio.DefaultRetryUnit = retry, unit })

	var mu sync.Mutex
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ids = append(ids, r.Header.Get(httpretry.RequestIDHeader))
		first := len(ids)%2 == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `<Error><Code>ServiceUnavailable</Code><Message>busy</Message></Error>`)
			return
		}
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult><Owner><ID>o</ID></Owner><Buckets></Buckets></ListAllMyBucketsResult>`)
	}))
	defer srv.Close()
	cli, err := New(Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := cli.ListBuckets(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(ids) != 4 {
		t.Fatalf("attempts: %d of 4: %v", len(ids), ids)
	}
	if ids[0] == "" || ids[0] != ids[1] || ids[2] != ids[3] {
		t.Errorf("the attempts of one operation carry other ids: %v", ids)
	}
	if ids[0] == ids[2] {
		t.Errorf("two operations carry one id: %v", ids)
	}
}

// Every method of Client that takes a ctx starts its operation first
// (httpretry.Operation): a method added without it would give the
// attempts of minio an id each.
func TestRequestID_EveryMethodIsAnOperation(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "client.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Type.Params.List) == 0 || len(fn.Type.Params.List[0].Names) == 0 ||
			fn.Type.Params.List[0].Names[0].Name != "ctx" {
			continue
		}
		first := ""
		if len(fn.Body.List) > 0 {
			if a, ok := fn.Body.List[0].(*ast.AssignStmt); ok && len(a.Rhs) == 1 {
				if c, ok := a.Rhs[0].(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok {
						first = s.Sel.Name
					}
				}
			}
		}
		if first != "Operation" {
			t.Errorf("%s: does not start with ctx = httpretry.Operation(ctx)", fn.Name.Name)
		}
	}
}
