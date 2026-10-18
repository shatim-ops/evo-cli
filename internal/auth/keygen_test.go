// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateSSHKeyPair(t *testing.T) {
	privPEM, pubKey, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error: %v", err)
	}

	// Private key should be non-empty and parseable PEM.
	if len(privPEM) == 0 {
		t.Fatal("private key PEM is empty")
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block from private key")
	}
	if block.Type != "OPENSSH PRIVATE KEY" {
		t.Errorf("expected PEM type %q, got %q", "OPENSSH PRIVATE KEY", block.Type)
	}

	// Public key should be non-empty and start with ssh-ed25519.
	if pubKey == "" {
		t.Fatal("public key is empty")
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("public key should start with 'ssh-ed25519', got: %s", pubKey[:30])
	}
}

func TestGenerateSSHKeyPair_Roundtrip(t *testing.T) {
	privPEM, _, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error: %v", err)
	}

	// Parse the private key back and create an SSH signer.
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("ssh.ParsePrivateKey() error: %v", err)
	}
	if signer == nil {
		t.Fatal("signer is nil")
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Errorf("expected key type ssh-ed25519, got %s", signer.PublicKey().Type())
	}
}
