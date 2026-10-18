// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"io"
	"regexp"
)

// sanitizeWriter wraps an io.Writer and redacts sensitive patterns
// (tokens, API keys) before passing data through.
type sanitizeWriter struct {
	w io.Writer
}

var (
	// Bearer <token> where token is 20+ base64-like characters.
	bearerRe = regexp.MustCompile(`Bearer\s+[A-Za-z0-9+/=.\-]{20,}`)
	// CLOUDRU_API_KEY=<value>
	apiKeyRe = regexp.MustCompile(`CLOUDRU_API_KEY=[A-Za-z0-9+/=.\-]+`)
	// Other secrets passed through the environment, whatever their length:
	// the MCP bearer token may be short.
	envSecretRe = regexp.MustCompile(`\b(CLOUDRU_SECRET|CLOUDRU_S3_SECRET_KEY|MCP_BEARER_TOKEN)=[^\s"]+`)
	// Generic key=<value> where value is 20+ alphanumeric characters.
	genericKeyRe = regexp.MustCompile(`(key=)([A-Za-z0-9+/=.\-]{20,})`)
)

// Write implements io.Writer. It scans p for sensitive patterns,
// redacts them, and writes the sanitized bytes to the underlying writer.
func (s *sanitizeWriter) Write(p []byte) (n int, err error) {
	sanitized := bearerRe.ReplaceAll(p, []byte("Bearer ****"))
	sanitized = apiKeyRe.ReplaceAll(sanitized, []byte("CLOUDRU_API_KEY=****"))
	sanitized = envSecretRe.ReplaceAll(sanitized, []byte("${1}=****"))
	sanitized = genericKeyRe.ReplaceAllFunc(sanitized, func(match []byte) []byte {
		// Only replace if we haven't already redacted (avoid double-redacting
		// CLOUDRU_API_KEY which contains "key=").
		loc := genericKeyRe.FindSubmatchIndex(match)
		if loc == nil {
			return match
		}
		value := match[loc[4]:loc[5]]
		if string(value) == "****" {
			return match
		}
		return append(match[:loc[4]], []byte("****")...)
	})

	written, err := s.w.Write(sanitized)
	if written != len(sanitized) && err == nil {
		err = io.ErrShortWrite
	}
	// Return the original length so callers believe all bytes were consumed.
	return len(p), err
}
