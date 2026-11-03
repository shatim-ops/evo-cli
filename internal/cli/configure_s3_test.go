// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Agent run M2 (Ц-10): the S3 keys could not be set by evo — configure
// had no word of them, and the refusal named only the variables. The S3
// key is the profile's own access key in another form (docs/auth.md):
// access_key <tenant_id>:<Key ID>, secret_key the Key Secret. So evo
// configure s3 needs the tenant ID alone, and no secret is typed twice.
func TestConfigureS3_FromTheProfilesKey(t *testing.T) {
	setupCLI(t)
	if err := config.SaveProfile("default", config.Profile{ProjectID: "11111111-1111-1111-1111-111111111111"},
		config.Credentials{KeyID: fakeKeyID, Secret: fakeSecret}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, "", "configure", "s3", "--tenant-id", "t-123")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	_, creds, _, err := config.LoadProfile("default")
	if err != nil {
		t.Fatal(err)
	}
	if creds.S3 == nil || creds.S3.AccessKey != "t-123:"+fakeKeyID || creds.S3.SecretKey != fakeSecret {
		t.Fatalf("s3 section: %+v", creds.S3)
	}
	if creds.KeyID != fakeKeyID || creds.Secret != fakeSecret {
		t.Errorf("the key of the profile changed: %+v", creds)
	}
	if strings.Contains(stdout+stderr, fakeSecret) || strings.Contains(stdout+stderr, fakeKeyID) {
		t.Errorf("the answer shows the key or the secret:\n%s%s", stdout, stderr)
	}
}

// Without a key in the profile there is nothing to build the S3 key from:
// CONFIG_INVALID, and nothing is written. A tenant ID with a space or a
// colon is no tenant ID: USAGE, before anything is written.
func TestConfigureS3_Refusals(t *testing.T) {
	setupCLI(t)
	code, _, stderr := runCLI(t, "", "configure", "s3", "--tenant-id", "t-123")
	if code != 2 || !strings.Contains(stderr, "evo configure") {
		t.Errorf("no key: exit %d, want 2 naming evo configure:\n%s", code, stderr)
	}
	if err := config.SaveProfile("default", config.Profile{}, config.Credentials{KeyID: fakeKeyID, Secret: fakeSecret}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "t 1", "t:1"} {
		code, _, stderr = runCLI(t, "", "configure", "s3", "--tenant-id", bad)
		if code != 1 {
			t.Errorf("tenant %q: exit %d, want 1:\n%s", bad, code, stderr)
		}
	}
	if _, creds, _, _ := config.LoadProfile("default"); creds.S3 != nil {
		t.Errorf("a refused call wrote the s3 section: %+v", creds.S3)
	}
}

// The refusal of an S3 tool without keys says how to set them, not only
// which variables (agent run M2, Ц-10).
func TestS3_NoKeysHintNamesConfigureS3(t *testing.T) {
	setupCLI(t)
	t.Setenv("CLOUDRU_S3_ACCESS_KEY", "")
	t.Setenv("CLOUDRU_S3_SECRET_KEY", "")
	code, _, stderr := runCLI(t, "", "s3", "buckets")
	if code != 2 || !strings.Contains(stderr, "evo configure s3") {
		t.Errorf("exit %d, want 2 with a hint naming evo configure s3:\n%s", code, stderr)
	}
}
