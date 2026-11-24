// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

func TestDiskDeleteInterruptedKeepsDoctorEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		switch req.Method {
		case "GET":
			fmt.Fprintf(rec, `{"id":%q,"name":"data","vms":[]}`, faultDisk)
		case "DELETE":
			writes++
			cancel()
			return nil, ctx.Err()
		default:
			t.Fatalf("unexpected %s", req.Method)
		}
		return rec.Result(), nil
	})
	r, dir := interruptedDeleteRegistry(t, rt)
	t.Chdir(dir)
	tl, _ := r.Lookup("disk_delete")
	text, failed := tl.Handler(ctx, map[string]any{"disk_id": faultDisk, "confirmed": true})
	if !failed || writes != 1 {
		t.Fatalf("writes=%d result=%s", writes, text)
	}
	pending := intent.Pending(intent.Dir(dir))
	if len(pending) != 1 || pending[0].Target != "disk_id="+faultDisk || pending[0].Key == nil || pending[0].Key.KeyID != config.MaskKeyID(provisionCfg.KeyID) || !pending[0].Sent {
		t.Fatalf("pending=%+v", pending)
	}
	report := doctorOnFake(t, r)
	if writes != 1 || len(intent.Pending(intent.Dir(dir))) != 1 || !strings.Contains(report, faultDisk) {
		t.Fatalf("writes=%d doctor=%s", writes, report)
	}
}
