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
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

type imageCatalogPage struct {
	status int
	body   string
}

// previewCloud supplies the ordinary preview reads and fails on any cloud write.
// Only the project catalogue is delegated to the production adapter and pager.
type planImageCatalog struct {
	previewCloud
	adapter *provision.CloudRuAdapter
	t       *testing.T
	status  int
	body    string
	readErr error
	pages   []imageCatalogPage
	gets    int
	details int
}

func (c *planImageCatalog) ListImagesInProject(ctx context.Context, projectID string) ([]*provision.Image, error) {
	return c.adapter.ListImagesInProject(ctx, projectID)
}

func (c *planImageCatalog) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/images" || r.URL.Query().Get("project_id") != pvProject || r.Header.Get("Authorization") != "Bearer fixture" {
		c.t.Errorf("unexpected %s %s at project catalogue", r.Method, r.URL.Path)
		return nil, fmt.Errorf("unexpected request")
	}
	c.gets++
	if c.readErr != nil {
		return nil, c.readErr
	}
	page := imageCatalogPage{status: c.status, body: c.body}
	if c.pages != nil {
		index := c.gets - 1
		offset := r.URL.Query().Get("offset")
		if offset == "" {
			offset = "0"
		}
		if index >= len(c.pages) || offset != strconv.Itoa(index) {
			c.t.Errorf("unexpected page %d with offset %q", index+1, offset)
			return nil, fmt.Errorf("unexpected catalogue page")
		}
		page = c.pages[index]
	}
	return &http.Response{StatusCode: page.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(page.body)), Request: r}, nil
}

const privatePlanImage = "00000000-0000-0000-0000-0000000000f9"

func (c *planImageCatalog) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	if id != privatePlanImage {
		c.t.Errorf("unexpected image detail lookup")
	}
	c.details++
	return &provision.ImageDetail{ID: id, Type: provision.ImageTypePrivate, ProjectID: "foreign-fixture"}, nil
}

func planImageRefusal(t *testing.T, surface, image string, wantExit int) (map[string]any, string) {
	t.Helper()
	if surface == "CLI" || surface == "CLI text" {
		newProcess()
		args := []string{"deploy", "plan", "--mode", "manual", "--image", image, "--zone", pvZone, "--flavor", "lowcost10-2-4", "--ssh-key", "fixture"}
		if surface == "CLI" {
			args = append(args, "--json")
		}
		exit, stdout, stderr := runCLI(t, "", args...)
		if surface == "CLI text" {
			message, hint := personText(stderr)
			if exit != wantExit || stdout != "" || message == "" {
				t.Errorf("exit=%d, want %d, stdout=%q, stderr=%s", exit, wantExit, stdout, stderr)
			}
			return map[string]any{"error": message, "hint": hint}, stderr
		}
		var result struct{ Error output.ErrorBody }
		if err := json.Unmarshal([]byte(stderr), &result); err != nil || stdout != "" || exit != wantExit || result.Error.Code != exit {
			t.Errorf("exit=%d, want %d, stdout=%q, stderr=%s", exit, wantExit, stdout, stderr)
		}
		fields := result.Error.Details
		if fields == nil {
			fields = map[string]any{}
		}
		fields["reason"], fields["error"], fields["hint"] = result.Error.Reason, result.Error.Message, result.Error.Hint
		return fields, stderr
	}

	// Same ownership policy as an --allow-project server, on the fixture engine.
	s := mcp.New(func(o map[string]any) (*engine.Engine, error) {
		if p, _ := o["ProjectID"].(string); p != "" {
			if err := checkAllowedProject([]string{pvProject}, p); err != nil {
				return nil, err
			}
		}
		if o == nil {
			o = map[string]any{}
		}
		return toolEngine(o)
	}, "fixture")
	s.SetProjectBound()
	declareStateDir(s)
	args := map[string]any{"mode": "manual", "image": image, "zone": pvZone, "flavor": "lowcost10-2-4", "ssh_key": "fixture"}
	var raw []byte
	if surface == "stdio" {
		raw = stdioRaw(t, s, "deploy_plan", args)
	} else {
		h := mcp.NewHTTPHandler(s, mcp.HTTPOptions{})
		defer h.Close()
		body := roRequest("tools/call", map[string]any{"name": "deploy_plan", "arguments": args})
		req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
		}
		raw = rec.Body.Bytes()
	}
	answer, _ := roDecode(t, raw)
	var fields map[string]any
	if !answer.isErr || json.Unmarshal([]byte(answer.text), &fields) != nil || fields["status"] != "error" {
		t.Fatalf("expected tool refusal, got %s", raw)
	}
	return fields, string(raw)
}

func TestDeployPlan_ProjectImageCatalog(t *testing.T) {
	const empty = `{"items":[],"total":0}`
	const privateBody = `{"message":"private-service-message"}`
	for _, surface := range []string{"CLI", "CLI text", "stdio", "HTTP"} {
		t.Run(surface, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			isolateHome(t)
			t.Setenv("USERPROFILE", dir)
			t.Setenv("CLOUDRU_KEY_ID", fakeKeyID)
			t.Setenv("CLOUDRU_SECRET", fakeSecret)
			t.Setenv("EVO_LANG", "en")
			c := &planImageCatalog{t: t}
			c.adapter = provision.NewCloudRuAdapter(&http.Client{Transport: httpretry.UserAgent(c)}, "https://fixture.invalid")
			c.adapter.SetToken("fixture")
			c.adapter.Backoff = time.Millisecond
			withCloud(t, c)
			t.Cleanup(func() {
				if files, err := filepath.Glob(filepath.Join(config.DataDir(dir), "plans", "*")); err != nil || len(files) != 0 {
					t.Errorf("refused plans persisted files: %v (%v)", files, err)
				}
			})
			t.Run("paged_404", func(t *testing.T) { checkPlanCatalogPages(t, c, surface) })
			if surface == "CLI text" {
				return
			}
			for _, tc := range []struct {
				name   string
				status int
				body   string
				err    error
				reason string
				exit   int
			}{
				{"empty", 200, empty, nil, "NOT_FOUND", 4},
				{"authenticated_403", 403, privateBody, nil, "AUTH_FAILED", 2},
				{"expired_401", 401, privateBody, nil, "AUTH_EXPIRED", 2},
				{"rate_429", 429, privateBody, nil, "API_RATE_LIMITED", 5},
				{"server_500", 500, privateBody, nil, "API_ERROR", 5},
				{"unavailable_503", 503, privateBody, nil, "API_ERROR", 5},
				{"timeout", 0, "", context.DeadlineExceeded, "API_UNREACHABLE", 1},
				{"cancelled_read", 0, "", context.Canceled, "INTERRUPTED", 130},
				{"incomplete", 200, `{"items":[],"total":1}`, nil, "API_ERROR", 5},
				{"bad_items", 200, `{"items":"malformed"}`, nil, "API_ERROR", 5},
				{"null_page", 200, `null`, nil, "API_ERROR", 5},
				{"null_entry", 200, `{"items":[null],"total":1}`, nil, "API_ERROR", 5},
			} {
				t.Run(tc.name, func(t *testing.T) {
					c.status, c.body, c.readErr, c.gets = tc.status, tc.body, tc.err, 0
					fields, _ := planImageRefusal(t, surface, "selected-image", tc.exit)
					if fields["reason"] != tc.reason || c.gets == 0 || c.details != 0 {
						t.Errorf("got %v, want %s from project catalogue, reads=%d details=%d", fields, tc.reason, c.gets, c.details)
					}
					if tc.status == http.StatusForbidden && (fields["access_denied"] != true || fields["http_status"] != float64(403)) {
						t.Errorf("403 lost its access_denied or http_status: %v", fields)
					}
				})
			}
			if surface != "CLI" {
				t.Run("H9_private_image", func(t *testing.T) {
					c.gets, c.details = 0, 0
					fields, public := planImageRefusal(t, surface, privatePlanImage, 4)
					if fields["reason"] != "NOT_FOUND" || strings.Contains(public, "foreign-fixture") || c.gets != 0 || c.details != 1 {
						t.Errorf("expected neutral ownership refusal before catalogue lookup, got %s, details=%d reads=%d", public, c.details, c.gets)
					}
				})
			}
		})
	}
}

func checkPlanCatalogPages(t *testing.T, c *planImageCatalog, surface string) {
	t.Helper()
	const target = "selected-image"
	const other = "private-unrelated-image"
	const firstID = "00000000-0000-4000-8000-000000000021"
	const secondID = "00000000-0000-4000-8000-000000000022"
	row := func(id, name string, total int) imageCatalogPage {
		return imageCatalogPage{200, fmt.Sprintf(`{"items":[{"id":%q,"name":%q}],"total":%d}`, id, name, total)}
	}
	service := imageCatalogPage{404, `{"message":"private-service-message"}`}
	gateway := imageCatalogPage{404, `{"error_msg":"Not Found"}`}
	run := func(t *testing.T, pages []imageCatalogPage, reason string, exit int) (map[string]any, string) {
		t.Helper()
		c.pages, c.gets, c.details = pages, 0, 0
		defer func() { c.pages = nil }()
		fields, public := planImageRefusal(t, surface, target, exit)
		if c.gets != len(pages) || c.details != 0 {
			t.Errorf("read %d pages, want %d; detail lookups=%d", c.gets, len(pages), c.details)
		}
		if surface != "CLI text" && fields["reason"] != reason {
			t.Errorf("reason=%v, want %s: %s", fields["reason"], reason, public)
		}
		for _, hidden := range []string{pvProject, fakeKeyID, fakeSecret, "Bearer fixture", "/api/v1/images", "fixture.invalid", "private-service-message", firstID, secondID, other} {
			if strings.Contains(public, hidden) {
				t.Errorf("public answer contains catalogue data %q: %s", hidden, public)
			}
		}
		return fields, public
	}
	t.Run("neutral_absence", func(t *testing.T) {
		fields, hidden := run(t, []imageCatalogPage{service}, "NOT_FOUND", 4)
		_, empty := run(t, []imageCatalogPage{{200, `{"items":[],"total":0}`}}, "NOT_FOUND", 4)
		_, complete := run(t, []imageCatalogPage{row(firstID, other, 2), row(secondID, other+"-2", 2)}, "NOT_FOUND", 4)
		if hidden != empty || hidden != complete {
			t.Errorf("first service 404 and complete searches differ: %q / %q / %q", hidden, empty, complete)
		}
		_, denied := fields["access_denied"]
		_, status := fields["http_status"]
		if denied || status || strings.Contains(hidden, "404") {
			t.Errorf("neutral refusal exposed the service 404: %s", hidden)
		}
	})
	if surface == "CLI text" {
		t.Run("service_page_2_ru", func(t *testing.T) {
			t.Setenv("EVO_LANG", "ru")
			ru(t)
			fields, _ := run(t, []imageCatalogPage{row(firstID, target, 2), service}, "API_ERROR", 5)
			if fields["error"] != "не удалось дочитать каталог" {
				t.Errorf("unexpected Russian diagnostic: %v", fields)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		pages   []imageCatalogPage
		gateway bool
	}{
		{"service_page_2", []imageCatalogPage{row(firstID, other, 2), service}, false},
		{"service_page_3_after_match", []imageCatalogPage{row(firstID, target, 3), row(secondID, other, 3), service}, false},
		{"gateway_page_1", []imageCatalogPage{gateway}, true},
		{"gateway_page_2", []imageCatalogPage{row(firstID, other, 2), gateway}, true},
		{"gateway_page_3_after_match", []imageCatalogPage{row(firstID, target, 3), row(secondID, other, 3), gateway}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields, public := run(t, tc.pages, "API_ERROR", 5)
			if tc.gateway {
				if fields["hint"] != clierrors.RouteNotFoundHint() {
					t.Errorf("lost D63 routing hint: %v", fields)
				}
			} else if fields["error"] != "Could not finish reading the image catalogue." || strings.Contains(public, clierrors.RouteNotFoundHint()) {
				t.Errorf("incomplete catalogue not explained: %s", public)
			}
		})
	}
}
