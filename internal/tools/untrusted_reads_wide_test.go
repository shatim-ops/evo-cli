// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// wideFields are fields a service answers beside the id, the name and the
// state, which the body of TestUntrustedClass_CloudValueMarkedOrQuoted does
// not give: the project, the times, a page token, a version, a zone, a type
// (release audit rc.1, A19-31).
var wideFields = []string{"projectId", "project_id", "createdAt", "created_at", "updatedAt", "updated_at",
	"nextPageToken", "next_page_token", "version", "zone", "type"}

// Every field of these a tool answers with, as the cloud gave it, is named
// in "_untrusted" or quoted in evo's words: the project in the preview of a
// delete, the times of an operation, the page token of the audit log, the
// version and zone of a node pool, the time and type of a backup (A19-31).
func TestUntrusted_WideFieldsOfTheCloudMarked(t *testing.T) {
	faultToolsIsolate(t)
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	wide := map[string]any{}
	for _, k := range wideFields {
		wide[k] = "wide"
	}
	var base any
	if err := json.Unmarshal(hostileBody(hostileNested(), wide), &base); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(hostileEverywhere(base, ""))
	t.Setenv("CLOUDRU_AGREEMENT_ID", "00000000-0000-4000-8000-0000000000a1")
	world := func(c *config.Config) {
		c.Region = "ru.AZ-1"
		c.Image = "ubuntu-22.04"
		c.S3 = config.S3Config{Endpoint: "https://s3.example.invalid", AccessKey: "a", SecretKey: "s", Region: "ru-central-1"}
	}
	api := &countingRT{RoundTripper: &oddAPI{status: http.StatusOK, body: body}}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	cloud.Backoff = 1
	r := raRegistry(t, api, cloud, world)
	r.SetCallBudget(containerBudget)
	checked := 0
	for _, tl := range r.Tools() {
		if untrustedClassPending[tl.Name+": *"] != "" {
			continue // a known place as a whole (billing_balance, #2434)
		}
		for _, call := range requiredCalls(tl) {
			maps.Copy(call, classWorldArgs[tl.Name])
			_, text, _ := callNamed(t, r, tl.Name, call)
			var m map[string]any
			if json.Unmarshal([]byte(text), &m) != nil {
				continue
			}
			checked++
			for _, f := range classLeaks(m) {
				if slices.Contains(wideFields, f) {
					t.Errorf("%s: the field %q carries a value of the cloud outside \"_untrusted\" %v", tl.Name, f, m["_untrusted"])
				}
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d answers read: the world lost the tools", checked)
	}
}
