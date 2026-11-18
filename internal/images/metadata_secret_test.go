// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The metadata of a VM carries a secret when evo fills a password field —
// its random password too — or the caller gives any value; evo's public
// key, login and hostname alone are no secret («Риск» to #2419).
func TestSendsSecret_APasswordOrAValueTheCallerGave(t *testing.T) {
	public := []provision.ImageMetadataField{
		{Name: "name", FieldType: TypeLinuxLogin, MandatoryGroup: "name"},
		{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication"},
		{Name: "hostname", FieldType: TypeHostname},
	}
	withPassword := append(append([]provision.ImageMetadataField(nil), public...), provision.ImageMetadataField{Name: "linux_password", FieldType: TypeLinuxPassword})
	in := Inputs{PublicKey: "ssh-ed25519 AAAA", Hostname: "web", Login: "user1", GenPassword: func() string { return "random" }}
	for _, c := range []struct {
		name   string
		fields []provision.ImageMetadataField
		values map[string]string
		want   bool
	}{
		{"evo's key, login and hostname", public, nil, false},
		{"a password evo makes", withPassword, nil, true},
		{"a value the caller gave", public, map[string]string{"name": "admin"}, true},
	} {
		in.Values = c.values
		_, res, err := FillMetadata(c.fields, in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := SendsSecret(c.values, res); got != c.want {
			t.Errorf("%s: SendsSecret %v, want %v (sensitive %v)", c.name, got, c.want, res.Sensitive)
		}
	}
}
