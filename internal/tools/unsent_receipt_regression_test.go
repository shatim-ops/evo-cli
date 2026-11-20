// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"net"
	"testing"
)

func TestProcessUnsentEvidenceCannotCloseNextReceipt(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, err := r.newEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := intent.Dir(eng.StateDir)
	ctx := context.WithValue(context.Background(), callRecordsKey{}, &callRecords{})
	open := func(id string) (func(string), string) {
		end, err := r.beginIntent(ctx, "disk_delete", map[string]any{"disk_id": id, "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		return end, ctx.Value(callRecordsKey{}).(*callRecords).open.ID
	}
	endFirst, first := open(resGone)
	_, failed := deleteConnection(ctx, "disk_delete")
	dialErr := clierrors.Wrap(clierrors.CodeAPIUnreachable, "dial failed", "", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")})
	if !failed(dialErr) {
		t.Fatal("missing pre-send evidence")
	}
	endSecond, second := open(resMine)
	if first == second {
		t.Fatal("test requires distinct receipt IDs")
	}
	const unknown = `{"status":"error","reason":"API_UNREACHABLE"}`
	endSecond(unknown)
	endFirst(unknown)
	left := intent.Pending(dir)
	if len(left) != 1 || left[0].ID != second || !left[0].Ended {
		t.Fatalf("first receipt alone was proved unsent; expected second pending, got %+v", left)
	}
}
