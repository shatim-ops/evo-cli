// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// 'evo k8s addons' and 'evo k8s addon-versions' show the answers of
// k8s_addons and k8s_addon_versions as tables with a footer that names the
// next command; --json prints the tools' answers as they are.

type addonListAnswer struct {
	Addons []struct {
		Name        string   `json:"name"`
		DisplayName string   `json:"display_name"`
		Categories  []string `json:"categories"`
		Billable    bool     `json:"billable"`
	} `json:"addons"`
	Shown      int      `json:"shown"`
	Total      int      `json:"total"`
	Categories []string `json:"categories"`
}

// dash is s, or "—" for an empty cell.
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func renderAddonList(p *output.Printer, w io.Writer, text string) error {
	var a addonListAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		fmt.Fprintln(w, text)
		return nil
	}
	if len(a.Addons) == 0 {
		fmt.Fprintln(w, i18n.T("No addons found.", "Плагины не найдены."))
		fmt.Fprintln(w, i18n.T("The search is a substring of the name, the display name or the description: evo k8s addons <substring>.",
			"Поиск — по подстроке имени, отображаемого имени или описания: evo k8s addons <подстрока>."))
		if len(a.Categories) > 0 {
			fmt.Fprintln(w, i18n.Tf("Categories (--category): %s.", "Категории (--category): %s.", output.SanitizeCell(strings.Join(a.Categories, ", "))))
		}
		return nil
	}
	rows := make([][]string, 0, len(a.Addons))
	for _, ad := range a.Addons {
		rows = append(rows, []string{ad.Name, clip(ad.DisplayName, 40), clip(strings.Join(ad.Categories, ", "), 40), yesNo(ad.Billable)})
	}
	p.Table([]string{
		i18n.T("NAME", "ИМЯ"), i18n.T("DISPLAY NAME", "ОТОБРАЖАЕМОЕ ИМЯ"),
		i18n.T("CATEGORIES", "КАТЕГОРИИ"), i18n.T("BILLABLE", "ПЛАТНЫЙ"),
	}, rows)
	fmt.Fprintln(w, i18n.Tf("Shown %d of %d. Versions: evo k8s addon-versions <name>; install: evo k8s add-addon <cluster-id> <name>.",
		"Показаны %d из %d. Версии: evo k8s addon-versions <имя>; установка: evo k8s add-addon <id-кластера> <имя>.", a.Shown, a.Total))
	return nil
}

type addonVersionsAnswer struct {
	Addon       string `json:"addon"`
	DisplayName string `json:"display_name"`
	Releases    []struct {
		Version      string          `json:"version"`
		AddonVersion string          `json:"addon_version"`
		Mode         string          `json:"mode"`
		KubeVersions string          `json:"kube_versions"`
		ReadyWorkers int             `json:"ready_workers"`
		Namespace    string          `json:"namespace"`
		HasSchema    bool            `json:"has_schema"`
		SchemaBytes  int             `json:"schema_bytes"`
		Latest       bool            `json:"latest"`
		Schema       json.RawMessage `json:"configuration_schema"`
	} `json:"releases"`
	Latest string `json:"latest"`
}

// modeText is a release's support mode in words.
func modeText(m string) string {
	switch m {
	case "ACTIVE":
		return i18n.T("active", "активная")
	case "EXPERIMENTAL":
		return i18n.T("experimental", "экспериментальная")
	case "END_OF_SUPPORT":
		return i18n.T("end of support", "поддержка завершается")
	case "END_OF_LIFE":
		return i18n.T("end of life", "снята с поддержки")
	case "INACTIVE":
		return i18n.T("inactive", "неактивная")
	}
	return dash(m)
}

func schemaSize(has bool, n int) string {
	if !has {
		return "—"
	}
	if n < 1024 {
		return i18n.Tf("%d B", "%d Б", n)
	}
	return i18n.Tf("%.0f KB", "%.0f КБ", float64(n)/1024)
}

func renderAddonVersions(p *output.Printer, w io.Writer, text string) error {
	var a addonVersionsAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Addon == "" {
		fmt.Fprintln(w, text)
		return nil
	}
	if a.DisplayName != "" {
		fmt.Fprintf(w, "%s — %s\n", a.Addon, output.SanitizeCell(a.DisplayName))
	}
	if len(a.Releases) == 0 {
		fmt.Fprintln(w, i18n.T("The addon has no releases.", "У плагина нет версий."))
		return nil
	}
	rows := make([][]string, 0, len(a.Releases))
	for _, r := range a.Releases {
		version := r.Version
		if r.Latest {
			version += " *"
		}
		workers := "—"
		if r.ReadyWorkers > 0 {
			workers = fmt.Sprint(r.ReadyWorkers)
		}
		rows = append(rows, []string{version, r.AddonVersion, modeText(r.Mode), dash(r.KubeVersions),
			workers, dash(r.Namespace), schemaSize(r.HasSchema, r.SchemaBytes)})
	}
	p.Table([]string{
		i18n.T("VERSION", "ВЕРСИЯ"), i18n.T("ADDON VERSION", "ВЕРСИЯ ПЛАГИНА"), i18n.T("SUPPORT", "ПОДДЕРЖКА"),
		"KUBERNETES", i18n.T("NODES", "УЗЛОВ"), i18n.T("NAMESPACE", "NAMESPACE"), i18n.T("SETTINGS SCHEMA", "СХЕМА НАСТРОЕК"),
	}, rows)
	if a.Latest != "" {
		fmt.Fprintln(w, i18n.Tf("* the version add-addon installs without --version (the newest active one). Install: evo k8s add-addon <cluster-id> %s [--version <version>] [--config settings.json].",
			"* версия, которую add-addon ставит без --version (новейшая активная). Установка: evo k8s add-addon <id-кластера> %s [--version <версия>] [--config настройки.json].", a.Addon))
	} else {
		fmt.Fprintln(w, i18n.T("No version to install: every release is end of life or inactive.",
			"Устанавливать нечего: все версии сняты с поддержки или неактивны."))
	}
	fmt.Fprintln(w, i18n.T("KUBERNETES is the cluster version range, NODES the ready worker nodes the release needs; the settings schema: --full (with --json — as JSON).",
		"KUBERNETES — какие версии кластера подходят, УЗЛОВ — сколько готовых рабочих узлов нужно; схема настроек — --full (с --json — в JSON)."))
	for _, r := range a.Releases {
		if len(r.Schema) == 0 {
			continue
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, i18n.Tf("Settings schema of %s:", "Схема настроек %s:", r.Version))
		var s string
		if json.Unmarshal(r.Schema, &s) == nil {
			// Not JSON in the catalogue: the string as is, line by line.
			for _, line := range strings.Split(s, "\n") {
				fmt.Fprintln(w, output.SanitizeCell(line))
			}
			continue
		}
		var buf bytes.Buffer
		if json.Indent(&buf, r.Schema, "", "  ") != nil {
			continue
		}
		fmt.Fprintln(w, buf.String())
	}
	return nil
}
