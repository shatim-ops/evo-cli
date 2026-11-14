// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// fallbackTokenTTL is the lifetime assumed when the IAM answer names none;
// tokens live about an hour.
const fallbackTokenTTL = 50 * time.Minute

// IAMEndpoint is the Cloud.ru IAM token exchange URL.
var IAMEndpoint = "https://iam.api.cloud.ru/api/v1/auth/token"

// refreshTTL is the minimum remaining TTL before a cached token is refreshed.
const refreshTTL = 5 * time.Minute

// minAcceptableTTL is the pre-flight check threshold (QCSD #1).
const minAcceptableTTL = 10 * time.Minute

// Credentials is a service account access key: key ID and secret.
type Credentials struct {
	KeyID  string
	Secret string
	// ExpiresAt is the key's term when evo login made it (the profile);
	// zero when unknown. Only the text of a refusal reads it (R-26).
	ExpiresAt time.Time
}

// IAMProvider exchanges an access key for IAM tokens. The credentials come
// from the resolved configuration (profile, env or flags), not from the
// environment directly. Tokens are cached in memory and, if Cache is set,
// on disk, so separate runs of the CLI do not each go to IAM.
type IAMProvider struct {
	creds  Credentials
	client *http.Client
	cache  TokenCache

	// sleep waits between retries; tests replace it to avoid real pauses.
	sleep func(ctx context.Context, d time.Duration) error

	mu        sync.Mutex
	cached    *Token
	exchanges int
	// issued are the tokens this provider handed out: a request a service
	// refused with one of them goes again with the provider's current
	// token (renewRefused).
	issued map[string]bool
}

// Retries of the token exchange. Corporate proxies and flaky links reset
// connections to IAM now and then; one reset must not fail the command.
const (
	exchangeAttempts  = 3
	exchangeBaseDelay = 500 * time.Millisecond // 0.5 s, then 1 s
)

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NewIAMProvider creates a provider for creds. If client is nil, a client
// with a 60s timeout is used: http.DefaultClient has no timeout and would
// hang forever on a stalled IAM connection. cache may be nil.
func NewIAMProvider(creds Credentials, client *http.Client, cache TokenCache) *IAMProvider {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second, Transport: httpretry.UserAgent(nil)}
	}
	p := &IAMProvider{creds: creds, client: client, cache: cache, sleep: sleepCtx}
	register(p)
	return p
}

// live holds every provider of the process, weakly: a token a Cloud.ru
// service answered 401 to is dropped by the provider that holds it,
// wherever that provider was made (tokenProvider of the CLI and evo mcp,
// configure, a test world), and a provider nobody uses any more is not
// kept alive by the list.
var (
	liveMu sync.Mutex
	live   []weak.Pointer[IAMProvider]
)

func init() {
	httpretry.OnTokenRejected(forgetRejected)
	httpretry.OnTokenRenewed(renewRefused)
}

func register(p *IAMProvider) {
	liveMu.Lock()
	defer liveMu.Unlock()
	live = append(alive(), weak.Make(p))
}

// alive drops the providers already collected from live; liveMu is held.
func alive() []weak.Pointer[IAMProvider] {
	kept := live[:0]
	for _, w := range live {
		if w.Value() != nil {
			kept = append(kept, w)
		}
	}
	return kept
}

// forgetRejected is told by httpretry the bearer token of a request a
// service answered 401 to (G140-3): every live provider forgets it.
func forgetRejected(token string) {
	liveMu.Lock()
	live = alive()
	list := make([]*IAMProvider, 0, len(live))
	for _, w := range live {
		if p := w.Value(); p != nil {
			list = append(list, p)
		}
	}
	liveMu.Unlock()
	for _, p := range list {
		p.Forget(token)
	}
}

// renewRefused is asked by httpretry for the token that replaces token, a
// refused one: the provider that issued it gives its current token, a new
// one if token was its current (Forget has dropped it).
func renewRefused(ctx context.Context, token string) (string, bool) {
	liveMu.Lock()
	live = alive()
	list := make([]*IAMProvider, 0, len(live))
	for _, w := range live {
		if p := w.Value(); p != nil {
			list = append(list, p)
		}
	}
	liveMu.Unlock()
	for _, p := range list {
		p.mu.Lock()
		mine := p.issued[token]
		p.mu.Unlock()
		if !mine {
			continue
		}
		tok, err := p.Token(ctx)
		if err != nil || tok.Value == token {
			return "", false
		}
		return tok.Value, true
	}
	return "", false
}

// KeyID returns the key ID the provider authenticates with.
func (p *IAMProvider) KeyID() string { return p.creds.KeyID }

// Exchanges returns how many times the provider went to IAM.
func (p *IAMProvider) Exchanges() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exchanges
}

// NoKeyHint is the one hint of «no key» (the texts brief, item 7): the
// error of a command without a key, doctor's check of the key and the
// generic hint of a refused key of a tool.
func NoKeyHint() string {
	if halfEnvKey() != "" {
		return i18n.T("Set both variables, or unset the one to use the profile; no profile with a key — evo login.",
			"Задайте обе переменные или уберите заданную, чтобы читался профиль; профиля с ключом нет — evo login.")
	}
	return i18n.T("Run evo login (a sign-in in the browser, a person is needed) or evo configure, or set CLOUDRU_KEY_ID and CLOUDRU_SECRET: the variables come before the profile.",
		"Выполните evo login (вход через браузер, нужен человек) или evo configure либо задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET: переменные важнее профиля.")
}

// RefusedKeyHint is the hint of a key that is set but refused or expired
// (AUTH_FAILED, AUTH_EXPIRED) when the refusal brings none of its own: the
// same key will not do, unlike NoKeyHint's «no key» («Риск», #1029).
func RefusedKeyHint() string {
	return i18n.T("Retrying with the same key will not help. For a new key: evo login (a sign-in in the browser, a person is needed) or evo configure.",
		"Повтор тем же ключом не поможет. Новый ключ — evo login (вход через браузер, нужен человек) или evo configure.")
}

// forbiddenKeyHint is added to the hint of a key IAM answered 403: a
// refusal of access, not a wrong key — a person checks the account. What
// the 403 means at the exchange is a hypothesis until a probe: the runs saw
// only 401 («Риск», conclusion 68).
func forbiddenKeyHint() string {
	return i18n.T("IAM answered 403, a refusal of access, not a wrong key: if the key should work, ask the administrator of the project to check the service account and its keys (Service accounts in the Cloud.ru console).",
		"IAM ответил 403 — это отказ в доступе, а не неверный ключ: если ключ должен работать, попросите администратора проекта проверить сервисный аккаунт и его ключи («Сервисные аккаунты» в личном кабинете Cloud.ru).")
}

// rejectedKeyHint is the hint of a key IAM rejected: a new key first, then
// where the key came from — the variables when set, else the profile.
func rejectedKeyHint() string {
	return i18n.T("Retrying with the same key will not help. For a new key: evo login (a sign-in in the browser, a person is needed) or evo configure. The key comes from CLOUDRU_KEY_ID / CLOUDRU_SECRET when they are set, else from the profile: if it should work, check the key ID and secret there — the key may have been deleted or expired, or the service account disabled.",
		"Повтор тем же ключом не поможет. Новый ключ — evo login (вход через браузер, нужен человек) или evo configure. Ключ берётся из CLOUDRU_KEY_ID / CLOUDRU_SECRET, если они заданы, иначе из профиля: если он должен работать, проверьте там Key ID и секрет — ключ мог быть удалён или истечь, а сервисный аккаунт выключен.")
}

// ErrNoCredentials is the error of a command that needs the Cloud.ru key
// and finds none: no profile from 'evo configure' and nothing in the
// environment (exit code 2).
func ErrNoCredentials() error {
	msg := i18n.T("Cloud.ru credentials not set", "не задан ключ доступа Cloud.ru")
	if half := halfEnvKey(); half != "" {
		msg = half
	}
	return clerrors.New(clerrors.CodeAuthRequired, msg, NoKeyHint())
}

// halfEnvKey names a half of the key pair in the environment: the key is
// taken from the environment only as a pair, else from the profile, and
// one variable alone read as «no key» (findings ledger 1.8, № 130).
func halfEnvKey() string {
	set, missing := config.HalfEnvKey()
	if set == "" {
		return ""
	}
	return i18n.Tf("%s is set without %s: the environment gives the key only as a pair, and then the profile is not read",
		"%s задан без %s: окружение дает ключ только парой, и тогда профиль не читается", set, missing)
}

// Token returns a valid IAM token. Refreshes if cached token has < 5min TTL.
// Pre-flight check: rejects if resulting token TTL < 10min (QCSD #1).
func (p *IAMProvider) Token(ctx context.Context) (Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.creds.KeyID == "" || p.creds.Secret == "" {
		return Token{}, ErrNoCredentials()
	}

	if p.cached != nil && p.cached.IsValid(refreshTTL) {
		return *p.cached, nil
	}
	if p.cache != nil {
		if tok, ok := p.cache.Load(CacheKey(p.creds)); ok && tok.IsValid(refreshTTL) {
			p.keep(tok)
			return tok, nil
		}
	}

	tok, err := p.exchange(ctx)
	if err != nil {
		return Token{}, err
	}

	// Pre-flight: reject tokens with very short TTL. A deadline, so on the
	// wall clock (IsValid); TTL only prints what is left (FB140-5).
	if !tok.IsValid(minAcceptableTTL) {
		return Token{}, clerrors.New(
			clerrors.CodeAuthExpired,
			i18n.Tf("token TTL too short: %s remaining", "срок жизни токена слишком мал: осталось %s", tok.TTL().Truncate(time.Second)),
			i18n.T("Your credentials may be expiring soon. Rotate them at https://console.cloud.ru",
				"Возможно, срок действия ключа заканчивается. Выпустите новый в https://console.cloud.ru"),
		)
	}

	p.keep(tok)
	if p.cache != nil {
		// The cache only saves IAM round trips; failing to write it must
		// not fail the command.
		_ = p.cache.Store(CacheKey(p.creds), tok)
	}
	return tok, nil
}

// keep makes tok the provider's current token and one it issued; p.mu is
// held.
func (p *IAMProvider) keep(tok Token) {
	p.cached = &tok
	// A few at most: one per hour of a long session, one per sleep.
	if p.issued == nil || len(p.issued) >= 16 {
		p.issued = map[string]bool{}
	}
	p.issued[tok.Value] = true
}

// exchange obtains a token, retrying transport errors and 5xx responses
// with exponential backoff. 4xx responses are final.
func (p *IAMProvider) exchange(ctx context.Context) (Token, error) {
	delay := exchangeBaseDelay
	// One id for every attempt: support sees the exchange as one request.
	id := httpretry.NewRequestID()
	for attempt := 1; ; attempt++ {
		tok, retryable, err := p.exchangeOnce(ctx, id)
		if err == nil || !retryable || attempt == exchangeAttempts || ctx.Err() != nil {
			return tok, err
		}
		if serr := p.sleep(ctx, delay); serr != nil {
			return Token{}, err
		}
		delay *= 2
	}
}

// exchangeOnce performs one HTTP POST to the IAM endpoint, named by the
// X-Request-Id id, and reports whether a failure is worth retrying.
func (p *IAMProvider) exchangeOnce(ctx context.Context, id string) (tok Token, retryable bool, err error) {
	p.exchanges++
	payload, err := json.Marshal(map[string]string{"keyId": p.creds.KeyID, "secret": p.creds.Secret})
	if err != nil {
		return Token{}, false, err
	}
	// A token changes nothing in the cloud: not a write of the call.
	req, err := http.NewRequestWithContext(httpretry.NotAChange(ctx), http.MethodPost, IAMEndpoint, bytes.NewReader(payload))
	if err != nil {
		return Token{}, false, clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			i18n.T("failed to create IAM token request", "не удалось сформировать запрос токена IAM"),
			i18n.T("Check your network connection", "Проверьте подключение к сети"),
			err,
		)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpretry.RequestIDHeader, id)

	// No redirects: on 307/308 the client would POST the same body, with
	// the key secret, to wherever Location points. IAM answers directly;
	// a redirect is treated as an unexpected status below.
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		// The hint names the cause a person can fix: a certificate the
		// system does not trust (a corporate proxy), the proxy itself, DNS.
		// A certificate check that failed is not repeated.
		return Token{}, !clerrors.TLSUntrusted(err), clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			i18n.T("failed to reach IAM endpoint", "IAM недоступен"),
			clerrors.NetworkHint(err, i18n.T("Check your network connection and try again", "Проверьте подключение к сети и повторите")),
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// A key past its term: a person issues the next one, and a retry
		// would only refuse again (R-26). The reason stays that of any
		// refused key.
		if exp := p.creds.ExpiresAt; !exp.IsZero() && !clock.Now().Before(exp) {
			ce := clerrors.New(clerrors.CodeAuthFailed,
				i18n.Tf("the term of the access key expired %s (IAM status %d)", "срок ключа доступа истек %s (статус IAM %d)",
					exp.UTC().Format(time.RFC3339), resp.StatusCode),
				i18n.T("Retrying will not help: a person is needed. Run evo login (a sign-in in the browser) to issue a new key, or create a key in the Cloud.ru console and run evo configure.",
					"Повтор не поможет: нужен человек. Выполните evo login (вход через браузер), чтобы выпустить новый ключ, или создайте ключ в личном кабинете Cloud.ru и выполните evo configure."))
			ce.Details = map[string]any{"key_expired": true, "http_status": resp.StatusCode}
			return Token{}, false, ce
		}
		// 401 and 403 are one reason — a person is needed either way — and
		// http_status with the hint tell the agent which way: the key itself,
		// or the key IAM knows but does not let in («Риск», conclusion 36
		// п. 3).
		hint := rejectedKeyHint()
		if resp.StatusCode == http.StatusForbidden {
			hint += " " + forbiddenKeyHint()
		}
		ce := clerrors.New(
			clerrors.CodeAuthFailed,
			i18n.Tf("IAM rejected the credentials (status %d)", "IAM отклонил ключ доступа (статус %d)", resp.StatusCode),
			hint,
		)
		ce.Details = map[string]any{"http_status": resp.StatusCode}
		return Token{}, false, ce
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, resp.StatusCode >= 500, clerrors.New(
			clerrors.CodeAPIError,
			i18n.Tf("IAM token exchange failed with status %d", "не удалось получить токен IAM: статус %d", resp.StatusCode),
			i18n.T("Retry later; if it persists, verify the key ID and secret",
				"Повторите позже; если ошибка не уходит, проверьте Key ID и секрет"),
		)
	}

	// The spec (iam-gw-public) names the fields accessToken/expiresIn; the
	// live answer has used access_token/expires_in. Both are read
	// (api-contract §4.12).
	var result struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int    `json:"expires_in"` // seconds
		AccessTokenCamel string `json:"accessToken"`
		ExpiresInCamel   int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Usually a connection reset mid-body: worth another attempt.
		return Token{}, true, clerrors.Wrap(
			clerrors.CodeAPIError,
			i18n.T("failed to parse IAM token response", "не удалось разобрать ответ IAM с токеном"),
			i18n.T("Unexpected response from IAM API", "Неожиданный ответ IAM API; повторите позже"),
			err,
		)
	}
	if result.AccessToken == "" {
		result.AccessToken = result.AccessTokenCamel
	}
	if result.ExpiresIn == 0 {
		result.ExpiresIn = result.ExpiresInCamel
	}
	if result.ExpiresIn <= 0 {
		// No lifetime in the answer: assume a conservative one instead of
		// "expired now", which would exchange the key before every call.
		result.ExpiresIn = int(fallbackTokenTTL / time.Second)
	}
	if result.AccessToken == "" {
		return Token{}, false, clerrors.New(
			clerrors.CodeAPIError,
			i18n.T("IAM returned empty access_token", "IAM вернул пустой access_token"),
			i18n.T("Verify your credentials are correct", "Проверьте ключ доступа"),
		)
	}

	expires := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	if exp, ok := jwtExpiry(result.AccessToken); ok && exp.Before(expires) {
		expires = exp
	}
	// Round(0) drops the monotonic reading: the deadline is the wall
	// clock's. The monotonic clock stands still while the host sleeps or a
	// VM is paused, and a long-lived MCP server kept an expired token as
	// valid after the pause (G140-3).
	return Token{Value: result.AccessToken, ExpiresAt: expires.Round(0)}, false, nil
}

// jwtExpiry is the exp claim of a JWT access token, read without checking
// the signature: it only bounds how long evo keeps the token, never
// whether it is trusted. ok is false for a token that is not a JWT or has
// no exp.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// Forget drops value, a token a Cloud.ru service answered 401 to, from the
// memory and the file cache, so the next Token exchanges the key again
// instead of sending the same token until its deadline. A token the
// provider no longer holds (it has been refreshed since) is left alone.
func (p *IAMProvider) Forget(value string) {
	if value == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil && p.cached.Value == value {
		p.cached = nil
	}
	if p.cache == nil {
		return
	}
	key := CacheKey(p.creds)
	if tok, ok := p.cache.Load(key); ok && tok.Value == value {
		_ = p.cache.Delete(key)
	}
}
