// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

type bodyTrip func(*http.Request) (*http.Response, error)

func (f bodyTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Use the actual encoders. Inspect field names only; restore the body when
// a loopback fixture also consumes it. OIDC has two grants on one endpoint.
func checkAdditionalInventoryBodies(t *testing.T, inventory []inventoryRequest, seen map[*inventoryRequest]bool) {
	record := func(req *http.Request, svc string) error {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		fields := map[string]bool{}
		if svc == "oidc" {
			if !strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				return fmt.Errorf("wrong form content type")
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				return err
			}
			want := "client_credentials"
			if values.Has("code") {
				want = "authorization_code"
			}
			if values.Get("grant_type") != want {
				return fmt.Errorf("unexpected OIDC grant type for these fields")
			}
			for k, vs := range values {
				if len(vs) != 1 || vs[0] == "" {
					return fmt.Errorf("empty or repeated form field %s", k)
				}
				fields[k] = true
			}
		} else {
			var v any
			if err := json.Unmarshal(body, &v); err != nil {
				return err
			}
			bodyFields(v, "", fields)
		}
		for i := range inventory {
			e := &inventory[i]
			if e.service != svc || e.method != req.Method || !pathMatches(e.path, req.URL.Path) || len(fields) == 0 {
				continue
			}
			if svc == "oidc" && len(fields) != len(e.body) {
				continue
			}
			match := true
			for f := range fields {
				if !listed(e.body, f) {
					match = false
				}
			}
			if match {
				seen[e] = true
				return nil
			}
		}
		return fmt.Errorf("no matching body of %s %s %s: %v", svc, req.Method, req.URL.Path, fields)
	}
	t.Run("service clients", func(t *testing.T) {
		old := httpretry.DefaultBase
		t.Cleanup(func() { httpretry.DefaultBase = old })
		ctx := context.Background()
		transport := bodyTrip(func(req *http.Request) (*http.Response, error) {
			svc := map[string]string{"compute.api.cloud.ru": "compute", "iam.api.cloud.ru": "iam", "id.cloud.ru": "oidc"}[req.URL.Host]
			if svc == "" || req.URL.Scheme != "https" || req.Body == nil {
				return nil, fmt.Errorf("unexpected service request %s %s", req.Method, req.URL)
			}
			if err := record(req, svc); err != nil {
				return nil, err
			}
			response := `{"id":"fixture"}`
			switch req.URL.Path {
			case "/api/v1/permissions/bulk-evaluate":
				response = `{"results":[{"accessGranted":true}]}`
			case "/api/v1/auth/token", "/auth/system/openid/token":
				response = `{"access_token":"fake","expires_in":3600}`
			}
			return contractRT{status: 200, body: response}.RoundTrip(req)
		})
		httpretry.DefaultBase = transport
		client := &http.Client{Transport: transport}
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err := auth.NewIAMProvider(auth.Credentials{KeyID: "fixture-key", Secret: "fixture-secret"}, client, nil).Token(ctx)
		check(err)
		_, err = mk8s.FetchOIDCToken(ctx, auth.OIDCTokenURL, "fixture-key", "fixture-secret")
		check(err)
		iam := iamadmin.New(previewToken{}, "")
		_, err = iam.BulkEvaluate(ctx, []iamadmin.Check{{Action: "iam.permissions.create", ResourceID: pvProject}})
		check(err)
		_, err = iam.CreateServiceAccount(ctx, pvProject, "fixture", "test account")
		check(err)
		_, err = iam.CreateAccessKey(ctx, parityVM, "test key", time.Hour)
		check(err)
		g := iamadmin.Grant{SubjectID: parityVM, ResourceID: pvProject, RoleName: "fixture", ExpiresAt: time.Unix(1800000000, 0)}
		_, err = iam.CreatePermission(ctx, g)
		check(err)
		g.RoleID = parityVM
		_, err = iam.CreatePermission(ctx, g)
		check(err)
		check(iam.ExtendPermission(ctx, parityVM, g.ExpiresAt))
		cloud := provision.NewCloudRuAdapter(client, "")
		_, err = cloud.PriceCalculation(ctx, pvProject, provision.PriceCalculationRequest{TotalCount: 1, ImageID: pvImage, FlavorID: pvFlavor, Disks: []provision.PriceDisk{{DiskTypeID: parityVM, Size: 20}}, FloatingIPs: []provision.PriceFloatingIP{{Action: "new", Count: 1}}})
		check(err)
	})
	t.Run("login authorization code", func(t *testing.T) {
		setupCLI(t)
		w := grantingWorld(t, loginworld.Variant{}, login.ForAgent)
		prior := loginConfig
		loginConfig = func(say func(string, string), open func(string) error) login.Config {
			c := prior(say, open)
			base := c.HTTP.Transport
			c.HTTP.Transport = bodyTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.Scheme+"://"+req.URL.Host != w.URL {
					return nil, fmt.Errorf("outside login fixture")
				}
				if req.URL.Path == loginworld.IssuerPath+"/openid/token" && req.Method == "POST" {
					if err := record(req, "oidc"); err != nil {
						return nil, err
					}
				}
				return base.RoundTrip(req)
			})
			return c
		}
		// iamadmin uses its own client; only the loopback world may see a request.
		old := httpretry.DefaultBase
		t.Cleanup(func() { httpretry.DefaultBase = old })
		httpretry.DefaultBase = bodyTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.Scheme+"://"+req.URL.Host != w.URL {
				return nil, fmt.Errorf("outside IAM fixture")
			}
			return w.Client().Transport.RoundTrip(req)
		})
		if code, _, _ := runCLI(t, "", "--json", "login", "--for", "agent", "--key-ttl", "7d"); code != 0 {
			t.Fatalf("login exit %d; routes %v", code, w.Calls())
		}
	})
	t.Run("private DNS", func(t *testing.T) {
		w, _ := withJSONWorld(t)
		prior := w.rt.extra
		writes := []string{}
		w.rt.extra = func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "dns.api.cloud.ru" {
				switch req.Method + " " + req.URL.Path {
				case "GET /v1/privateRecordsSole":
					return contractRT{status: 200, body: `{"records":[]}`}.RoundTrip(req)
				case "GET /v1/privateZones/" + parityVM:
					return contractRT{status: 200, body: `{"id":"` + parityVM + `","project_id":"` + pvProject + `","name":"private","state":"ok"}`}.RoundTrip(req)
				case "POST /v1/privateZones", "POST /v1/privateRecordsSole":
					if err := record(req, "dns"); err != nil {
						return nil, err
					}
					writes = append(writes, req.URL.Path)
					return contractRT{status: 200, body: jsonOp}.RoundTrip(req)
				}
			}
			return prior(req)
		}
		for _, name := range []string{"dns_zone_create", "dns_record_create"} {
			args := copyArgs(parityArgs[name])
			args["type"] = "private"
			if name == "dns_zone_create" {
				args["description"] = "test private zone"
			}
			tool, _ := newRegistry().Lookup(name)
			before := len(writes)
			preview, _ := tool.Handler(context.Background(), args)
			if parseObject(preview)["status"] != "confirmation_required" || len(writes) != before {
				t.Fatalf("%s preview: %s", name, preview)
			}
			args["confirmed"] = true
			if out, failed := tool.Handler(context.Background(), args); failed {
				t.Fatalf("%s: %s", name, out)
			}
		}
		if !slices.Equal(writes, []string{"/v1/privateZones", "/v1/privateRecordsSole"}) {
			t.Fatal(writes)
		}
	})
}
