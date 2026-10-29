// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// postBodies records the body of every POST to a path, before the world
// answers it.
type postBodies struct {
	next   http.RoundTripper
	mu     sync.Mutex
	bodies map[string]map[string]any
}

func (p *postBodies) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost && r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		p.mu.Lock()
		p.bodies[r.URL.Path] = m
		p.mu.Unlock()
	}
	return p.next.RoundTrip(r)
}

// README (1.2.1): «--region понимает имя». The VM paths resolve the
// configured zone by name (resolveVMZone); disk_create, snat_create and
// image_create sent CLOUDRU_REGION=ru.AZ-1 as availability_zone_id, and
// the platform refused it after consent (class D15; audit cli-ids-1.6,
// P-Z1). Configured by its name, the zone goes out as its UUID; a name
// that is no zone is refused before the question.
func TestCreate_ConfiguredZoneByName(t *testing.T) {
	faultToolsIsolate(t)
	a := &faultAPI{made: []faultResource{{host: "compute.api.cloud.ru", path: "/api/v1/disks", ID: "00000000-0000-4000-8000-0000000000d7", Name: "src", before: true}}}
	rt := &postBodies{next: a, bodies: map[string]map[string]any{}}
	old := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = old })
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
	cloud.Backoff = time.Millisecond
	region := "ru.AZ-1"
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s", Region: region},
			Auth:   staticToken{}, Cloud: cloud,
			Printer: output.NewWithWriter(&buf, output.ModeText, false), StateDir: t.TempDir(), Timings: faultToolsTimings(),
		}, nil
	})
	cases := []struct {
		tool, path string
		args       map[string]any
	}{
		{"disk_create", "/api/v1/disks", map[string]any{"name": "evo-d", "size_gb": float64(10)}},
		{"snat_create", "/api/v1/nat-gateways", map[string]any{"name": "evo-n"}},
		{"image_create", "/api/v1/images", map[string]any{"name": "evo-i", "disk_id": "00000000-0000-4000-8000-0000000000d7"}},
	}
	// A configured zone that is not there: refused before the question,
	// nothing written (axis 1).
	region = "ru.AZ-9"
	for _, c := range cases {
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		if !isErr || m["status"] == "confirmation_required" || len(rt.bodies) != 0 {
			t.Errorf("%s with zone ru.AZ-9 configured: want a refusal before the question: %s", c.tool, clip(text))
		}
	}
	region = "ru.AZ-1"
	for _, c := range cases {
		args := map[string]any{"confirmed": true}
		for k, v := range c.args {
			args[k] = v
		}
		_, text, _ := callNamed(t, r, c.tool, args)
		body := rt.bodies[c.path]
		zone := body["availability_zone_id"]
		if azs, ok := body["availability_zones"].([]any); ok && len(azs) == 1 {
			zone = azs[0].(map[string]any)["availability_zone_id"] // an image's targets
		}
		if zone != zoneAZ1 {
			t.Errorf("%s: zone %v, want the UUID of ru.AZ-1: %s", c.tool, zone, clip(text))
		}
	}
}
