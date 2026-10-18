// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// GetObject с пределом: объект больше предела — TooLargeError с размером,
// а не чтение целиком (объект Object Storage бывает до 5 ТБ).
func TestGetObject_Limit(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Last-Modified", "Mon, 12 Oct 2026 10:00:00 GMT")
		w.Header().Set("ETag", `"e"`)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c, err := New(Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := c.GetObject(context.Background(), "bucket", "k", 4096); err != nil || len(data) != len(body) {
		t.Fatalf("under the limit: %d bytes, %v", len(data), err)
	}
	_, err = c.GetObject(context.Background(), "bucket", "k", 1024)
	var tl *TooLargeError
	if !errors.As(err, &tl) || tl.Limit != 1024 || tl.Size != int64(len(body)) {
		t.Fatalf("over the limit: %#v", err)
	}
}
