// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package verify

import (
	"encoding/json"
	"testing"
	"time"
)

// "evo verify --json" keys are a contract: snake_case, latency in
// milliseconds, error only when there is one (QA r4 Q8).
func TestProbeResult_JSON(t *testing.T) {
	for _, tc := range []struct {
		r    ProbeResult
		want string
	}{
		{ProbeResult{Port: 8080, StatusCode: 200, Healthy: true, Latency: 1567 * time.Microsecond, Protocol: "http"},
			`{"port":8080,"status_code":200,"healthy":true,"protocol":"http","latency_ms":1}`},
		{ProbeResult{Port: 5432, Healthy: true, Latency: 12 * time.Millisecond, Protocol: "tcp"},
			`{"port":5432,"status_code":0,"healthy":true,"protocol":"tcp","latency_ms":12}`},
		{ProbeResult{Port: 9000, Latency: 3 * time.Second, Error: "connection refused"},
			`{"port":9000,"status_code":0,"healthy":false,"error":"connection refused","protocol":"","latency_ms":3000}`},
	} {
		got, err := json.Marshal(tc.r)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("got  %s\nwant %s", got, tc.want)
		}
	}

	// A slice, as verify prints it, uses the same method.
	got, _ := json.Marshal([]ProbeResult{{Port: 80, Latency: 2 * time.Millisecond}})
	if string(got) != `[{"port":80,"status_code":0,"healthy":false,"protocol":"","latency_ms":2}]` {
		t.Errorf("slice: %s", got)
	}
}
