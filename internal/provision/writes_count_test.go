// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// The command of power the Compute client sends is a write of its call,
// counted by the client itself (httpretry.UserAgent) — the layer of
// intents keeps the record of a cut vm_start, vm_stop or vm_reboot by this
// count alone, not by a word of the tool's handler; a read is no write
// («Риск», conclusion 187, У2-5).
func TestSetVMPower_IsAWriteOfItsCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"vm-1","name":"web","state":"running"}`)
	}))
	defer srv.Close()
	a := NewCloudRuAdapter(nil, srv.URL) // the client evo builds: UserAgent
	a.SetToken("t")
	told := 0
	ctx := httpretry.CountWrites(context.Background(), func() error { told++; return nil })
	if _, err := a.GetVM(ctx, "vm-1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n != 0 || told != 0 {
		t.Fatalf("a read counted %d writes, told %d", n, told)
	}
	if err := a.SetVMPower(ctx, "vm-1", "reboot"); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n != 1 || told != 1 {
		t.Errorf("the command of power counted %d writes, told %d; want 1 and 1", n, told)
	}
}
