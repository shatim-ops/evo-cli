// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// An argument of echoedArgs is a line a person reads in the preview: it has
// a label of paramLabels, not its key, and the tool has the argument.
func TestEchoedArgs_DeclarationsHold(t *testing.T) {
	r := New(nil)
	for tool, keys := range echoedArgs {
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("echoedArgs names %s, which is no tool", tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, k := range keys {
			if _, has := props[k]; !has {
				t.Errorf("echoedArgs[%s]: no argument %s", tool, k)
			}
			if engine.ParamLabel(k) == strings.ReplaceAll(k, "_", "-") {
				t.Errorf("echoedArgs[%s]: %s has no label in paramLabels", tool, k)
			}
		}
	}
}

// A PEM chain and a template are named by a summary a person can check: the
// certificate's subject, issuer and last day, the SHA-256 of what is sent;
// Base64, as Certificate Manager takes a chain, reads as the PEM.
func TestSummarizedArgs(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Example CA"},
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	chain := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	fp := fmt.Sprintf("%x", sha256.Sum256(der))
	for _, v := range []string{chain, "\n " + chain, base64.StdEncoding.EncodeToString([]byte(chain))} {
		got := certChainSummary(v)
		for _, want := range []string{"1 certificate(s)", `"CN=Example CA"`, "2027-01-02", fp} {
			if !strings.Contains(got, want) {
				t.Errorf("certChainSummary: %q, want %q in it", got, want)
			}
		}
	}
	if got := certChainSummary("not a chain"); !strings.Contains(got, "11 bytes, 1 lines, SHA-256 ") {
		t.Errorf("certChainSummary of text: %q", got)
	}
	tpl := "#cloud-config\nusers:\n  - name: {{ user }}\n"
	got := templateSummary(tpl)
	for _, want := range []string{fmt.Sprintf("%d bytes, 3 lines", len(tpl)), fmt.Sprintf("%x", sha256.Sum256([]byte(tpl))), `first line "#cloud-config"`} {
		if !strings.Contains(got, want) {
			t.Errorf("templateSummary: %q, want %q in it", got, want)
		}
	}
	if strings.Contains(got, "{{ user }}") {
		t.Errorf("templateSummary prints the template: %q", got)
	}
}
