// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// 'evo image list' and 'evo image get' show the answers of image_list and
// image_get in the words of the console: a table with a footer, a card.
// --json prints the tools' answers as they are.

type imageListAnswer struct {
	Images []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		DisplayName string   `json:"display_name"`
		Type        string   `json:"type"`
		ZonesReady  []string `json:"zones_ready"`
		MinDiskGB   int      `json:"min_disk_gb"`
		// AvailableInProject is false for an image the project cannot
		// use, absent otherwise.
		AvailableInProject *bool `json:"available_in_project"`
	} `json:"images"`
	Shown              int    `json:"shown"`
	Total              int    `json:"total"`
	Hidden             int    `json:"hidden"`
	HiddenNotInProject int    `json:"hidden_not_in_project"`
	Warning            string `json:"warning"`
}

// notInProjectLine explains the images the project cannot use, under the
// list and on the card.
func notInProjectLine() string {
	return i18n.T("Images not available to the project are in the Cloud.ru catalogue, but the API will not create a VM of this project from them (probably the Marketplace product has to be connected to the project; unverified).",
		"Недоступные проекту образы есть в каталоге Cloud.ru, но ВМ этого проекта API из них не создаст (вероятно, продукт Marketplace нужно подключить к проекту; не проверено).")
}

func gb(n int) string {
	if n <= 0 {
		return "—"
	}
	return i18n.Tf("%d GB", "%d ГБ", n)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func renderImageList(p *output.Printer, w io.Writer, text string) error {
	var a imageListAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		fmt.Fprintln(w, text)
		return nil
	}
	if a.Warning != "" {
		p.Warning(a.Warning)
	}
	hidden := ""
	if a.Hidden > 0 {
		var why []string
		if n := a.Hidden - a.HiddenNotInProject; n > 0 {
			why = append(why, i18n.Tf("ready in no zone: %d", "не готовы ни в одной зоне: %d", n))
		}
		if a.HiddenNotInProject > 0 {
			why = append(why, i18n.Tf("not available to the project: %d", "недоступны проекту: %d", a.HiddenNotInProject))
		}
		hidden = " (" + strings.Join(why, "; ") + i18n.T("; show them: --all)", "; показать — --all)")
	}
	flagged := false
	for _, img := range a.Images {
		flagged = flagged || (img.AvailableInProject != nil && !*img.AvailableInProject)
	}
	explain := func() {
		if flagged || a.HiddenNotInProject > 0 {
			fmt.Fprintln(w, notInProjectLine())
		}
	}
	if len(a.Images) == 0 {
		fmt.Fprintln(w, i18n.T("No images found", "Образы не найдены")+hidden+".")
		explain()
		fmt.Fprintln(w, i18n.T("The search is a substring of the name or the display name: evo image list <part of the name>.",
			"Поиск — по подстроке имени или отображаемого имени: evo image list <часть имени>."))
		return nil
	}
	rows := make([][]string, 0, len(a.Images))
	for _, img := range a.Images {
		zones := strings.Join(img.ZonesReady, ",")
		if zones == "" {
			zones = "—"
		}
		row := []string{img.Name, clip(img.DisplayName, 48), img.Type, zones, gb(img.MinDiskGB)}
		if flagged {
			// The column is there only when a row is flagged: the check
			// was made, so the other rows are available.
			avail := i18n.T("yes", "да")
			if img.AvailableInProject != nil && !*img.AvailableInProject {
				avail = i18n.T("NO", "НЕТ")
			}
			row = append(row, avail)
		}
		rows = append(rows, row)
	}
	headers := []string{
		i18n.T("NAME", "ИМЯ"), i18n.T("DISPLAY NAME", "ОТОБРАЖАЕМОЕ ИМЯ"), i18n.T("TYPE", "ТИП"),
		i18n.T("ZONES", "ЗОНЫ"), i18n.T("MIN DISK", "МИН. ДИСК"),
	}
	if flagged {
		headers = append(headers, i18n.T("AVAILABLE TO PROJECT", "ДОСТУПЕН ПРОЕКТУ"))
	}
	p.Table(headers, rows)
	fmt.Fprintln(w, i18n.Tf("Shown %d of %d%s. UUID: --json or 'evo image get <name>'.",
		"Показаны %d из %d%s. UUID — в --json или 'evo image get <имя>'.", len(a.Images), a.Total, hidden))
	explain()
	return nil
}

type imageCardAnswer struct {
	Image struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Type        string `json:"type"`
		MinCPU      int    `json:"min_cpu"`
		MinRAM      int    `json:"min_ram"`
		MinDisk     int    `json:"min_disk"`
	} `json:"image"`
	Ready          bool               `json:"ready"`
	ZoneStates     map[string]string  `json:"zone_states"`
	ZonesReady     []string           `json:"zones_ready"`
	Fields         []images.FieldInfo `json:"fields"`
	FieldsRequired []string           `json:"fields_required"`
	// AvailableInProject is false for an image the project cannot use.
	AvailableInProject *bool `json:"available_in_project"`
}

// fieldFiller says who fills a field, for the card.
func fieldFiller(f images.FieldInfo) string {
	switch f.FilledBy {
	case images.FilledByEvo:
		switch f.Role {
		case images.RoleKey:
			return i18n.T("evo: the SSH key", "evo: ключ SSH")
		case images.RoleHostname:
			return i18n.T("evo: the VM name", "evo: имя ВМ")
		case images.RoleLogin:
			return i18n.T("evo: the login", "evo: логин")
		case images.RolePassword:
			return i18n.T("evo: a random password", "evo: случайный пароль")
		}
		return "evo"
	case images.FilledByDefault:
		if f.Default == "" {
			return i18n.T("the image's default", "значение образа по умолчанию")
		}
		return i18n.Tf("default: %s", "по умолчанию: %s", clip(f.Default, 40))
	case images.FilledByUser:
		return i18n.T("YOU: mandatory, evo cannot fill it", "ВЫ: обязательное, evo его не заполнит")
	case images.FilledByStatus:
		return i18n.T("status flag, never sent", "служебный флаг, не передаётся")
	}
	return i18n.T("left empty (optional)", "не заполняется (необязательное)")
}

func renderImageCard(p *output.Printer, w io.Writer, text string) error {
	var a imageCardAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Image.ID == "" {
		fmt.Fprintln(w, text)
		return nil
	}
	img := a.Image
	lines := []cardLine{
		{i18n.T("Name", "Имя"), img.Name},
		{i18n.T("Display name", "Отображаемое имя"), img.DisplayName},
		{i18n.T("Type", "Тип"), img.Type},
		{"UUID", img.ID},
	}
	if d := strings.TrimSpace(img.Description); d != "" {
		lines = append(lines, cardLine{i18n.T("Description", "Описание"), clip(d, 200)})
	}
	var min []string
	if img.MinCPU > 0 {
		min = append(min, fmt.Sprintf("%d vCPU", img.MinCPU))
	}
	if img.MinRAM > 0 {
		min = append(min, i18n.Tf("%d GB RAM", "%d ГБ RAM", img.MinRAM))
	}
	if img.MinDisk > 0 {
		min = append(min, i18n.Tf("disk %d GB", "диск %d ГБ", img.MinDisk))
	}
	if len(min) == 0 {
		min = []string{i18n.T("not stated", "не указан")}
	}
	lines = append(lines, cardLine{i18n.T("Minimum", "Минимум"), strings.Join(min, ", ")})

	zones := i18n.T("ready in no zone", "не готов ни в одной зоне")
	if len(a.ZonesReady) > 0 {
		zones = strings.Join(a.ZonesReady, ", ") + i18n.T(" — ready", " — готов")
	}
	var other []string
	for name, state := range a.ZoneStates {
		short := images.ZoneShort(name)
		ready := false
		for _, z := range a.ZonesReady {
			ready = ready || z == short
		}
		if !ready {
			other = append(other, short+": "+state)
		}
	}
	sort.Strings(other)
	if len(other) > 0 {
		zones += "; " + strings.Join(other, ", ")
	}
	lines = append(lines, cardLine{i18n.T("Zones", "Зоны"), zones})
	unavailable := a.AvailableInProject != nil && !*a.AvailableInProject
	if unavailable {
		lines = append(lines, cardLine{i18n.T("Project", "Проект"), i18n.T("NOT available: a VM cannot be created from it", "НЕ доступен: ВМ из него не создать")})
	}
	printCard(w, lines)

	fmt.Fprintln(w)
	if len(a.Fields) == 0 {
		fmt.Fprintln(w, i18n.T("Fields at VM creation: none.", "Поля при создании ВМ: нет."))
	} else {
		fmt.Fprintln(w, i18n.T("Fields at VM creation:", "Поля при создании ВМ:"))
		rows := make([][]string, 0, len(a.Fields))
		for _, f := range a.Fields {
			req := ""
			if f.Required {
				req = i18n.T("yes", "да")
			}
			rows = append(rows, []string{"  " + f.Name, f.FieldType, req, fieldFiller(f)})
		}
		p.Table([]string{"  " + i18n.T("FIELD", "ПОЛЕ"), i18n.T("TYPE", "ТИП"), i18n.T("REQUIRED", "ОБЯЗАТЕЛЬНОЕ"), i18n.T("FILLED BY", "КТО ЗАПОЛНЯЕТ")}, rows)
	}
	meta := ""
	if len(a.FieldsRequired) > 0 {
		fmt.Fprintln(w, i18n.Tf("Needs values evo cannot make itself: %s — pass them with --meta field=value.",
			"Нужны значения, которых evo сам не знает: %s — передайте их: --meta поле=значение.", strings.Join(a.FieldsRequired, ", ")))
		for _, f := range a.FieldsRequired {
			meta += " --meta " + f + i18n.T("=<value>", "=<значение>")
		}
	}

	fmt.Fprintln(w)
	if unavailable {
		fmt.Fprintln(w, notInProjectLine())
		fmt.Fprintln(w, i18n.T("Images the project can use: evo image list <query>", "Образы, доступные проекту: evo image list <запрос>"))
		return nil
	}
	fmt.Fprintln(w, i18n.T("Create a VM:", "Создать ВМ:"))
	fmt.Fprintf(w, "  evo vm create %s --image %s%s\n", i18n.T("<vm-name>", "<имя-вм>"), images.ShellQuote(img.Name), meta)
	if img.MinCPU > 0 || img.MinRAM > 0 {
		fmt.Fprintln(w, i18n.T("  the flavor must meet the minimum above: --flavor <name> (evo vm flavors)",
			"  flavor — не меньше минимума выше: --flavor <имя> (evo vm flavors)"))
	}
	return nil
}
