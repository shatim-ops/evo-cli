// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// dropFirst answers the first n requests by closing the connection without
// a response — the transport error seen on ~1 of 3 calls from the
// container — and then with handler.
func dropFirst(t *testing.T, n int32, handler http.HandlerFunc) (*CloudRuAdapter, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= n {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		handler(w, r)
	})
	t.Cleanup(srv.Close)
	return adapter, &calls
}

func cliCode(err error) string {
	var ce *clierrors.CLIError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestGET_RetriesDroppedConnection(t *testing.T) {
	adapter, calls := dropFirst(t, 2, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"vm-1","state":"running"}`))
	})
	vm, err := adapter.GetVM(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("GetVM after two dropped connections: %v", err)
	}
	if vm.ID != "vm-1" || calls.Load() != 3 {
		t.Fatalf("vm=%+v calls=%d, want vm-1 after 3 calls", vm, calls.Load())
	}
}

// DELETE is idempotent: a repeat after a lost response answers 404, which
// destroy already reads as "already deleted".
func TestDELETE_RetriesDroppedConnection(t *testing.T) {
	adapter, calls := dropFirst(t, 1, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err := adapter.DeleteVM(context.Background(), "vm-1"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

// POST is never repeated: the server may have created the VM before the
// connection broke, and a second POST would create another one.
func TestPOST_DroppedConnectionNotRetried(t *testing.T) {
	adapter, calls := dropFirst(t, 1, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"vm-2"}]`))
	})
	_, err := adapter.CreateVM(context.Background(), CreateVMRequest{Name: "x"})
	if cliCode(err) != clierrors.CodeAPIUnreachable {
		t.Fatalf("CreateVM = %v, want API_UNREACHABLE", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no retry of POST)", calls.Load())
	}
	if !strings.Contains(err.(*clierrors.CLIError).Hint, "console") {
		t.Errorf("hint %q does not say the VM may exist", err.(*clierrors.CLIError).Hint)
	}
}

func TestGET_GivesUpAfterRetries(t *testing.T) {
	adapter, calls := dropFirst(t, 100, nil)
	_, err := adapter.GetVM(context.Background(), "vm-1")
	if cliCode(err) != clierrors.CodeAPIUnreachable {
		t.Fatalf("GetVM = %v, want API_UNREACHABLE", err)
	}
	if calls.Load() != httpretry.MaxRetries+1 {
		t.Fatalf("calls = %d, want %d", calls.Load(), httpretry.MaxRetries+1)
	}
}

func TestGET_CancelledContextNotRetried(t *testing.T) {
	adapter, calls := dropFirst(t, 100, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.GetVM(ctx, "vm-1")
	if err == nil || calls.Load() > 1 {
		t.Fatalf("err=%v calls=%d, want an error without retries", err, calls.Load())
	}
}

// 401/403 are credential problems (exit 2), not generic API errors (exit 5).
func TestAuthStatusesAreAuthErrors(t *testing.T) {
	for status, code := range map[int]string{
		http.StatusUnauthorized: clierrors.CodeAuthExpired,
		http.StatusForbidden:    clierrors.CodeAuthFailed,
	} {
		srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"denied"}`))
		})
		_, err := adapter.ListFlavors(context.Background())
		srv.Close()
		if cliCode(err) != code {
			t.Errorf("status %d: %v, want %s", status, err, code)
		}
		if clierrors.ExitCode(err) != clierrors.ExitAuth {
			t.Errorf("status %d: exit %d, want %d", status, clierrors.ExitCode(err), clierrors.ExitAuth)
		}
	}
}

// A 4xx is the API refusing this request; "try again later" is wrong
// advice for it. The status stays available to callers.
func TestClientErrorCarriesStatus(t *testing.T) {
	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":"vm_can_not_be_deleted_from_current_state"}`))
	})
	defer srv.Close()
	err := adapter.DeleteVM(context.Background(), "vm-1")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError {
		t.Fatalf("DeleteVM = %v", err)
	}
	if strings.Contains(ce.Hint, "try again later") {
		t.Errorf("hint for a 422: %q", ce.Hint)
	}
	if HTTPStatus(err) != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatus = %d, want 422", HTTPStatus(err))
	}
}
