// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"
)

// --dry-run and a refused confirmation create nothing — not even the SSH
// key in ./.evo/ (BACKLOG [05]). With consent the key is made and
// lands in the image metadata.
func TestDeploy_SSHKeyOnlyAfterConsent(t *testing.T) {
	newEngine := func() (*Engine, *mockCloudAPI, DeployOptions, *int) {
		eng, cloud, opts, _, _ := newDeployForConfirm(t)
		calls := 0
		eng.SSHKeyPEM, eng.SSHPubKey = nil, ""
		eng.SSHKeyLoader = func() ([]byte, string, error) {
			calls++
			return []byte("pem"), "ssh-ed25519 AAAAkey", nil
		}
		return eng, cloud, opts, &calls
	}

	eng, _, opts, calls := newEngine()
	opts.DryRun = true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if *calls != 0 {
		t.Errorf("--dry-run loaded/created the SSH key")
	}

	eng, _, opts, calls = newEngine()
	opts.NonInteractive = true // no --yes: refused with code 3
	if err := eng.Deploy(context.Background(), opts); err == nil {
		t.Fatal("deploy without consent succeeded")
	}
	if *calls != 0 {
		t.Errorf("a refused deploy created the SSH key")
	}

	eng, cloud, opts, calls := newEngine()
	opts.Yes, opts.NonInteractive = true, true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if *calls != 1 || cloud.createReq.ImageMetadata["ssh_key"] != "ssh-ed25519 AAAAkey" {
		t.Errorf("calls=%d metadata=%v, want the key in ssh_key", *calls, cloud.createReq.ImageMetadata)
	}
}
