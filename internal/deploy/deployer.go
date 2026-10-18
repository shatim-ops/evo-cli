// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deploy

import (
	"context"
	"io"
)

// FileEntry represents a file to upload.
type FileEntry struct {
	RemotePath string
	Content    []byte
}

// Deployer abstracts SSH/SFTP operations for VM deployment.
type Deployer interface {
	// Connect establishes an SSH connection to the host.
	// keyPEM is the Ed25519 private key in PEM format.
	// knownHostKey is the expected host key fingerprint (empty on first connect = TOFU).
	Connect(ctx context.Context, host string, port int, user string, keyPEM []byte, knownHostKey string) error

	// HostKeyFingerprint returns the SSH host key fingerprint from the current connection.
	HostKeyFingerprint() string

	// Upload sends files to the remote host via SFTP.
	Upload(ctx context.Context, files []FileEntry) error

	// Exec runs a command on the remote host and returns stdout.
	Exec(ctx context.Context, cmd string) (string, error)

	// ExecStream runs a command, streaming its stdout/stderr to the writers
	// until it exits or ctx is cancelled. No timeout of its own.
	ExecStream(ctx context.Context, cmd string, stdout, stderr io.Writer) error

	// Close terminates the SSH connection.
	Close() error
}
