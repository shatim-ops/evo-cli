// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func setupTestServer(handler http.HandlerFunc) (*httptest.Server, *CloudRuAdapter) {
	srv := httptest.NewServer(handler)
	adapter := NewCloudRuAdapter(srv.Client(), srv.URL)
	adapter.SetToken("test-token")
	adapter.Backoff = time.Millisecond
	return srv, adapter
}

func TestCreateVM(t *testing.T) {
	want := VM{
		ID:       "vm-123",
		Name:     "my-vm",
		Status:   "creating",
		FlavorID: "flavor-small",
		ImageID:  "img-456",
		Region:   "ru-1",
		PublicIP: "1.2.3.4",
	}

	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1.1/vms" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header")
		}

		var reqs []CreateVMRequest
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if len(reqs) != 1 {
			t.Fatalf("expected 1 VM request, got %d", len(reqs))
		}
		if reqs[0].Name != "my-vm" {
			t.Errorf("expected name my-vm, got %s", reqs[0].Name)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]VM{want})
	})
	defer srv.Close()

	got, err := adapter.CreateVM(context.Background(), CreateVMRequest{
		Name:               "my-vm",
		FlavorID:           "flavor-small",
		AvailabilityZoneID: "ru-1",
		ImageID:            "img-456",
	})
	if err != nil {
		t.Fatalf("CreateVM returned error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.FlavorID != want.FlavorID {
		t.Errorf("FlavorID = %q, want %q", got.FlavorID, want.FlavorID)
	}
	if got.PublicIP != want.PublicIP {
		t.Errorf("PublicIP = %q, want %q", got.PublicIP, want.PublicIP)
	}
}

func TestGetVM(t *testing.T) {
	want := VM{
		ID:       "vm-789",
		Name:     "test-vm",
		Status:   "running",
		FlavorID: "flavor-large",
		Region:   "ru-2",
		PublicIP: "5.6.7.8",
	}

	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/vms/vm-789" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	})
	defer srv.Close()

	got, err := adapter.GetVM(context.Background(), "vm-789")
	if err != nil {
		t.Fatalf("GetVM returned error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.Region != want.Region {
		t.Errorf("Region = %q, want %q", got.Region, want.Region)
	}
	if got.PublicIP != want.PublicIP {
		t.Errorf("PublicIP = %q, want %q", got.PublicIP, want.PublicIP)
	}
}

func TestDeleteVM(t *testing.T) {
	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/vms/vm-del" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := adapter.DeleteVM(context.Background(), "vm-del")
	if err != nil {
		t.Fatalf("DeleteVM returned error: %v", err)
	}
}

func TestListFlavors(t *testing.T) {
	var callCount atomic.Int32
	items := []Flavor{
		{ID: "f-1", Name: "m1.small", VCPUs: 2, RAMGB: 4},
		{ID: "f-2", Name: "m1.large", VCPUs: 8, RAMGB: 16},
	}

	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if r.URL.Path != "/api/v1/flavors" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse[Flavor]{Items: items, Total: len(items)})
	})
	defer srv.Close()

	// First call: should hit the server.
	got, err := adapter.ListFlavors(context.Background())
	if err != nil {
		t.Fatalf("ListFlavors returned error: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("got %d flavors, want %d", len(got), len(items))
	}
	for i := range items {
		if got[i].Name != items[i].Name {
			t.Errorf("Flavor[%d].Name = %q, want %q", i, got[i].Name, items[i].Name)
		}
		if got[i].VCPUs != items[i].VCPUs {
			t.Errorf("Flavor[%d].VCPUs = %d, want %d", i, got[i].VCPUs, items[i].VCPUs)
		}
	}

	// Second call: should use cache, no additional server hit.
	got2, err := adapter.ListFlavors(context.Background())
	if err != nil {
		t.Fatalf("ListFlavors (cached) returned error: %v", err)
	}
	if len(got2) != len(items) {
		t.Fatalf("cached: got %d flavors, want %d", len(got2), len(items))
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 server call, got %d (cache not working)", callCount.Load())
	}
}

func TestRetryOn429(t *testing.T) {
	var callCount atomic.Int32
	want := VM{ID: "vm-retry", Status: "active"}

	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	})
	defer srv.Close()

	got, err := adapter.GetVM(context.Background(), "vm-retry")
	if err != nil {
		t.Fatalf("GetVM with retries returned error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if callCount.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", callCount.Load())
	}
}

func TestBackoffDefault(t *testing.T) {
	a := NewCloudRuAdapter(nil, "")
	if got := a.firstBackoff(); got != time.Second {
		t.Errorf("default first backoff = %v, want 1s", got)
	}
	a.Backoff = 5 * time.Millisecond
	if got := a.firstBackoff(); got != 5*time.Millisecond {
		t.Errorf("injected first backoff = %v, want 5ms", got)
	}
}

func TestNotFound(t *testing.T) {
	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()

	_, err := adapter.GetVM(context.Background(), "vm-nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierrors.CodeVMNotFound {
		t.Errorf("Code = %q, want %q", cliErr.Code, clierrors.CodeVMNotFound)
	}
}

func TestServerError(t *testing.T) {
	srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	defer srv.Close()

	_, err := adapter.GetVM(context.Background(), "vm-err")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierrors.CodeAPIError {
		t.Errorf("Code = %q, want %q", cliErr.Code, clierrors.CodeAPIError)
	}
}

func TestConnectionError(t *testing.T) {
	// Create adapter pointing to a closed server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	adapter := NewCloudRuAdapter(srv.Client(), srv.URL)
	adapter.SetToken("test-token")
	adapter.Backoff = time.Millisecond // GET is retried before giving up
	srv.Close()                        // close immediately to simulate unreachable

	_, err := adapter.GetVM(context.Background(), "vm-unreach")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierrors.CodeAPIUnreachable {
		t.Errorf("Code = %q, want %q", cliErr.Code, clierrors.CodeAPIUnreachable)
	}
}
