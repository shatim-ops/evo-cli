// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Кэш в странном состоянии — промах, а не ошибка команды: каталог вместо
// файла, токен без срока, пустой токен, чужой ключ в регистре, пустой файл.
// Записать кэш не удалось — команда всё равно получает токен из IAM.
func TestFileCache_BrokenStatesAreMisses(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	dir := t.TempDir()
	for name, content := range map[string]string{
		"no-expiry":  `{"key_id":"test-key-123","access_token":"t"}`,
		"bad-expiry": `{"key_id":"test-key-123","access_token":"t","expires_at":"tomorrow"}`,
		"empty-tok":  `{"key_id":"test-key-123","access_token":"","expires_at":"2999-01-01T00:00:00Z"}`,
		"key-case":   `{"key_id":"TEST-KEY-123","access_token":"t","expires_at":"2999-01-01T00:00:00Z"}`,
		"empty":      ``,
		"null":       `null`,
		"array":      `[]`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		before := calls.Load()
		tok, err := NewIAMProvider(testCreds, srv.Client(), FileCache{Path: path}).Token(context.Background())
		if err != nil || tok.Value == "t" || calls.Load() != before+1 {
			t.Errorf("%s: token %q, err %v, IAM calls %d", name, tok.Value, err, calls.Load()-before)
		}
	}

	asDir := filepath.Join(dir, "is-a-dir")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewIAMProvider(testCreds, srv.Client(), FileCache{Path: asDir}).Token(context.Background()); err != nil {
		t.Errorf("cache path is a directory: %v", err)
	}
}
