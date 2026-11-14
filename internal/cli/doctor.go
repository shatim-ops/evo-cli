// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: i18n.T("Check configuration, credentials and connectivity", "Проверить настройки, ключ доступа и связь с API"),
		Long: i18n.T(`Check the setup: config files and their permissions, where each setting
comes from, reachability of the IAM and Compute APIs, the credentials,
kubectl/helm in PATH (needed only by the k8s tools; a warning, not an error),
and evo's entry in Claude Desktop, if it has one: the evo it starts is there
and is this one, its profile is there (a warning, not an error).

Exits with 2 if the configuration or credentials are broken, 1 if only
connectivity checks failed.`, `Проверить окружение: файлы настроек и их права, откуда взята каждая
настройка, доступность IAM и Compute API, ключ доступа, kubectl/helm в PATH
(нужны только инструментам k8s; их отсутствие — предупреждение, не ошибка) и
запись evo в Claude Desktop, если она есть: запускаемый evo на месте и это
этот evo, её профиль есть (предупреждение, не ошибка).

Код выхода 2 — сломаны настройки или ключ, 1 — не прошли только проверки
связи.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := &tools.Caller{Verbose: flagVerbose, Doctor: doctorLocal}
			text, err := callEngineTool(cmd, "doctor", map[string]any{}, c)
			var answer struct {
				Checks []tools.DoctorCheck `json:"checks"`
				OK     bool                `json:"ok"`
			}
			if jerr := json.Unmarshal([]byte(text), &answer); jerr != nil {
				return jerr
			}
			if p := cmdPrinter(cmd); p.IsJSON() {
				// The checks and ok, as since v1.0: the error is the
				// envelope in stderr.
				if jerr := p.JSON(map[string]any{"checks": answer.Checks, "ok": answer.OK}); jerr != nil {
					return jerr
				}
			} else {
				rows := make([][]string, len(answer.Checks))
				for i, ch := range answer.Checks {
					rows[i] = []string{statusText(ch.Status), checkLabel(ch.Name), ch.Detail}
				}
				p.Table([]string{i18n.T("Status", "Статус"), i18n.T("Check", "Проверка"), i18n.T("Detail", "Подробности")}, rows)
			}
			var ce *clerrors.CLIError
			if stderrors.As(err, &ce) && ce.Code == clerrors.CodeAPIUnreachable {
				// Only connectivity failed: exit 1 and reason ERROR, as
				// since v1.0; the tool says API_UNREACHABLE (jsonAliasCompat).
				return stderrors.New(ce.Message)
			}
			return err
		},
	}
	return engineCmd(cmd, "doctor")
}

// checkLabels are the names of doctor's checks in the human column, in the
// CLI's language (D6; Astra's list #1835): the JSON keeps the machine name.
var checkLabels = map[string][2]string{
	"version":               {"version", "версия"},
	"config":                {"config", "настройки"},
	"config dir":            {"config dir", "каталог настроек"},
	"legacy config":         {"legacy config", "старые настройки"},
	"other evo":             {"other evo", "другой evo"},
	"credentials":           {"credentials", "ключ доступа"},
	"key term":              {"key term", "срок ключа"},
	"key roles":             {"key roles", "роли ключа"},
	"key scope":             {"key scope", "область действия ключа"},
	"pending operation":     {"pending operation", "незавершённая операция"},
	"lost floating IP":      {"lost floating IP", "floating IP с неизвестным исходом создания"},
	"foreign intent record": {"foreign intent record", "чужая запись намерения"},
}

// checkLabel is the name of a check as the human column shows it: its
// label, else the name as it is (a file, a program, a service: D6).
func checkLabel(name string) string {
	if l, ok := checkLabels[name]; ok {
		return i18n.T(l[0], l[1])
	}
	return name
}

// doctorLocal is the CLI's part of doctor's answer (tools.Caller.Doctor):
// the binary and the config files before the tool's own checks, kubectl
// and helm in PATH after them.
func doctorLocal(own []tools.DoctorCheck) []tools.DoctorCheck {
	checks := []tools.DoctorCheck{{
		Name: "version", Status: tools.CheckOK,
		Detail: fmt.Sprintf("evo %s %s/%s", version, runtime.GOOS, runtime.GOARCH),
	}}
	checks = append(checks, otherEvoChecks()...)
	checks = append(checks, agentEntryChecks()...)
	checks = append(checks, fileChecks()...)
	checks = append(checks, own...)
	for _, bin := range []string{"kubectl", "helm"} {
		if path, err := exec.LookPath(bin); err == nil {
			checks = append(checks, tools.DoctorCheck{Name: bin, Status: tools.CheckOK, Detail: path})
		} else {
			checks = append(checks, tools.DoctorCheck{Name: bin, Status: tools.CheckWarn,
				Detail: i18n.T("not in PATH (needed only by the k8s_kubectl, k8s_apply and k8s_helm tools)",
					"нет в PATH (нужен только инструментам k8s_kubectl, k8s_apply и k8s_helm)")})
		}
	}
	return checks
}

// fileChecks reports the config directory, both files and their modes.
func fileChecks() []tools.DoctorCheck {
	var checks []tools.DoctorCheck
	dir := config.Dir()
	if dir == "" {
		return append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckWarn, Detail: tools.CheckDetail(config.NoConfigDir())})
	}
	st, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckWarn,
			Detail: dir + i18n.T(" does not exist; run 'evo configure' (or use environment variables)",
				" не существует; выполните 'evo configure' (или используйте переменные окружения)")})
	case err != nil:
		return append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckFail, Detail: err.Error(), Auth: true})
	case !st.IsDir():
		// CLOUDRU_CONFIG_DIR set to a file (config.yaml itself): the checks
		// of the files below would only say "not a directory" twice.
		return append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckFail, Auth: true,
			Detail: tools.CheckDetail(config.NotADirectory(dir))})
	case config.PermissionsEnforced() && config.LooseMode(st.Mode()):
		checks = append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckWarn,
			Detail: i18n.Tf("%s has mode %04o; run: chmod 700 %s", "у %s права %04o; выполните: chmod 700 %s", dir, st.Mode().Perm(), dir)})
	case !config.PermissionsEnforced() && config.PrivateDirWarning(dir) != "":
		checks = append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckWarn, Detail: config.PrivateDirWarning(dir)})
	default:
		checks = append(checks, tools.DoctorCheck{Name: "config dir", Status: tools.CheckOK, Detail: dir})
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
			checks = append(checks, tools.DoctorCheck{Name: f.name, Status: tools.CheckWarn, Detail: i18n.T("not found", "не найден")})
		case err != nil:
			checks = append(checks, tools.DoctorCheck{Name: f.name, Status: tools.CheckFail, Detail: err.Error(), Auth: true})
		case !config.PermissionsEnforced():
			// Windows: the ACL instead of the mode.
			if w := config.PrivateFileWarning(f.path); w != "" {
				status := tools.CheckWarn
				if f.secret {
					status = tools.CheckFail
				}
				checks = append(checks, tools.DoctorCheck{Name: f.name, Status: status, Auth: f.secret, Detail: w})
			} else {
				checks = append(checks, tools.DoctorCheck{Name: f.name, Status: tools.CheckOK,
					Detail: f.path + i18n.T(" (ACL: no access for other users)", " (ACL: у других пользователей доступа нет)")})
			}
		case config.LooseMode(st.Mode()):
			status := tools.CheckWarn
			if f.secret {
				status = tools.CheckFail
			}
			checks = append(checks, tools.DoctorCheck{Name: f.name, Status: status, Auth: f.secret,
				Detail: i18n.Tf("mode %04o, readable by others; run: chmod 600 %s",
					"права %04o, файл читают другие; выполните: chmod 600 %s", st.Mode().Perm(), f.path)})
		default:
			checks = append(checks, tools.DoctorCheck{Name: f.name, Status: tools.CheckOK, Detail: fmt.Sprintf("%s (%04o)", f.path, st.Mode().Perm())})
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		legacy := filepath.Join(home, config.LegacyProjectFileName)
		if _, err := os.Stat(legacy); err == nil {
			checks = append(checks, tools.DoctorCheck{Name: "legacy config", Status: tools.CheckWarn,
				Detail: legacy + i18n.T(" is no longer read: move its settings to 'evo configure' or ./",
					" больше не читается: перенесите настройки в 'evo configure' или ./") + config.ProjectFileName})
		}
	}
	return checks
}

// statusText is the status of a check in the text table; JSON keeps the
// stable ok/warn/fail/skip.
func statusText(s string) string {
	if i18n.Lang() == i18n.EN {
		return s
	}
	switch s {
	case tools.CheckOK:
		return "ок"
	case tools.CheckWarn:
		return "внимание"
	case tools.CheckFail:
		return "ошибка"
	case tools.CheckSkip:
		return "пропуск"
	}
	return s
}
