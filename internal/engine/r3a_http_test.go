// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Cursor r3a, R3A-1: POST /vms answered a one-element array whose object
// has no id. The object form of the same body was already an API error
// (TestFault_HTTP_CreateVMObjectWithoutID); the array form was taken for a
// VM with an empty id. Now it is an error too.
func TestCreateVM_ArrayWithoutIDIsAnError(t *testing.T) {
	a := faultAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{}]`))
	})
	vm, err := a.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-app", ProjectID: faultProject})
	if err == nil && (vm == nil || vm.ID == "") {
		t.Fatal("200 [{ }] accepted as a created VM with an empty id")
	}
	if err == nil {
		t.Fatalf("accepted VM %+v", vm)
	}
}
