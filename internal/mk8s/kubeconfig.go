// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultOIDCTokenURL is the Cloud.ru IAM OIDC token endpoint the mk8s
// `cloudlogin` exec-plugin uses (client-credentials grant).
const DefaultOIDCTokenURL = "https://id.cloud.ru/auth/system/openid/token"

// oidcClient sends the token request; tests replace its transport. No
// redirects: on 307/308 the client would POST the same body, with
// client_secret, to wherever the Location points.
var oidcClient = &http.Client{Timeout: 20 * time.Second, CheckRedirect: noRedirects}

func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// FetchOIDCToken performs the same client-credentials exchange the Cloud.ru
// mk8s `cloudlogin get-token` plugin does, returning the JWT used as the
// kube-apiserver bearer token. Prefers id_token, falls back to access_token
// (they carry identical claims: iss=id.cloud.ru/auth/system, sub=serviceAccount).
func FetchOIDCToken(ctx context.Context, tokenURL, keyID, secret string) (string, error) {
	// The URL comes from the cluster's kubeconfig, i.e. from an API
	// response, and the request carries the service account's secret: send
	// it only to Cloud.ru IAM over https.
	if !trustedOIDCURL(tokenURL) {
		tokenURL = DefaultOIDCTokenURL
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {keyID},
		"client_secret": {secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oidcClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("OIDC token HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}
	var r struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse oidc token: %w", err)
	}
	if r.IDToken != "" {
		return r.IDToken, nil
	}
	if r.AccessToken != "" {
		return r.AccessToken, nil
	}
	return "", fmt.Errorf("OIDC response had no id_token/access_token")
}

// trustedOIDCHosts are the IAM hosts the access key may be sent to (both
// answer the client-credentials grant, checked 2026-10-14).
var trustedOIDCHosts = map[string]bool{"id.cloud.ru": true, "auth.iam.cloud.ru": true}

// trustedOIDCURL reports whether u is https on a Cloud.ru IAM host.
func trustedOIDCURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.User == nil && p.Port() == "" &&
		trustedOIDCHosts[strings.ToLower(p.Hostname())]
}

// OIDCURLFromKubeconfig pulls the --iam-url from the kubeconfig's exec-auth
// block (so we hit the exact endpoint the cluster expects); returns "" if absent.
func OIDCURLFromKubeconfig(raw string) string {
	var kc map[string]any
	if yaml.Unmarshal([]byte(raw), &kc) != nil {
		return ""
	}
	users, _ := kc["users"].([]any)
	for _, u := range users {
		um, _ := u.(map[string]any)
		user, _ := um["user"].(map[string]any)
		exec, _ := user["exec"].(map[string]any)
		args, _ := exec["args"].([]any)
		for _, a := range args {
			if s, _ := a.(string); strings.HasPrefix(s, "--iam-url=") {
				return strings.TrimPrefix(s, "--iam-url=")
			}
		}
	}
	return ""
}

// EmbedToken rewrites the kubeconfig so users[].user carries a static bearer
// token instead of the `cloudlogin` exec plugin — making the kubeconfig work
// with a plain kubectl/helm that doesn't have cloudlogin installed.
func EmbedToken(raw, token string) (string, error) {
	var kc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &kc); err != nil {
		return "", err
	}
	users, ok := kc["users"].([]any)
	if !ok || len(users) == 0 {
		return "", fmt.Errorf("kubeconfig has no users to rewrite")
	}
	for _, u := range users {
		if um, _ := u.(map[string]any); um != nil {
			um["user"] = map[string]any{"token": token}
		}
	}
	out, err := yaml.Marshal(kc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
