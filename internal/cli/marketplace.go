// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/marketplace"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// 'evo marketplace list' shows the answer of marketplace_list: a table of
// what can be deployed and the command of its own tree that deploys it.
// --json prints the tool's answer as is.

// marketDeploy is the deploy of a row: the tool's arguments.
type marketDeploy struct {
	Args map[string]any `json:"args"`
}

type marketplaceAnswer struct {
	Items []struct {
		Kind               string        `json:"kind"`
		Name               string        `json:"name"`
		DisplayName        string        `json:"display_name"`
		AvailableInProject *bool         `json:"available_in_project"`
		Ready              *bool         `json:"ready"`
		Deploy             *marketDeploy `json:"deploy"`
	} `json:"items"`
	Total              int            `json:"total"`
	Counts             map[string]int `json:"counts"`
	HiddenNotInProject int            `json:"hidden_not_in_project"`
	HiddenNotReady     int            `json:"hidden_not_ready"`
	Warnings           []string       `json:"warnings"`
}

func renderMarketplaceList(p *output.Printer, w io.Writer, text string) error {
	var a marketplaceAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		fmt.Fprintln(w, text)
		return nil
	}
	for _, warn := range a.Warnings {
		p.Warning(warn)
	}
	var why []string
	if a.HiddenNotInProject > 0 {
		why = append(why, i18n.Tf("not available to the project: %d", "недоступны проекту: %d", a.HiddenNotInProject))
	}
	if a.HiddenNotReady > 0 {
		why = append(why, i18n.Tf("ready in no zone: %d", "не готовы ни в одной зоне: %d", a.HiddenNotReady))
	}
	hidden := ""
	if len(why) > 0 {
		hidden = i18n.Tf("Hidden images: %s; show them: --all.", "Скрыты образы: %s; показать — --all.", strings.Join(why, "; "))
	}
	flagged, hasImages := false, false
	for _, it := range a.Items {
		flagged = flagged || (it.AvailableInProject != nil && !*it.AvailableInProject)
		hasImages = hasImages || it.Kind != marketplace.KindAddon
	}

	if len(a.Items) == 0 {
		fmt.Fprintln(w, i18n.T("Nothing found.", "Ничего не найдено."))
		if hidden != "" {
			fmt.Fprintln(w, hidden)
		}
		if a.HiddenNotInProject > 0 {
			fmt.Fprintln(w, notInProjectLine())
		}
		fmt.Fprintln(w, i18n.T("The search is a substring of the name or the display name (of an addon, the description too): evo marketplace list <substring>.",
			"Поиск — по подстроке имени или отображаемого имени (у плагина — и описания): evo marketplace list <подстрока>."))
		return nil
	}
	rows := make([][]string, 0, len(a.Items))
	for _, it := range a.Items {
		rows = append(rows, []string{it.Kind, it.Name, clip(it.DisplayName, 48), deployCell(it.Kind, it.AvailableInProject, it.Ready, it.Deploy)})
	}
	p.Table([]string{
		i18n.T("KIND", "ВИД"), i18n.T("NAME", "ИМЯ"), i18n.T("DISPLAY NAME", "ОТОБРАЖАЕМОЕ ИМЯ"), i18n.T("HOW TO DEPLOY", "КАК РАЗВЕРНУТЬ"),
	}, rows)
	fmt.Fprintln(w, i18n.Tf("Shown %d of %d: vm-os %d, vm-app %d, k8s-addon %d.",
		"Показаны %d из %d: vm-os — %d, vm-app — %d, k8s-addon — %d.",
		len(a.Items), a.Total, a.Counts[marketplace.KindOS], a.Counts[marketplace.KindApp], a.Counts[marketplace.KindAddon]))
	if hidden != "" {
		fmt.Fprintln(w, hidden)
	}
	if flagged || a.HiddenNotInProject > 0 {
		fmt.Fprintln(w, notInProjectLine())
	}
	if hasImages {
		fmt.Fprintln(w, i18n.T("vm-os and vm-app tell OS images from products by the name (a heuristic). Whether an image's licence is paid the API does not say: the price calculation gives 0 for every image. An image's card: evo image get <name>.",
			"vm-os и vm-app различаются по имени образа (эвристика). Платная ли лицензия образа, API не говорит: в расчёте цены любой образ стоит 0. Карточка образа — evo image get <имя>."))
	}
	return nil
}

// deployCell is the command that deploys a row, or why there is none.
func deployCell(kind string, available, ready *bool, deploy *marketDeploy) string {
	switch {
	case available != nil && !*available:
		return i18n.T("not available to the project", "недоступен проекту")
	case ready != nil && !*ready:
		return i18n.T("ready in no zone", "не готов ни в одной зоне")
	case deploy == nil:
		return "—"
	}
	if kind == marketplace.KindAddon {
		name, _ := deploy.Args["name"].(string)
		return "evo k8s add-addon " + i18n.T("<cluster-id>", "<id-кластера>") + " " + images.ShellQuote(name)
	}
	ref, _ := deploy.Args["image"].(string)
	return "evo vm create " + i18n.T("<vm-name>", "<имя-вм>") + " --image " + images.ShellQuote(ref)
}
