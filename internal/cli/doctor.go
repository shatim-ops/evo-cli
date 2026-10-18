package cli

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// computeBaseURL is probed by doctor; replaceable in tests.
var computeBaseURL = "https://compute.api.cloud.ru"

// doctorProbeTimeout bounds each reachability probe.
const doctorProbeTimeout = 10 * time.Second

// Check statuses.
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
	checkSkip = "skip"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// HTTPStatus is what a reachability probe got back. Not in the text
	// (only with --verbose): the API root answers 404, which reads as an
	// error to a person, while it proves the host is reachable.
	HTTPStatus int `json:"http_status,omitempty"`
	// auth marks a failure of configuration or credentials (exit code 2).
	auth bool
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: i18n.T("Check configuration, credentials and connectivity", "Проверить настройки, ключ доступа и связь с API"),
		Long: i18n.T(`Check the setup: config files and their permissions, where each setting
comes from, reachability of the IAM and Compute APIs, the credentials, and
kubectl/helm in PATH (needed only by the k8s tools; a warning, not an error).

Exits with 2 if the configuration or credentials are broken, 1 if only
connectivity checks failed.`, `Проверить окружение: файлы настроек и их права, откуда взята каждая
настройка, доступность IAM и Compute API, ключ доступа, kubectl/helm в PATH
(нужны только инструментам k8s; их отсутствие — предупреждение, не ошибка).

Код выхода 2 — сломаны настройки или ключ, 1 — не прошли только проверки
связи.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			checks := runDoctor(cmd.Context())
			return reportDoctor(printer, checks)
		},
	}
}

func runDoctor(ctx context.Context) []doctorCheck {
	checks := []doctorCheck{{
		Name: "version", Status: checkOK,
		Detail: fmt.Sprintf("evo %s %s/%s", version, runtime.GOOS, runtime.GOARCH),
	}}
	checks = append(checks, fileChecks()...)

	// --project-id and --region count here as in any command: doctor
	// checks the setup the next command will use.
	cfg, sources, err := config.ResolveWithSources(config.Options{
		Profile:     flagProfile,
		ProjectFile: projectConfigPath(),
		Flags:       scopeOverrides(),
	})
	if err != nil {
		checks = append(checks, doctorCheck{Name: "config", Status: checkFail, Detail: errDetail(err), auth: true})
	} else {
		checks = append(checks, configChecks(cfg, sources)...)
	}

	checks = append(checks,
		probe(ctx, "IAM API", auth.IAMEndpoint),
		probe(ctx, "Compute API", computeBaseURL),
	)

	switch {
	case err != nil:
		checks = append(checks, doctorCheck{Name: "credentials", Status: checkSkip,
			Detail: i18n.T("configuration is broken", "настройки сломаны")})
	case cfg.KeyID == "" || cfg.Secret == "":
		checks = append(checks, doctorCheck{Name: "credentials", Status: checkFail, auth: true,
			Detail: i18n.T("no access key: run 'evo configure' or set CLOUDRU_KEY_ID and CLOUDRU_SECRET",
				"нет ключа доступа: выполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET")})
	default:
		provider := tokenProvider(cfg)
		before := provider.Exchanges()
		tok, terr := provider.Token(ctx)
		if terr != nil {
			checks = append(checks, doctorCheck{Name: "credentials", Status: checkFail, Detail: errDetail(terr),
				auth: clerrors.ExitCode(terr) == clerrors.ExitAuth})
		} else {
			src := i18n.T("cached token", "токен из кэша")
			if provider.Exchanges() != before {
				src = i18n.T("token issued", "токен выдан")
			}
			checks = append(checks, doctorCheck{Name: "credentials", Status: checkOK,
				Detail: i18n.Tf("key %s: %s, valid for %s", "ключ %s: %s, действует ещё %s",
					config.MaskKeyID(cfg.KeyID), src, tok.TTL().Truncate(time.Minute))})
			checks = append(checks, keyScopeCheck(ctx, tok.Value))
		}
	}

	for _, bin := range []string{"kubectl", "helm"} {
		if path, err := exec.LookPath(bin); err == nil {
			checks = append(checks, doctorCheck{Name: bin, Status: checkOK, Detail: path})
		} else {
			checks = append(checks, doctorCheck{Name: bin, Status: checkWarn,
				Detail: i18n.T("not in PATH (needed only by the k8s_kubectl, k8s_apply and k8s_helm tools)",
					"нет в PATH (нужен только инструментам k8s_kubectl, k8s_apply и k8s_helm)")})
		}
	}
	return checks
}

// fileChecks reports the config directory, both files and their modes.
func fileChecks() []doctorCheck {
	var checks []doctorCheck
	dir := config.Dir()
	st, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return append(checks, doctorCheck{Name: "config dir", Status: checkWarn,
			Detail: dir + i18n.T(" does not exist; run 'evo configure' (or use environment variables)",
				" не существует; выполните 'evo configure' (или используйте переменные окружения)")})
	case err != nil:
		return append(checks, doctorCheck{Name: "config dir", Status: checkFail, Detail: err.Error(), auth: true})
	case config.PermissionsEnforced() && config.LooseMode(st.Mode()):
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkWarn,
			Detail: i18n.Tf("%s has mode %04o; run: chmod 700 %s", "у %s права %04o; выполните: chmod 700 %s", dir, st.Mode().Perm(), dir)})
	case !config.PermissionsEnforced() && config.PrivateDirWarning(dir) != "":
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkWarn, Detail: config.PrivateDirWarning(dir)})
	default:
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkOK, Detail: dir})
	}

	for _, f := range []struct {
		name, path string
		secret     bool
	}{
		{"config.yaml", config.SettingsPath(), false},
		{"credentials.yaml", config.CredentialsPath(), true},
	} {
		st, err := os.Stat(f.path)
		switch {
		case os.IsNotExist(err):
			checks = append(checks, doctorCheck{Name: f.name, Status: checkWarn, Detail: i18n.T("not found", "не найден")})
		case err != nil:
			checks = append(checks, doctorCheck{Name: f.name, Status: checkFail, Detail: err.Error(), auth: true})
		case !config.PermissionsEnforced():
			// Windows: the ACL instead of the mode.
			if w := config.PrivateFileWarning(f.path); w != "" {
				status := checkWarn
				if f.secret {
					status = checkFail
				}
				checks = append(checks, doctorCheck{Name: f.name, Status: status, auth: f.secret, Detail: w})
			} else {
				checks = append(checks, doctorCheck{Name: f.name, Status: checkOK,
					Detail: f.path + i18n.T(" (ACL: no access for other users)", " (ACL: у других пользователей доступа нет)")})
			}
		case config.LooseMode(st.Mode()):
			status := checkWarn
			if f.secret {
				status = checkFail
			}
			checks = append(checks, doctorCheck{Name: f.name, Status: status, auth: f.secret,
				Detail: i18n.Tf("mode %04o, readable by others; run: chmod 600 %s",
					"права %04o, файл читают другие; выполните: chmod 600 %s", st.Mode().Perm(), f.path)})
		default:
			checks = append(checks, doctorCheck{Name: f.name, Status: checkOK, Detail: fmt.Sprintf("%s (%04o)", f.path, st.Mode().Perm())})
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		legacy := filepath.Join(home, config.LegacyProjectFileName)
		if _, err := os.Stat(legacy); err == nil {
			checks = append(checks, doctorCheck{Name: "legacy config", Status: checkWarn,
				Detail: legacy + i18n.T(" is no longer read: move its settings to 'evo configure' or ./",
					" больше не читается: перенесите настройки в 'evo configure' или ./") + config.ProjectFileName})
		}
	}
	return checks
}

// configChecks shows where the key settings come from.
func configChecks(cfg config.Config, sources []config.Source) []doctorCheck {
	// "project_id from default" read as if a default project existed: an
	// unset project says so (release audit, R-06).
	project := i18n.Tf("project_id from %s", "project_id из %s", sourceOrigin(sources, "ProjectID"))
	if cfg.ProjectID == "" {
		project = i18n.T("project_id not set", "project_id не задан")
	}
	checks := []doctorCheck{{
		Name: "config", Status: checkOK,
		Detail: i18n.Tf("profile %s; key_id from %s; %s", "профиль %s; key_id из %s; %s",
			cfg.Profile, sourceOrigin(sources, "KeyID"), project),
	}}
	// Without a key "ok config … key_id from default" above a failed
	// credentials check read as a contradiction: say the key is missing.
	if cfg.KeyID == "" || cfg.Secret == "" {
		checks[0].Status = checkWarn
		checks[0].Detail = i18n.Tf("profile %s; access key not set; %s",
			"профиль %s; ключ доступа не задан; %s", cfg.Profile, project)
	}
	// Without a project every project command (vm list, vpc subnets, …)
	// fails with CONFIG_INVALID, exit 2: doctor fails the same way, not
	// "ok" with exit 0 (release audit, R-06).
	if cfg.ProjectID == "" {
		ce := clerrors.ProjectRequired()
		checks = append(checks, doctorCheck{Name: "project_id", Status: checkFail, auth: true,
			Detail: i18n.T("not set: ", "не задан: ") + ce.Hint})
	}
	for _, w := range cfg.Warnings {
		checks = append(checks, doctorCheck{Name: "config", Status: checkWarn, Detail: w})
	}
	return checks
}

// probe checks that an API host answers HTTP at all; the status code does
// not matter, only that TLS and the connection work.
func probe(ctx context.Context, name, rawURL string) doctorCheck {
	u, err := url.Parse(rawURL)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: err.Error()}
	}
	base := u.Scheme + "://" + u.Host + "/"
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: err.Error()}
	}
	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: i18n.Tf("%s unreachable: %v", "%s недоступен: %v", u.Host, err)}
	}
	resp.Body.Close()
	took := latencyText(time.Since(start))
	detail := i18n.Tf("%s reachable (%s)", "%s доступен (%s)", u.Host, took)
	if flagVerbose {
		detail = i18n.Tf("%s reachable (HTTP %d, %s)", "%s доступен (HTTP %d, %s)", u.Host, resp.StatusCode, took)
	}
	return doctorCheck{Name: name, Status: checkOK, Detail: detail, HTTPStatus: resp.StatusCode}
}

// latencyText is a probe's time: "66ms", in Russian «66 мс».
func latencyText(d time.Duration) string {
	if d < time.Second {
		return i18n.Tf("%dms", "%d мс", d.Milliseconds())
	}
	return i18n.Tf("%.1fs", "%.1f с", d.Seconds())
}

func errDetail(err error) string {
	if cliErr, ok := err.(*clerrors.CLIError); ok && cliErr.Hint != "" {
		return cliErr.Message + " — " + cliErr.Hint
	}
	return err.Error()
}

func reportDoctor(printer *output.Printer, checks []doctorCheck) error {
	failed, authFailed := 0, false
	for _, c := range checks {
		if c.Status == checkFail {
			failed++
			authFailed = authFailed || c.auth
		}
	}
	if printer.IsJSON() {
		if err := printer.JSON(map[string]any{"checks": checks, "ok": failed == 0}); err != nil {
			return err
		}
	} else {
		rows := make([][]string, len(checks))
		for i, c := range checks {
			rows[i] = []string{statusText(c.Status), c.Name, c.Detail}
		}
		printer.Table([]string{i18n.T("Status", "Статус"), i18n.T("Check", "Проверка"), i18n.T("Detail", "Подробности")}, rows)
	}
	switch {
	case failed == 0:
		return nil
	case authFailed:
		return clerrors.New(clerrors.CodeConfigInvalid, failedChecks(failed),
			i18n.T("Fix the configuration or credentials above, e.g. with 'evo configure'",
				"Исправьте настройки или ключ (см. выше), например через 'evo configure'"))
	default:
		return stderrors.New(failedChecks(failed))
	}
}

// statusText is the status of a check in the text table; JSON keeps the
// stable ok/warn/fail/skip.
func statusText(s string) string {
	if i18n.Lang() == i18n.EN {
		return s
	}
	switch s {
	case checkOK:
		return "ок"
	case checkWarn:
		return "внимание"
	case checkFail:
		return "ошибка"
	case checkSkip:
		return "пропуск"
	}
	return s
}

func failedChecks(n int) string {
	return i18n.Tf("doctor: %d check(s) failed", "doctor: не прошли проверки: %d", n)
}

// keyScopeCheck warns about a key that sees several projects: the project
// a command works in is then a setting, not a limit of the key.
func keyScopeCheck(ctx context.Context, token string) doctorCheck {
	visible, err := listProjects(ctx, token)
	switch {
	case err != nil:
		return doctorCheck{Name: "key scope", Status: checkSkip, Detail: scopeText(nil, err)}
	case len(visible) > 1:
		return doctorCheck{Name: "key scope", Status: checkWarn, Detail: manyProjectsWarning(len(visible))}
	default:
		return doctorCheck{Name: "key scope", Status: checkOK, Detail: scopeText(visible, nil)}
	}
}
