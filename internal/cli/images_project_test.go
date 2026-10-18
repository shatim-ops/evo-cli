// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Образы общего каталога, которых проект не видит (project_id=): живой
// факт 16.10.2026 — так невидимы все Astra Linux, Альт, РЕД ОС и SberLinux
// тестового проекта, и vm create из них отвечает 422 wrong_image. Все ID
// выдуманы.

// image list прячет такие образы, как неготовые, и говорит почему; --all
// показывает их с колонкой «ДОСТУПЕН ПРОЕКТУ».
func TestImageList_NotInProjectHiddenAndFlagged(t *testing.T) {
	russian(t)
	cloud := withImageCloud(t)
	cloud.hidden[imgID(1)] = true
	out, errOut, code := execCLI(t, "image", "list", "astra")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"alse-base-1-7-7  ",
		"Показаны 1 из 3 (не готовы ни в одной зоне: 1; недоступны проекту: 1; показать — --all).",
		"Недоступные проекту образы есть в каталоге Cloud.ru, но ВМ этого проекта API из них не создаст (вероятно, продукт Marketplace нужно подключить к проекту; не проверено).",
	})
	if strings.Contains(out, "AstraLinuxSmolensk1.8.3KES") || strings.Contains(out, "ДОСТУПЕН ПРОЕКТУ") {
		t.Errorf("hidden image or the column without --all:\n%s", out)
	}

	out, _, _ = execCLI(t, "image", "list", "astra", "--all")
	t.Logf("\n%s", out)
	assertHas(t, out, []string{"ДОСТУПЕН ПРОЕКТУ", "Показаны 3 из 3.", "Недоступные проекту образы есть"})
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "AstraLinuxSmolensk1.8.3KES "):
			if !strings.HasSuffix(strings.TrimSpace(line), "НЕТ") {
				t.Errorf("unavailable row: %q", line)
			}
		case strings.HasPrefix(line, "alse-base-1-7-7 "):
			if !strings.HasSuffix(strings.TrimSpace(line), "да") {
				t.Errorf("available row: %q", line)
			}
		}
	}

	// Ничего недоступного — ни колонки, ни пояснения.
	out, _, _ = execCLI(t, "image", "list", "ubuntu")
	if strings.Contains(out, "ДОСТУПЕН ПРОЕКТУ") || strings.Contains(out, "Недоступные проекту") {
		t.Errorf("ubuntu:\n%s", out)
	}

	out, _, _ = execCLI(t, "--json", "image", "list", "astra", "--all")
	var got struct {
		Images []map[string]any `json:"images"`
		Note   string           `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, row := range got.Images {
		v, has := row["available_in_project"]
		if want := row["id"] == imgID(1); has != want || (has && v != false) {
			t.Errorf("row %v: available_in_project %v (present %v)", row["name"], v, has)
		}
	}
	if !strings.Contains(got.Note, "not available to this project") || !strings.Contains(got.Note, "probably") {
		t.Errorf("note: %q", got.Note)
	}
	out, _, _ = execCLI(t, "--json", "image", "list", "astra")
	if !strings.Contains(out, `"hidden_not_in_project": 1`) {
		t.Errorf("hidden_not_in_project:\n%s", out)
	}
}

// Каталог проекта не прочитался: ничего не отмечено, предупреждение.
func TestImageList_ProjectViewFails(t *testing.T) {
	russian(t)
	cloud := withImageCloud(t)
	cloud.hidden[imgID(1)] = true
	*cloud.projErr = errors.New("503 unavailable")
	out, errOut, code := execCLI(t, "image", "list", "astra")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "AstraLinuxSmolensk1.8.3KES") || strings.Contains(out, "недоступны проекту") {
		t.Errorf("marked although the project's view failed:\n%s", out)
	}
	if !strings.Contains(out+errOut, "недоступные проекту образы не отмечены: 503 unavailable") {
		t.Errorf("no warning:\n%s\n%s", out, errOut)
	}
}

// Карточка недоступного образа: пометка вместо команды создания ВМ.
func TestImageGet_NotInProject(t *testing.T) {
	russian(t)
	cloud := withImageCloud(t)
	cloud.hidden[imgID(1)] = true
	for _, ref := range []string{"AstraLinuxSmolensk1.8.3KES", imgID(1)} {
		out, errOut, code := execCLI(t, "image", "get", ref)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", ref, code, errOut)
		}
		assertHas(t, out, []string{
			"Проект:           НЕ доступен: ВМ из него не создать\n",
			"Недоступные проекту образы есть в каталоге Cloud.ru",
			"Образы, доступные проекту: evo image list <запрос>\n",
		})
		if strings.Contains(out, "Создать ВМ") {
			t.Errorf("%s: the card offers to create a VM:\n%s", ref, out)
		}

		out, _, _ = execCLI(t, "--json", "image", "get", ref)
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		if got["available_in_project"] != false || got["create"] != nil || !strings.Contains(got["note"].(string), "image_list") {
			t.Errorf("%s json: %v", ref, got)
		}
	}
	// Доступный образ — без пометки.
	out, _, _ := execCLI(t, "--json", "image", "get", "OpensearchDashboards")
	if strings.Contains(out, "available_in_project") {
		t.Errorf("available image flagged:\n%s", out)
	}
}

// vm create из недоступного проекту образа — отказ до вопроса и до любых
// изменений в облаке, код 4; по имени и по UUID, с --yes и без.
func TestVMCreate_ImageNotInProjectRefused(t *testing.T) {
	russian(t)
	for _, tc := range []struct{ name, flag, ref string }{
		{"по имени", "--image", "astralinuxsmolensk1.8.3kes"},
		{"по UUID", "--image", imgID(1)},
		{"по UUID без --yes", "--image", imgID(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupCLI(t)
			cloud := withVMCreateCloud(t)
			cloud.hidden[imgID(1)] = true
			args := []string{"vm", "create", "web", tc.flag, tc.ref}
			if !strings.Contains(tc.name, "без --yes") {
				args = append([]string{"--yes"}, args...)
			}
			code, out, errOut := runCLI(t, "", args...)
			if code != clerrors.ExitNotFound {
				t.Fatalf("exit %d, ждали %d:\n%s\n%s", code, clerrors.ExitNotFound, out, errOut)
			}
			for _, w := range []string{
				`образ "AstraLinuxSmolensk1.8.3KES" есть в каталоге Cloud.ru, но недоступен проекту: создать из него ВМ API не даст`,
				"evo image list <запрос>", "вероятно", "не проверено",
			} {
				if !strings.Contains(errOut, w) {
					t.Errorf("нет %q в:\n%s", w, errOut)
				}
			}
			if strings.Contains(out+errOut, "Создать?") || strings.Contains(out+errOut, "--dry-run") {
				t.Errorf("вопрос или превью до отказа:\n%s\n%s", out, errOut)
			}
			noMutations(t, cloud)
		})
	}

	// Тот же образ, видимый проекту, создаётся: Astra Linux vm create берёт.
	setupCLI(t)
	cloud := withVMCreateCloud(t)
	if code, _, errOut := runCLI(t, "", "--yes", "vm", "create", "web", "--image", "astralinuxsmolensk1.8.3kes", "--no-public-ip"); code != 0 || len(cloud.creates) != 1 {
		t.Errorf("доступный Astra Linux: exit %d, creates %d\n%s", code, len(cloud.creates), errOut)
	}
}

// errNoPriceAPI — ответ подделок на PriceCalculation: расчёт цены не
// получен, превью показывает оценку по карте, как раньше.
var errNoPriceAPI = errors.New("price-calculation: not in this fake")
