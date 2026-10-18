// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Русский вывод превью и шагов; остальные тесты пакета идут по-английски
// (TestMain).

func russian(t *testing.T) {
	t.Helper()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
}

func TestRussian_DeployPreview(t *testing.T) {
	russian(t)
	eng, cloud, opts, stdout, _ := newDeployForConfirm(t)
	opts.DryRun = true

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if cloud.createCalled {
		t.Fatal("--dry-run created a VM")
	}
	out := stdout.String()
	t.Logf("\n%s", out)
	for _, want := range []string{
		"==> [1/5] Аутентификация...\n",
		"Пробный запуск, ничего не меняется. План: создать ВМ ",
		"(small) в зоне ru-moscow-1\n",
		"  проект:     ",
		"  flavor:     small (2 vCPU, 2 ГБ RAM)\n",
		"  зона:       ru-moscow-1",
		"  образ:      Ubuntu 22.04",
		"  диск:       загрузочный, 10 ГБ (цена по тарифу SSD)\n",
		"  сеть:       подсеть ",
		"  внешний IP: будет создан и привязан floating IP\n",
		"  цена:       ~",
		" в месяц (вычисления ",
		"оценка по тарифной карте evo, а не счёт",
		"  цена API:   не получена (", // the fake has no price API
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Dry run") || strings.Contains(out, "project:") {
		t.Errorf("English left in the preview:\n%s", out)
	}
}

func TestRussian_DeployRefusedWithoutTTY(t *testing.T) {
	russian(t)
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader("\n"), false)

	err := eng.Deploy(context.Background(), opts)
	if clerrors.ExitCode(err) != clerrors.ExitConfirmationRequired || cloud.createCalled {
		t.Fatalf("err %v, created %v", err, cloud.createCalled)
	}
	if !strings.Contains(stderr.String(), "План: создать ВМ ") ||
		!strings.Contains(err.Error(), "deploy не выполняется: нужно подтверждение, а stdin — не терминал") {
		t.Errorf("err %v, stderr:\n%s", err, stderr.String())
	}
}

func TestRussian_DeployAnswerDa(t *testing.T) {
	russian(t)
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader("да\n"), true)

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !cloud.createCalled || !strings.Contains(stderr.String(), "Продолжить? (y/д — да, Enter — нет) ") {
		t.Fatalf("created = %v, prompt:\n%s", cloud.createCalled, stderr.String())
	}
}

func TestRussian_DestroyPreview(t *testing.T) {
	russian(t)
	e, buf := destroyEngineWithVM(t)
	sf, _ := state.Read(state.DefaultPath(e.StateDir))
	rec := sf.ActiveDeployment()
	rec.VMIP = "203.0.113.7"
	sf.UpsertDeployment(*rec)
	_ = state.Write(state.DefaultPath(e.StateDir), sf)

	if err := e.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	t.Logf("\n%s", buf.String())
	for _, want := range []string{
		"==> [1/3] Чтение сведений о развёртывании...\n",
		"Пробный запуск, ничего не меняется. План: удалить ВМ vm-1",
		"  проект:     ",
		"  удалить ВМ vm-1 и все данные на её дисках\n",
		"  освободить floating IP 203.0.113.7",
		"  удалить запись о развёртывании из локального state\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("preview lacks %q:\n%s", want, buf.String())
		}
	}
	assertVMStillInState(t, e)
}

// Labels of the preview line up in both languages.
func TestPreviewLabels_Aligned(t *testing.T) {
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		width := -1
		for _, l := range [][2]string{{"project", "проект"}, {"flavor", "flavor"}, {"zone", "зона"}, {"image", "образ"},
			{"disk", "диск"}, {"network", "сеть"}, {"public IP", "внешний IP"}, {"ports", "порты"}, {"cost", "цена"}} {
			s := previewLabel(l[0], l[1])
			n := len([]rune(s))
			if width >= 0 && n != width {
				t.Errorf("%s: label %q is %d wide, others %d", lang, s, n, width)
			}
			width = n
			if !strings.HasSuffix(s, " ") {
				t.Errorf("%s: label %q has no space before the value", lang, s)
			}
		}
	}
	i18n.SetLang(i18n.EN)
	if previewLabel("project", "проект") != "project:   " {
		t.Errorf("English label changed: %q", previewLabel("project", "проект"))
	}
}
