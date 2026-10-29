// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D54 (#543, option A): a value the catalogue answered a list of, and the
// value is not in it, is refused before the question — NOT_FOUND, naming
// what there is and where to look; a list that was not read or names
// nothing decides nothing (the call goes as before).
func TestNotInCatalogue(t *testing.T) {
	known := []string{"v1.34.6", "v1.33.2"}
	if err := notInCatalogue("Kubernetes version", "1.34.6", known, "k8s_options"); err != nil {
		t.Errorf("1.34.6 against %v: %v (the leading v is not a difference)", known, err)
	}
	err := notInCatalogue("Kubernetes version", "v1.99.0", known, "k8s_options")
	var ce *clierrors.CLIError
	if !asCLI(err, &ce) || ce.Code != clierrors.CodeNotFound || !strings.Contains(ce.Message, "v1.99.0") ||
		!strings.Contains(ce.Message, "v1.33.2") || !strings.Contains(ce.Hint, "k8s_options") {
		t.Errorf("v1.99.0: %v", err)
	}
	for _, c := range []struct {
		value string
		known []string
	}{{"", known}, {"v1.99.0", nil}, {"v1.99.0", []string{}}} {
		if err := notInCatalogue("Kubernetes version", c.value, c.known, "k8s_options"); err != nil {
			t.Errorf("%q against %v: %v, want nothing decided", c.value, c.known, err)
		}
	}
}

// pgCatalogue is the PostgreSQL service of a world with one version, 17,
// and its specification; the rest as fakeDbaas.
func pgCatalogue(versions string) func(w http.ResponseWriter, r *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "postgresql.api.cloud.ru" {
			return false
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters":
			_, _ = io.WriteString(w, `{"clusters":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/versions":
			_, _ = io.WriteString(w, versions)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/specifications":
			_, _ = io.WriteString(w, `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","max_hosts":2}]}`)
		default:
			http.Error(w, `{"message":"no such path"}`, 404)
		}
		return true
	}
}

// D54, FB15-38: a DBaaS create with a version the service's list does not
// have, or a subnet_id that is not one of the project's subnets, asked for
// consent and failed after it; it is NOT_FOUND before the question, with
// confirmed or without, and nothing is created. A list without the field
// (versions without id or name) decides nothing.
func TestDbaasCreate_VersionAndSubnetNotInTheListRefusedBeforeTheQuestion(t *testing.T) {
	pgVersions := `{"versions":[{"id":"v17","name":"17"}]}`
	cases := []struct {
		label, tool, host, want string
		args                    map[string]any
	}{
		{"kafka version", "kafka_create", "kafka.api.cloud.ru", "v9", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "version_id": "v9"}},
		{"redis version", "redis_create", "redis.api.cloud.ru", "v9", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "version_id": "v9"}},
		{"pg version", "pg_create", "postgresql.api.cloud.ru", "99", map[string]any{"name": "web", "specification_id": "std", "version": "99"}},
		{"kafka subnet", "kafka_create", "kafka.api.cloud.ru", "sn-other", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "subnet_id": "sn-other"}},
		{"redis subnet", "redis_create", "redis.api.cloud.ru", "sn-other", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "subnet_id": "sn-other"}},
		{"pg subnet", "pg_create", "postgresql.api.cloud.ru", "sn-other", map[string]any{"name": "web", "specification_id": "std", "subnet_id": "sn-other"}},
	}
	for _, c := range cases {
		for _, confirmed := range []bool{false, true} {
			f := &apiFake{own: pgCatalogue(pgVersions)}
			args := map[string]any{"confirmed": confirmed}
			for k, v := range c.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, apiFakeRegistry(t, f), c.tool, args)
			if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(text, c.want) {
				t.Errorf("%s, confirmed=%v: %s", c.label, confirmed, text)
			}
			if body, ok := f.posted(c.host); ok {
				t.Errorf("%s: created: %s", c.label, body)
			}
		}
	}
	for _, c := range []struct {
		label, tool string
		args        map[string]any
	}{
		{"kafka, listed version and subnet", "kafka_create", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "version_id": "v2", "subnet_id": "sn-1"}},
		{"redis, listed version", "redis_create", map[string]any{"name": "c1", "cpu": 2, "ram": 4, "version_id": "v1"}},
		{"pg, listed version and subnet", "pg_create", map[string]any{"name": "web", "specification_id": "std", "version": "17", "subnet_id": "sn-1"}},
	} {
		m, text, _ := callNamed(t, apiFakeRegistry(t, &apiFake{own: pgCatalogue(pgVersions)}), c.tool, c.args)
		if m["status"] != "confirmation_required" {
			t.Errorf("%s: %s", c.label, text)
		}
	}
	// The list names no version: decides nothing, as before D54.
	m, text, _ := callNamed(t, apiFakeRegistry(t, &apiFake{own: pgCatalogue(`{"versions":[{}]}`)}), "pg_create",
		map[string]any{"name": "web", "specification_id": "std", "version": "99"})
	if m["status"] != "confirmation_required" {
		t.Errorf("pg, a list without names: %s", text)
	}
}

// D54, FB15-42: k8s_create and k8s_plan with a version no release channel
// lists failed after consent; it is NOT_FOUND before the question.
func TestK8sCreate_VersionNotInTheChannelsRefusedBeforeTheQuestion(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "version": "v1.99.0", "confirmed": confirmed})
		if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(text, "v1.99.0") || !strings.Contains(text, "v1.34.6") {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
	}
	if m, text, _ := callNamed(t, r, "k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "version": "1.34.6"}); m["status"] != "confirmation_required" {
		t.Errorf("a listed version: %s", text)
	}
	if n := f.postCount(); n != 0 {
		t.Errorf("sent %d POST", n)
	}
}

// natCloud is the fault world with SNAT gateways given as the API lists them.
type natCloud struct {
	*faultCloud
	gateways []map[string]any
}

func (c *natCloud) ListNATGateways(context.Context, string) ([]map[string]any, error) {
	return c.gateways, nil
}

// D54, FB15-17: a zone's router takes one SNAT gateway — a second one in
// the zone was refused after consent (422 «Router already …», Grok live
// run 1.4.0, 2 of 2). A gateway the list puts in the zone of the call is
// USAGE before the question, with its id; one of another zone, or one
// whose zone the answer does not name, decides nothing.
func TestSnatCreate_SecondGatewayInTheZoneRefusedBeforeTheQuestion(t *testing.T) {
	gw := func(id, zone string) map[string]any {
		m := map[string]any{"id": id, "name": "gw-" + id, "state": "created"}
		if zone != "" {
			m["availability_zone"] = map[string]any{"id": zone, "name": "z"}
		}
		return m
	}
	flat := map[string]any{"id": "g-1", "name": "gw-flat", "availability_zone_id": faultZone}
	for _, gws := range [][]map[string]any{{gw("g-1", faultZone)}, {flat}} {
		for _, confirmed := range []bool{false, true} {
			c := &natCloud{faultCloud: &faultCloud{}, gateways: gws}
			m, text, isErr := callNamed(t, natRegistry(t, c), "snat_create", map[string]any{"name": "nat", "confirmed": confirmed})
			if !isErr || m["reason"] != clierrors.CodeUsage || m["existing_id"] != "g-1" {
				t.Errorf("confirmed=%v: %s", confirmed, text)
			}
			if c.seen["CreateNATGateway"] > 0 {
				t.Errorf("confirmed=%v: created a second gateway", confirmed)
			}
		}
	}
	for label, gws := range map[string][]map[string]any{
		"another zone":  {gw("g-2", "00000000-0000-4000-8000-0000000000a9")},
		"zone not said": {gw("g-3", "")},
	} {
		c := &natCloud{faultCloud: &faultCloud{}, gateways: gws}
		if m, text, _ := callNamed(t, natRegistry(t, c), "snat_create", map[string]any{"name": "nat"}); m["status"] != "confirmation_required" {
			t.Errorf("%s: %s", label, text)
		}
	}
}

// asCLI is errors.As for a CLIError.
func asCLI(err error, ce **clierrors.CLIError) bool {
	if err == nil {
		return false
	}
	c, ok := err.(*clierrors.CLIError)
	*ce = c
	return ok
}

// natRegistry is the fault registry over cloud, in the fault world's zone.
func natRegistry(t *testing.T, cloud provision.CloudAPI) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: faultProject, Region: faultZone, KeyID: "k", Secret: "s"},
			Auth: staticToken{}, Cloud: cloud, Printer: output.NewWithWriter(&buf, output.ModeText, false)}, nil
	})
}
