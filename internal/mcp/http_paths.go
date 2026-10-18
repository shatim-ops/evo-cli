// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// insideWorkDir checks that path, after symlinks, lies inside the server's
// working directory. Over HTTP the caller is remote: a tool must not read
// files of the server's host outside the project it serves.
func insideWorkDir(path string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return err
	}
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(wd, p)
	}
	// A missing file is reported by the tool itself; resolve the part of
	// the path that exists (its nearest existing ancestor).
	p = filepath.Clean(p)
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = filepath.Join(r, rest)
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			p = filepath.Join(p, rest)
			break
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
	rel, err := filepath.Rel(root, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%q is outside the server's working directory", path)
	}
	return nil
}

// redacted replaces a credential in a tool result over HTTP.
const redacted = "[redacted over HTTP: start the server with --allow-secret-export]"

// secretKey reports whether a JSON object key names a credential.
func secretKey(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	switch k {
	case "secret", "token", "access_token", "id_token", "refresh_token", "bearer_token", "api_key", "apikey":
		return true
	}
	for _, s := range []string{"password", "passwd", "secret_key", "secretkey", "private_key", "privatekey", "client_secret", "clientsecret", "secret_access_key"} {
		if strings.Contains(strings.ReplaceAll(k, "_", ""), strings.ReplaceAll(s, "_", "")) {
			return true
		}
	}
	return false
}

// redactSecrets masks the string values of credential-named keys anywhere in
// a JSON tool result. Text that is not JSON is returned as is.
func redactSecrets(text string) string {
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		return text
	}
	if !redactValue(v) {
		return text
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return text
	}
	return string(out)
}

// redactValue masks in place and reports whether anything was masked.
func redactValue(v any) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok && s != "" && secretKey(k) {
				t[k] = redacted
				changed = true
				continue
			}
			if redactValue(x) {
				changed = true
			}
		}
	case []any:
		for _, x := range t {
			if redactValue(x) {
				changed = true
			}
		}
	}
	return changed
}
