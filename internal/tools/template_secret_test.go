// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// templateSecret stands for a password or a token a cloud-init template
// may carry, on its first line or in its body.
const templateSecret = "TEMPLATESECRET42"

// A cloud-init template may carry a secret, and a preview is text that
// reaches a model and a log (§4): the preview of image_create names the
// template by its size and its lines, and by its first line
// only where that line is a known header — never a comment the author
// wrote, nor a line of the body. The answer is one for MCP and --json, and
// the human preview shows the values of the same details
// (TestPreview_HumanShowsEveryValue).
func TestImageCreatePreview_TemplateSecretNotShown(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBody()}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	for _, tpl := range []string{
		"#cloud-config password: " + templateSecret + "\nusers: []\n",
		"# token " + templateSecret + "\n#cloud-config\n",
		"#!/bin/sh " + templateSecret + "\necho hi\n",
		"#cloud-config\nchpasswd:\n  list: root:" + templateSecret + "\n",
		"## template: jinja\n#cloud-config\nwrite_files:\n- content: " + templateSecret + "\n",
		"runcmd:\n- echo " + templateSecret + "\n",
	} {
		_, text, _ := callNamed(t, r, "image_create", map[string]any{"name": "img1", "disk_id": "00000000-0000-4000-8000-0000000000f1",
			"zone": "00000000-0000-4000-8000-0000000000f1", "user_data_template": tpl})
		if !strings.Contains(text, `"confirmation_required"`) {
			t.Fatalf("image_create gives no preview: %s", text)
		}
		if strings.Contains(text, templateSecret) {
			t.Errorf("the preview of image_create shows the template's text: %q\n%s", tpl, text)
		}
	}
}

// The first line is named where it is a header that says what the template
// is, and only then.
func TestTemplateSummary_KnownHeadersOnly(t *testing.T) {
	for first, named := range map[string]bool{
		"#cloud-config":          true,
		"## template: jinja":     true,
		"#cloud-boothook":        true,
		"#!/bin/bash":            true,
		"#!/usr/bin/env python3": true,
		"#cloud-config ":         false,
		"#!/bin/sh -x secret":    false,
		"# password: secret":     false,
		"#cloud-config secret":   false,
	} {
		got := templateSummary(first + "\nbody\n")
		if strings.Contains(got, first) != named {
			t.Errorf("templateSummary(%q): %q, the first line named: %v, want %v", first, got, !named, named)
		}
	}
}

// A short secret in a template whose other text is known is found from the
// SHA-256 of the whole by a dictionary, offline (risk conclusion 299): the
// preview names the template by its size and its lines, with no hash of it
// (release's decision 17.11).
func TestImageCreatePreview_NoTemplateHash(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBody()}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	tpl := "#cloud-config\nchpasswd:\n  list: root:" + templateSecret + "\n"
	_, text, _ := callNamed(t, r, "image_create", map[string]any{"name": "img1", "disk_id": "00000000-0000-4000-8000-0000000000f1",
		"zone": "00000000-0000-4000-8000-0000000000f1", "user_data_template": tpl})
	if !strings.Contains(text, `"confirmation_required"`) || !strings.Contains(text, fmt.Sprintf("%d bytes, 3 lines", len(tpl))) {
		t.Fatalf("image_create does not name the template by size and lines: %s", text)
	}
	if h := fmt.Sprintf("%x", sha256.Sum256([]byte(tpl))); strings.Contains(text, h) || strings.Contains(text, h[:16]) {
		t.Errorf("the preview of image_create names the template's SHA-256: %s", text)
	}
}
