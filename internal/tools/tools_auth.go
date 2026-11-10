// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
)

// whoami and doctor say who the tools work as and whether the setup works
// (spec REQ-11, owner's decision Q2). 'evo whoami' and 'evo doctor' are
// their commands; the checks of the machine the CLI runs on (its config
// files, their modes, kubectl and helm in PATH) are the CLI's part of
// doctor's answer (Caller.Doctor): over MCP they would be the server's.

// ComputeBaseURL is the Compute API doctor probes (a variable for the
// tests of the CLI).
var ComputeBaseURL = "https://compute.api.cloud.ru"

// doctorProbeTimeout bounds each reachability probe.
const doctorProbeTimeout = 10 * time.Second

// probeClient sends doctor's probes once: a retry would only make an
// unreachable host slower to report.
var probeClient = &http.Client{Transport: httpretry.UserAgent(nil)}

// Statuses of a doctor check.
const (
	CheckOK   = "ok"
	CheckWarn = "warn"
	CheckFail = "fail"
	CheckSkip = "skip"
)

// DoctorCheck is one check of doctor.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// HTTPStatus is what a reachability probe got back. Not in the detail
	// (only with Caller.Verbose): the API root answers 404, which reads as
	// an error to a person, while it proves the host is reachable.
	HTTPStatus int `json:"http_status,omitempty"`
	// Auth marks a failure of the configuration or the credentials:
	// doctor then fails with CONFIG_INVALID (exit code 2).
	Auth bool `json:"-"`
}

func (s *Registry) handleWhoami(ctx context.Context, _ map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return failed(ctx, err, nil), true
	}
	cfg := eng.Config
	before := exchanges(eng.Auth)
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return failed(ctx, err, nil), true
	}
	source := "cache"
	if exchanges(eng.Auth) != before {
		source = "iam"
	}
	out := map[string]any{
		"profile":           cfg.Profile,
		"key_id":            config.MaskKeyID(cfg.KeyID),
		"credentials_from":  cfg.KeyOrigin,
		"project_id":        cfg.ProjectID,
		"project_from":      projectOrigin(cfg),
		"token_expires_at":  tok.ExpiresAt.UTC().Format(time.RFC3339),
		"token_ttl_seconds": int(tok.TTL().Seconds()),
		"token_source":      source,
	}
	// The number is unknown when the Organization API does not list
	// projects for the key: then there is no field (ScopeText).
	if n, err := visibleProjects(ctx, eng.Auth); err == nil {
		out["visible_projects"] = n
	}
	// The roles of the key are a field more: when IAM does not tell them,
	// the answer is the one of before and roles_note says why («Риск»,
	// conclusion 22, У-6).
	roles, note := keyRoles(ctx, eng.Auth, tok, cfg.ProjectID)
	if roles != nil {
		out["roles"] = roles
	}
	if note != "" {
		out["roles_note"] = note
	}
	// The term of the key, read here and in doctor only (R-26): a person
	// issues the next key, and the agent learns when it has to ask one.
	term, termNote := keyTerm(ctx, eng.Auth, tok, cfg.KeyID)
	if !term.IsZero() {
		out["key_expires_at"] = term.UTC().Format(time.RFC3339)
		if keyExpiresSoon(term) {
			out["key_expires_soon"] = true
		}
	} else {
		out["key_term_note"] = termNote
	}
	return toJSON(out), false
}

// keyTermWarning is how long before its term a key is said to expire.
const keyTermWarning = 5 * 24 * time.Hour

// keyExpiresSoon reports whether the key's term is within keyTermWarning
// of now (the answer's value: clock).
func keyExpiresSoon(term time.Time) bool { return term.Sub(clock.Now()) <= keyTermWarning }

// keyTerm is the term of the key keyID: the expiredAt of the access key of
// the key's service account — the token's sub — with that keyId (A0-3).
// It is zero when unknown, and note then says why: IAM did not tell (no
// right to read the keys), the key is not in the list, it has no term.
func keyTerm(ctx context.Context, tp auth.TokenProvider, tok auth.Token, keyID string) (term time.Time, note string) {
	sub := jwtSub(tok.Value)
	if sub == "" {
		return time.Time{}, i18n.T("unknown: the IAM token names no service account", "неизвестен: токен IAM не называет сервисный аккаунт")
	}
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	keys, err := iamadmin.New(tp, "").AccessKeys(ctx, sub)
	if err != nil {
		return time.Time{}, i18n.Tf("unknown: IAM did not tell it (%s)", "неизвестен: IAM его не сообщил (%s)", CheckDetail(err))
	}
	for _, k := range keys {
		if k.KeyID != keyID {
			continue
		}
		t, err := time.Parse(time.RFC3339, k.ExpiredAt)
		if k.ExpiredAt == "" || err != nil {
			return time.Time{}, i18n.T("unknown: IAM names no term of the key (no term)", "неизвестен: IAM не называет срок ключа (no term)")
		}
		return t, ""
	}
	return time.Time{}, i18n.T("unknown: the key is not in the list of its service account's keys (not in the list)",
		"неизвестен: ключа нет в списке ключей его сервисного аккаунта (not in the list)")
}

// keyTermCheck is doctor's check of the key's term: a warning within
// keyTermWarning, with the way — a person issues a new key — and
// «unknown» (skip) when IAM does not tell it; never a failure.
func keyTermCheck(ctx context.Context, tp auth.TokenProvider, tok auth.Token, keyID string) DoctorCheck {
	term, note := keyTerm(ctx, tp, tok, keyID)
	switch {
	case term.IsZero():
		return DoctorCheck{Name: "key term", Status: CheckSkip, Detail: note}
	case keyExpiresSoon(term):
		return DoctorCheck{Name: "key term", Status: CheckWarn,
			Detail: i18n.Tf("the key expires %s: a person issues a new one before that — 'evo login' or a new key in the console (Users → Service accounts)",
				"ключ истекает %s: новый выпускает человек до этого срока — 'evo login' или новый ключ в консоли («Пользователи» → «Сервисные аккаунты»)",
				term.UTC().Format(time.RFC3339))}
	}
	return DoctorCheck{Name: "key term", Status: CheckOK,
		Detail: i18n.Tf("the key is valid until %s", "ключ действует до %s", term.UTC().Format(time.RFC3339))}
}

// keyRole is a role of the key on the project as whoami names it.
type keyRole struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// keyRoles are the roles the key's service account — the token's sub —
// has on the project, by name: its enabled permissions there (A0-2, no
// map of tools to actions). roles is nil when they are unknown, and note
// then says why; an empty list has a note too: a role on the organization
// is a permission of another resource, not read here.
func keyRoles(ctx context.Context, tp auth.TokenProvider, tok auth.Token, projectID string) (roles []keyRole, note string) {
	if projectID == "" {
		return nil, i18n.T("unknown: no project is set, and roles are given per project",
			"неизвестны: проект не задан, а роли выдаются на проект")
	}
	sub := jwtSub(tok.Value)
	if sub == "" {
		return nil, i18n.T("unknown: the IAM token names no service account", "неизвестны: токен IAM не называет сервисный аккаунт")
	}
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	perms, err := iamadmin.New(tp, "").Permissions(ctx, projectID, sub)
	if err != nil {
		return nil, i18n.Tf("unknown: IAM did not tell them (%s)", "неизвестны: IAM их не сообщил (%s)", CheckDetail(err))
	}
	roles = []keyRole{}
	for _, p := range perms {
		if p.Enabled && p.RoleName != "" {
			roles = append(roles, keyRole{Name: p.RoleName, ExpiresAt: p.ExpiresAt})
		}
	}
	if len(roles) == 0 {
		note = i18n.T("no role of the key on the project; a role on the organization is not seen here",
			"у ключа нет ролей на проекте; роль на организацию здесь не видна")
	}
	return roles, note
}

// keyRolesCheck is doctor's check of the key's roles on the project: a
// warning without one, «unknown» (skip) when IAM does not tell — never a
// failure: the key may work all the same («Риск», conclusion 22, У-7).
func keyRolesCheck(ctx context.Context, tp auth.TokenProvider, tok auth.Token, projectID string) DoctorCheck {
	roles, note := keyRoles(ctx, tp, tok, projectID)
	switch {
	case roles == nil:
		return DoctorCheck{Name: "key roles", Status: CheckSkip, Detail: note}
	case len(roles) == 0:
		return DoctorCheck{Name: "key roles", Status: CheckWarn, Detail: note}
	}
	names := make([]string, len(roles))
	for i, r := range roles {
		// The cloud's name, quoted on one line in evo's own text.
		names[i] = quoteCloud(r.Name)
		if r.ExpiresAt != "" {
			names[i] += i18n.Tf(" until %s", " до %s", r.ExpiresAt)
		}
	}
	return DoctorCheck{Name: "key roles", Status: CheckOK,
		Detail: i18n.Tf("on project %s: %s", "на проекте %s: %s", projectID, strings.Join(names, ", "))}
}

func (s *Registry) handleDoctor(ctx context.Context, _ map[string]any) (string, bool) {
	c := callerOf(ctx)
	checks := s.doctorChecks(ctx, c != nil && c.Verbose)
	if c != nil && c.Doctor != nil {
		checks = c.Doctor(checks)
	}
	err := doctorVerdict(checks)
	answer := map[string]any{"checks": checks, "ok": err == nil}
	if err != nil {
		return failed(ctx, err, answer), true
	}
	return toJSON(answer), false
}

// doctorChecks are doctor's own checks: the configuration the next call
// uses, the reachability of IAM and Compute, the key and what it sees.
func (s *Registry) doctorChecks(ctx context.Context, verbose bool) []DoctorCheck {
	// The configuration's warnings are checks here: the factory does not
	// print them as well.
	eng, err := s.newEngine(map[string]any{WarningsShown: true})
	var checks []DoctorCheck
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "config", Status: CheckFail, Detail: CheckDetail(err), Auth: true})
	} else {
		checks = append(checks, configChecks(eng.Config)...)
	}
	checks = append(checks,
		probe(ctx, "IAM API", auth.IAMEndpoint, verbose),
		probe(ctx, "Compute API", ComputeBaseURL, verbose),
	)

	switch {
	case err != nil:
		checks = append(checks, DoctorCheck{Name: "credentials", Status: CheckSkip,
			Detail: i18n.T("configuration is broken", "настройки сломаны")})
	case eng.Config.KeyID == "" || eng.Config.Secret == "":
		checks = append(checks, DoctorCheck{Name: "credentials", Status: CheckFail, Auth: true,
			Detail: i18n.T("no access key. ", "нет ключа доступа. ") + auth.NoKeyHint()})
	default:
		before := exchanges(eng.Auth)
		tok, terr := eng.Auth.Token(ctx)
		if terr != nil {
			checks = append(checks, DoctorCheck{Name: "credentials", Status: CheckFail, Detail: CheckDetail(terr),
				Auth: clierrors.ExitCode(terr) == clierrors.ExitAuth})
			break
		}
		src := i18n.T("cached token", "токен из кэша")
		if exchanges(eng.Auth) != before {
			src = i18n.T("token issued", "токен выдан")
		}
		checks = append(checks, DoctorCheck{Name: "credentials", Status: CheckOK,
			Detail: i18n.Tf("key %s: %s, valid for %s", "ключ %s: %s, действует ещё %s",
				config.MaskKeyID(eng.Config.KeyID), src, tok.TTL().Truncate(time.Minute))})
		checks = append(checks, keyScopeCheck(ctx, eng.Auth), keyRolesCheck(ctx, eng.Auth, tok, eng.Config.ProjectID),
			keyTermCheck(ctx, eng.Auth, tok, eng.Config.KeyID))
	}
	return checks
}

// configChecks show where the key settings come from.
func configChecks(cfg config.Config) []DoctorCheck {
	// "project_id from default" read as if a default project existed: an
	// unset project says so (release audit, R-06).
	project := i18n.Tf("project_id from %s", "project_id из %s", projectOrigin(cfg))
	if cfg.ProjectID == "" {
		project = i18n.T("project_id not set", "project_id не задан")
	}
	checks := []DoctorCheck{{
		Name: "config", Status: CheckOK,
		Detail: i18n.Tf("profile %s; key_id from %s; %s", "профиль %s; key_id из %s; %s",
			cfg.Profile, cfg.KeyOrigin, project),
	}}
	// Without a key "ok config … key_id from default" above a failed
	// credentials check read as a contradiction: say the key is missing.
	if cfg.KeyID == "" || cfg.Secret == "" {
		checks[0].Status = CheckWarn
		checks[0].Detail = i18n.Tf("profile %s; access key not set; %s",
			"профиль %s; ключ доступа не задан; %s", cfg.Profile, project)
	}
	// Without a project every project command (vm list, vpc subnets, …)
	// fails with CONFIG_INVALID, exit 2: doctor fails the same way, not
	// "ok" with exit 0 (release audit, R-06).
	// Without a key the key is the first thing to fix: the project is a
	// warning then, and evo login sets both (plan 1.8, #1087, audit of
	// 06.11; findings ledger 1.8, № 86).
	if cfg.ProjectID == "" {
		ce := clierrors.ProjectRequired()
		st, hint := CheckFail, ce.Hint
		if cfg.KeyID == "" || cfg.Secret == "" {
			st, hint = CheckWarn, i18n.T("evo login sets it together with the key", "evo login задаст его вместе с ключом")
		}
		checks = append(checks, DoctorCheck{Name: "project_id", Status: st, Auth: true,
			Detail: i18n.T("not set: ", "не задан: ") + hint})
	}
	for _, w := range cfg.Warnings {
		checks = append(checks, DoctorCheck{Name: "config", Status: CheckWarn, Detail: w})
	}
	return checks
}

// projectOrigin is where the project came from, OriginDefault when it was
// not set.
func projectOrigin(cfg config.Config) string {
	if cfg.ProjectOrigin == "" {
		return config.OriginDefault
	}
	return cfg.ProjectOrigin
}

// probe checks that an API host answers HTTP at all; the status code does
// not matter, only that TLS and the connection work.
func probe(ctx context.Context, name, rawURL string, verbose bool) DoctorCheck {
	u, err := url.Parse(rawURL)
	if err != nil {
		return DoctorCheck{Name: name, Status: CheckFail, Detail: err.Error()}
	}
	base := u.Scheme + "://" + u.Host + "/"
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return DoctorCheck{Name: name, Status: CheckFail, Detail: err.Error()}
	}
	start := clock.Now()
	resp, err := probeClient.Do(req)
	if err != nil {
		detail := i18n.Tf("%s unreachable: %v", "%s недоступен: %v", u.Host, err)
		if hint := clierrors.NetworkHint(err, ""); hint != "" {
			// The hint names the cause itself (the certificate, the proxy).
			detail = i18n.Tf("%s unreachable: %s", "%s недоступен: %s", u.Host, hint)
		}
		return DoctorCheck{Name: name, Status: CheckFail, Detail: detail}
	}
	resp.Body.Close()
	took := latencyText(clock.Since(start))
	detail := i18n.Tf("%s reachable (%s)", "%s доступен (%s)", u.Host, took)
	if verbose {
		detail = i18n.Tf("%s reachable (HTTP %d, %s)", "%s доступен (HTTP %d, %s)", u.Host, resp.StatusCode, took)
	}
	return DoctorCheck{Name: name, Status: CheckOK, Detail: detail, HTTPStatus: resp.StatusCode}
}

// latencyText is a probe's time: "66ms", in Russian «66 мс».
func latencyText(d time.Duration) string {
	if d < time.Second {
		return i18n.Tf("%dms", "%d мс", d.Milliseconds())
	}
	return i18n.Tf("%.1fs", "%.1f с", d.Seconds())
}

// keyScopeCheck warns about a key that sees several projects: the project
// a call works in is then a setting, not a limit of the key.
func keyScopeCheck(ctx context.Context, tp auth.TokenProvider) DoctorCheck {
	n, err := visibleProjects(ctx, tp)
	switch {
	case err != nil:
		return DoctorCheck{Name: "key scope", Status: CheckSkip, Detail: ScopeText(0, false)}
	case n > 1:
		return DoctorCheck{Name: "key scope", Status: CheckWarn, Detail: ManyProjectsWarning(n)}
	default:
		return DoctorCheck{Name: "key scope", Status: CheckOK, Detail: ScopeText(n, true)}
	}
}

// doctorVerdict is the error of a doctor whose checks failed: CONFIG_INVALID
// (exit code 2) when the configuration or the key is broken, else
// API_UNREACHABLE (exit code 1): only connectivity failed.
func doctorVerdict(checks []DoctorCheck) error {
	failedN, authFailed := 0, false
	for _, c := range checks {
		if c.Status == CheckFail {
			failedN++
			authFailed = authFailed || c.Auth
		}
	}
	msg := i18n.Tf("doctor: %d check(s) failed", "doctor: не прошли проверки: %d", failedN)
	switch {
	case failedN == 0:
		return nil
	case authFailed:
		return clierrors.New(clierrors.CodeConfigInvalid, msg,
			i18n.T("Fix the configuration or credentials above: evo login (through the browser) or evo configure",
				"Исправьте настройки или ключ (см. выше): evo login (через браузер) или evo configure"))
	default:
		return clierrors.New(clierrors.CodeAPIUnreachable, msg, "")
	}
}

// CheckDetail is err as the detail of a failed check: the message and the
// hint.
func CheckDetail(err error) string {
	if cliErr, ok := err.(*clierrors.CLIError); ok && cliErr.Hint != "" {
		return cliErr.Message + " — " + cliErr.Hint
	}
	return err.Error()
}

// exchanges is how many times tp went to IAM: auth.IAMProvider counts
// them, a provider that does not count never goes.
func exchanges(tp auth.TokenProvider) int {
	if c, ok := tp.(interface{ Exchanges() int }); ok {
		return c.Exchanges()
	}
	return 0
}

// visibleProjects is how many projects the key of tp sees, an error when
// the Organization API does not list them for it.
func visibleProjects(ctx context.Context, tp auth.TokenProvider) (int, error) {
	raw, _, err := orgProjectPages(ctx, tp)
	return len(raw), err
}

// ScopeText says how many projects the key sees: n, or, when the
// Organization API does not list them for the key (known false), that the
// number is unknown. That is no error: the API lists no projects for a
// service account (400 "user not found", 14.10.2026), which works in its
// project.
func ScopeText(n int, known bool) string {
	switch {
	case !known:
		// No name of an API: «the Organization API does not list» read as
		// a failure (release audit 1.8, L7).
		return i18n.T("unknown: Cloud.ru does not give a service account key the list of projects — not an error, the key works in its project",
			"неизвестно: ключу сервисного аккаунта Cloud.ru не отдаёт список проектов — это не ошибка, ключ работает в своём проекте")
	case n == 1:
		return i18n.T("1 visible to the key", "ключ видит 1 проект")
	}
	return i18n.Tf("%d visible to the key", "ключ видит %d "+i18n.Plural(n, "проект", "проекта", "проектов"), n)
}

// ManyProjectsWarning explains what a key that sees several projects means:
// every command works in the configured project, and an MCP server with
// this key can be told to work in any of them (QA P5).
func ManyProjectsWarning(n int) string {
	return i18n.Tf("the key sees %d projects (organization-level access): commands work in the project above "+
		"unless --project-id says otherwise; limit a shared 'evo mcp --http' server with --allow-project",
		"ключ видит %d "+i18n.Plural(n, "проект", "проекта", "проектов")+" (доступ уровня организации): команды работают "+
			"в проекте выше, если --project-id не задаёт другой; общий сервер 'evo mcp --http' ограничьте флагом --allow-project", n)
}
