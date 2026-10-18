// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
)

// s3_get не тянет объект больше 10 МиБ в ответ MCP: USAGE с подсказкой
// про S3-клиент и консоль.
func TestS3Get_OverLimitIsUsage(t *testing.T) {
	size := s3GetMaxBytes + 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.Header().Set("Last-Modified", "Mon, 12 Oct 2026 10:00:00 GMT")
		w.Header().Set("ETag", `"e"`)
		if r.Method != http.MethodHead {
			_, _ = w.Write(make([]byte, size))
		}
	}))
	defer srv.Close()
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{S3: config.S3Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s"}}}, nil
	})
	m, text, isErr := callNamed(t, r, "s3_get", map[string]any{"bucket": "bucket", "key": "big.bin"})
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "S3 client") {
		t.Errorf("answer: %.300s", text)
	}
}
