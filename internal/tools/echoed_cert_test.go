// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// testCert is a valid self-signed X.509 certificate in PEM, made here: its
// subject (and so its issuer) has the common name cn, its alternative
// names are dns. The dates are fixed, not the clock's.
func testCert(cn string, dns ...string) string {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dns,
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// validCertTexts are the shapes of a valid certificate with a secret in its
// text outside the subject and the issuer — the parts a summary of a chain
// decodes (Астра #2242: a PEM whose body is a marker is no certificate,
// so the shapes of echoedTexts reach the fall-back summary only).
var validCertTexts = sync.OnceValue(func() []string {
	m := echoedMarker
	inSAN := testCert("example.test", m+".example.test")
	plain := testCert("example.test")
	return []string{
		inSAN,
		base64.StdEncoding.EncodeToString([]byte(inSAN)),
		"# password " + m + "\n" + plain,
		plain + "# token " + m + "\n",
		plain + "-----BEGIN PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString([]byte(m)) + "\n-----END PRIVATE KEY-----\n",
		plain + inSAN,
	}
})

// certPublicNames are the parts of a certificate a summary of a chain
// names as the certificate has them, with why that is no secret: the
// summary tells the person which certificate the consent uploads (CLAUDE.md
// §6, axis 4). Any other text of the argument stays out of the preview.
var certPublicNames = map[string]string{
	"subject": "a server sends its certificate, and so its subject, to every TLS client that connects",
	"issuer":  "the subject of the certificate above it in the chain, as public as that subject",
}

// certPublicPart is a part of certPublicNames in a summary: the name, then
// the value in quotes as quoteCloud gives it (RU: «субъект», «издатель»).
var certPublicPart = regexp.MustCompile(`(subject|issuer|субъект|издатель) "(?:[^"\\]|\\.)*"`)

// A summary of a certificate chain names a valid certificate by its
// public parts (certPublicNames) and nothing else of the text: a secret
// in a comment, in an alternative name, in a block of a key beside the
// certificate, stays out, and a marker in the subject is shown only there.
func TestEchoedArgs_CertificateNamesItsPublicPartsOnly(t *testing.T) {
	n := 0
	for _, tool := range sortedDecls() {
		for _, a := range decls[tool].EchoedArgs {
			if a.summary == nil {
				continue
			}
			for _, text := range validCertTexts() {
				if got := a.summary(text); strings.Contains(got, echoedMarker) {
					t.Errorf("%s %s: the summary names the text of a valid certificate outside its subject and issuer: %q", tool, a.name, got)
				}
			}
			if got := a.summary(testCert("example.test")); !strings.Contains(got, `"CN=example.test"`) {
				continue // a summary that decodes no certificate
			}
			n++
			for _, text := range []string{testCert(echoedMarker), testCert(echoedMarker, echoedMarker+".example.test")} {
				got := a.summary(text)
				if !strings.Contains(got, echoedMarker) {
					t.Errorf("%s %s: the summary does not name the subject of the certificate: %q", tool, a.name, got)
				}
				if rest := certPublicPart.ReplaceAllString(got, ""); strings.Contains(rest, echoedMarker) {
					t.Errorf("%s %s: the summary names the subject outside its public parts (%v): %q", tool, a.name, certPublicNames, got)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no summary decodes a certificate: the check sees none")
	}
}

// holdsSecret reports whether text holds the marker of secret, or any
// part of secret in the Base64 a tool sends it in (pemArg): each run of 12
// bytes at an offset a multiple of 3 is 16 letters of Base64 of the whole,
// whatever comes before it.
func holdsSecret(text, secret string) bool {
	if strings.Contains(text, echoedMarker) {
		return true
	}
	for i := 0; i+12 <= len(secret); i += 3 {
		if strings.Contains(text, base64.StdEncoding.EncodeToString([]byte(secret[i:i+12]))) {
			return true
		}
	}
	return false
}

// The preview of a tool with a secret argument (CLISpec.SecretFiles) never
// holds its text, on the whole answer — whatever the other arguments of
// the call are, a valid certificate among them (Астра #2242, S5/S6: the
// check was on the summary alone).
func TestSecretArgs_PreviewHoldsNoSecret(t *testing.T) {
	// The world of faults with nothing made: every read finds nothing, so
	// each create reaches its question.
	r := faultAPIRegistry(t, &faultAPI{cancel: func() {}})
	n := 0
	for _, tl := range r.Tools() {
		for secret := range tl.CLI.SecretFiles {
			for _, other := range []string{"", testCert("example.test")} {
				args := requiredArgs(tl)
				args[secret] = "-----BEGIN PRIVATE KEY-----\n" + echoedMarker + "\n-----END PRIVATE KEY-----\n"
				for _, a := range decls[tl.Name].EchoedArgs {
					if other != "" {
						args[a.name] = other
					}
				}
				n++
				_, text, _ := callNamed(t, r, tl.Name, args)
				if holdsSecret(text, args[secret].(string)) {
					t.Errorf("%s: the answer before the consent holds %s, as it is or in Base64:\n%s", tl.Name, secret, text)
				}
				if !strings.Contains(text, "confirmation_required") {
					t.Errorf("%s: the call reaches no preview, the check sees nothing:\n%s", tl.Name, text)
				}
			}
		}
	}
	if n < 4 {
		t.Fatalf("calls with a secret argument: %d — the check sees too few", n)
	}
}
