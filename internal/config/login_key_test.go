// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

// A key set by evo configure (or any way but evo login) drops what evo
// login knew of the key before: logout must not delete a key by an id that
// is no longer the profile's (R-25).
func TestCredentials_ForKeyDropsTheLoginOfAnotherKey(t *testing.T) {
	c := Credentials{KeyID: "k1", Secret: "s1", Login: &LoginKey{ServiceAccountID: "sa", AccessKeyID: "ak"}}
	c.ForKey("k1", "s2")
	if c.Login == nil {
		t.Fatal("the same key dropped its login")
	}
	c.ForKey("k2", "s3")
	if c.Login != nil || c.KeyID != "k2" || c.Secret != "s3" {
		t.Fatalf("another key kept the login: %+v", c)
	}
}
