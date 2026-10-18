// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// GenerateSSHKeyPair creates an Ed25519 key pair.
// Returns the private key in OpenSSH PEM format and the public key in authorized_keys format.
func GenerateSSHKeyPair() (privateKeyPEM []byte, publicKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate ed25519 key: %w", err)
	}

	// Encode private key in OpenSSH format (compatible with both Go and ssh CLI).
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, "", fmt.Errorf("marshal private key: %w", err)
	}
	privateKeyPEM = pem.EncodeToMemory(block)

	// Encode public key in SSH authorized_keys format.
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", fmt.Errorf("create ssh public key: %w", err)
	}
	publicKey = string(ssh.MarshalAuthorizedKey(sshPub))

	return privateKeyPEM, publicKey, nil
}
