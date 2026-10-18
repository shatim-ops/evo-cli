// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_DefaultLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false)

	// Info message should be emitted.
	l.Info("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatal("expected Info message to appear in non-verbose mode")
	}

	buf.Reset()

	// Debug message should be suppressed.
	l.Debug("hidden")
	if strings.Contains(buf.String(), "hidden") {
		t.Fatal("expected Debug message to be suppressed in non-verbose mode")
	}
}

func TestNew_VerboseLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, true)

	l.Debug("details")
	if !strings.Contains(buf.String(), "details") {
		t.Fatal("expected Debug message to appear in verbose mode")
	}
}

func TestSanitize_BearerToken(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature"
	_, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("bearer token was not redacted: %s", got)
	}
	if !strings.Contains(got, "Bearer ****") {
		t.Fatalf("expected 'Bearer ****', got: %s", got)
	}
}

func TestSanitize_APIKey(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := "CLOUDRU_API_KEY=abc123def456ghi789jkl0"
	_, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "abc123") {
		t.Fatalf("API key value was not redacted: %s", got)
	}
	if !strings.Contains(got, "CLOUDRU_API_KEY=****") {
		t.Fatalf("expected 'CLOUDRU_API_KEY=****', got: %s", got)
	}
}

func TestSanitize_NoMatch(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := "just a regular log line with nothing sensitive"
	_, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if got != input {
		t.Fatalf("expected unchanged text %q, got %q", input, got)
	}
}

func TestSanitize_MultiplePatterns(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test CLOUDRU_API_KEY=supersecretvalue123456789 key=anotherlongcredentialvalue12345"
	_, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()

	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("bearer token was not redacted: %s", got)
	}
	if strings.Contains(got, "supersecret") {
		t.Fatalf("API key was not redacted: %s", got)
	}
	if strings.Contains(got, "anotherlongcredential") {
		t.Fatalf("generic key value was not redacted: %s", got)
	}
	if !strings.Contains(got, "Bearer ****") {
		t.Fatalf("expected 'Bearer ****' in output: %s", got)
	}
	if !strings.Contains(got, "CLOUDRU_API_KEY=****") {
		t.Fatalf("expected 'CLOUDRU_API_KEY=****' in output: %s", got)
	}
	if !strings.Contains(got, "key=****") {
		t.Fatalf("expected 'key=****' in output: %s", got)
	}
}

func TestSanitize_WrittenLength(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	n, err := sw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Write should report the original input length consumed.
	if n != len(input) {
		t.Fatalf("expected n=%d, got %d", len(input), n)
	}
}

func TestNew_LoggerOutputSanitized(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false)

	l.Info("auth", slog.String("header", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig"))

	got := buf.String()
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("bearer token leaked through slog output: %s", got)
	}
	if !strings.Contains(got, "Bearer ****") {
		t.Fatalf("expected sanitized bearer in slog output: %s", got)
	}
}

func TestSanitize_EnvSecrets(t *testing.T) {
	var buf bytes.Buffer
	sw := &sanitizeWriter{w: &buf}

	input := `MCP_BEARER_TOKEN=t CLOUDRU_SECRET=s3cr3t "CLOUDRU_S3_SECRET_KEY=abc" CLOUDRU_KEY_ID=visible`
	if _, err := sw.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	want := `MCP_BEARER_TOKEN=**** CLOUDRU_SECRET=**** "CLOUDRU_S3_SECRET_KEY=****" CLOUDRU_KEY_ID=visible`
	if got := buf.String(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
