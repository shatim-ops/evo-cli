// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import "testing"

const sampleKC = `apiVersion: v1
kind: Config
clusters:
- name: pg-mon
  cluster:
    server: https://203.0.113.50:6443
    certificate-authority-data: LS0tLS1CRUdJTg==
contexts:
- name: pg-mon
  context: {cluster: pg-mon, user: pg-mon}
current-context: pg-mon
users:
- name: pg-mon
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      args:
      - get-token
      - --iam-url=https://id.cloud.ru/auth/system/openid/token
      command: cloudlogin
      interactiveMode: Never
`

func TestOIDCURLFromKubeconfig(t *testing.T) {
	got := OIDCURLFromKubeconfig(sampleKC)
	want := "https://id.cloud.ru/auth/system/openid/token"
	if got != want {
		t.Fatalf("iam-url: got %q want %q", got, want)
	}
}

func TestEmbedToken(t *testing.T) {
	out, err := EmbedToken(sampleKC, "TESTTOKEN123")
	if err != nil {
		t.Fatal(err)
	}
	// exec plugin must be gone; token must be present; server/CA preserved.
	for _, must := range []string{"token: TESTTOKEN123", "server: https://203.0.113.50:6443", "certificate-authority-data"} {
		if !contains(out, must) {
			t.Errorf("embedded kubeconfig missing %q\n---\n%s", must, out)
		}
	}
	for _, forbidden := range []string{"cloudlogin", "exec:"} {
		if contains(out, forbidden) {
			t.Errorf("embedded kubeconfig still has %q\n---\n%s", forbidden, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
