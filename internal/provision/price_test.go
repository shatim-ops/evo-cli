// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	priceProject = "00000000-0000-4000-8000-000000000001"
	priceFlavor  = "00000000-0000-4000-8000-000000000002"
	priceImage   = "00000000-0000-4000-8000-000000000003"
	priceDisk    = "00000000-0000-4000-8000-000000000004"
)

// The request of the api-probe and its 200 answer: the documented body,
// the keys of the answer as seen live (mkp_image and custom_image_size are
// beyond the spec), plus one unknown key that must not break the decode.
func TestPriceCalculation(t *testing.T) {
	var got map[string]any
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/projects/"+priceProject+"/price-calculation" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type %q", r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("body %s: %v", b, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"custom_image_size":0,"disks":140.16,"flavor":1022.0,"floating_ips":146.0,
			"image":0,"mkp_image":0,"nat_gateways":0,"total_price_hour":1.7922,"total_price_month":1308.16,"future_key":{"x":1}}`)
	})
	defer srv.Close()

	pc, err := a.PriceCalculation(context.Background(), priceProject, PriceCalculationRequest{
		TotalCount:  1,
		FlavorID:    priceFlavor,
		ImageID:     priceImage,
		Disks:       []PriceDisk{{DiskTypeID: priceDisk, Size: 20}},
		FloatingIPs: []PriceFloatingIP{{Action: "new", Count: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := PriceCalculation{Flavor: 1022, Disks: 140.16, FloatingIPs: 146, TotalPriceHour: 1.7922, TotalPriceMonth: 1308.16}
	if *pc != want {
		t.Errorf("answer %+v, want %+v", *pc, want)
	}

	wantBody := `{"disks":[{"disk_type_id":"` + priceDisk + `","size":20}],"flavor_id":"` + priceFlavor +
		`","floating_ips":[{"count":1,"floating_ip_action":"new"}],"image_id":"` + priceImage + `","total_count":1}`
	if b, _ := json.Marshal(got); string(b) != wantBody {
		t.Errorf("body\n %s\nwant\n %s", b, wantBody)
	}
}

// Without a disk, a floating IP or an image the body has only what is set:
// the spec forbids empty disks/floating_ips arrays and other keys.
func TestPriceCalculationMinimalBody(t *testing.T) {
	var body string
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"total_price_month":1022,"total_price_hour":1.4}`)
	})
	defer srv.Close()
	if _, err := a.PriceCalculation(context.Background(), priceProject, PriceCalculationRequest{TotalCount: 1, FlavorID: priceFlavor}); err != nil {
		t.Fatal(err)
	}
	if want := `{"total_count":1,"flavor_id":"` + priceFlavor + `"}`; body != want {
		t.Errorf("body %s, want %s", body, want)
	}
}

// The 422 of a marketplace image without a SKU in billing (seen live for two
// Astra images): an API error with the status and the message, not retried.
func TestPriceCalculation422(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"detail":"sku_list[1]: (sku_identifier: cannot be blank.)."}`)
	})
	defer srv.Close()

	pc, err := a.PriceCalculation(context.Background(), priceProject, PriceCalculationRequest{TotalCount: 1, FlavorID: priceFlavor, ImageID: priceImage})
	if err == nil {
		t.Fatalf("no error, answer %+v", pc)
	}
	if HTTPStatus(err) != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatus = %d, want 422", HTTPStatus(err))
	}
	if !strings.Contains(err.Error(), "sku_identifier: cannot be blank") {
		t.Errorf("error %q has no message of the API", err)
	}
	if calls != 1 {
		t.Errorf("%d calls, want 1 (a 422 is not retried)", calls)
	}
}
