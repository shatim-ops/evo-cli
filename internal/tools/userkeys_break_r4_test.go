// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Строка authorized_keys с опциями (command=…) — открытый ключ, который
// принимает ssh.ParseAuthorizedKey. loadUserKeys её выбрасывает: ни план,
// ни apply её не покажут и на ВМ не допишут, ошибки тоже нет.
func TestBreakR4_R4_10_OptionAuthorizedKeyKept(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	line := `command="echo hi",no-agent-forwarding ` + pub
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
		t.Fatalf("fixture is not an authorized_keys line: %v", err)
	}

	dir := t.TempDir()
	evo := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(evo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evo, userKeysFile), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	keys, name := loadUserKeys(dir)
	if name != userKeysFile || len(keys) != 1 || keys[0] != line {
		t.Fatalf("loadUserKeys = %q %q, want the option-prefixed line", name, keys)
	}
	// describeUserKeys принимает и отвергнутые строки: план показывает их
	// с error (R4-10); здесь их нет. Опции видны до подтверждения.
	shown := describeUserKeys(keys, nil)
	if len(shown) != 1 || shown[0]["error"] != nil || shown[0]["fingerprint"] == "" {
		t.Fatalf("describeUserKeys = %v", shown)
	}
	if opts, _ := shown[0]["options"].([]string); len(opts) != 2 || opts[0] != `command="echo hi"` || opts[1] != "no-agent-forwarding" {
		t.Errorf("options = %#v", shown[0]["options"])
	}
}
