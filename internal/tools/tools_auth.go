// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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
	return toJSON(out), false
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
			Detail: i18n.T("no access key: run 'evo configure' or set CLOUDRU_KEY_ID and CLOUDRU_SECRET",
				"нет ключа доступа: выполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET")})
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
		checks = append(checks, keyScopeCheck(ctx, eng.Auth))
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
	if cfg.ProjectID == "" {
		ce := clierrors.ProjectRequired()
		checks = append(checks, DoctorCheck{Name: "project_id", Status: CheckFail, Auth: true,
			Detail: i18n.T("not set: ", "не задан: ") + ce.Hint})
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
	start := time.Now()
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
	took := latencyText(time.Since(start))
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
			i18n.T("Fix the configuration or credentials above, e.g. with 'evo configure'",
				"Исправьте настройки или ключ (см. выше), например через 'evo configure'"))
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
		return i18n.T("unknown (the Organization API does not list projects for this key)",
			"неизвестно (Organization API не отдаёт список проектов для этого ключа)")
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
