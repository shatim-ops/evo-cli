// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// имитацияОблака подменяет только выборку образов. Интерфейс CloudAPI широкий,
// и встраивание его значением — способ удовлетворить компилятор, не описывая
// три десятка ненужных методов: вызов любого из них уронит тест паникой, что
// здесь и требуется — он проверяет ровно одно правило.
type имитацияОблака struct {
	provision.CloudAPI
	общие      []*provision.Image
	проектные  []*provision.Image
	ошибка     error
	спрошенПро string
}

func (м *имитацияОблака) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return м.общие, nil
}

func (м *имитацияОблака) ListImagesInProject(_ context.Context, projectID string) ([]*provision.Image, error) {
	м.спрошенПро = projectID
	if м.ошибка != nil {
		return nil, м.ошибка
	}
	return м.проектные, nil
}

// Образ, собранный внутри проекта, обязан находиться по имени и по UUID.
//
// Голый /api/v1/images возвращает только общий каталог, и всё, что произвёл
// image_create, там невидимо. Пока выборка смотрела только туда, движок не мог
// развернуть образ, который сам же и собрал: он отвечал «not found» про образ,
// который list_images показывает в том же сеансе. Найдено живым прогоном — ВМ
// из готового образа OpenClaw создать не удалось.
func TestОбразПроектаНаходится(t *testing.T) {
	облако := &имитацияОблака{
		общие: []*provision.Image{
			{ID: "474c9e98-0000-0000-0000-000000000001", Name: "ubuntu-22.04"},
		},
		проектные: []*provision.Image{
			{ID: "e92e877c-0000-0000-0000-000000000002", Name: "openclaw-2026.8.1"},
		},
	}

	for _, запрос := range []string{"openclaw-2026.8.1", "e92e877c-0000-0000-0000-000000000002"} {
		img, err := selectImage(context.Background(), облако, "проект-1", запрос)
		if err != nil {
			t.Fatalf("по запросу %q образ не нашёлся: %v", запрос, err)
		}
		if img.Name != "openclaw-2026.8.1" {
			t.Errorf("по запросу %q вернулся %q", запрос, img.Name)
		}
	}
	if облако.спрошенПро != "проект-1" {
		t.Errorf("в проектную выборку ушёл проект %q", облако.спрошенПро)
	}
}

// Общий каталог по-прежнему в приоритете и проверяется первым.
func TestОбщийКаталогПроверяетсяПервым(t *testing.T) {
	облако := &имитацияОблака{
		общие:     []*provision.Image{{ID: "id-общий", Name: "ubuntu-22.04"}},
		проектные: []*provision.Image{{ID: "id-проектный", Name: "ubuntu-22.04"}},
	}
	img, err := selectImage(context.Background(), облако, "проект-1", "ubuntu-22.04")
	if err != nil {
		t.Fatal(err)
	}
	if img.ID != "id-общий" {
		t.Errorf("выбран %q, а общий каталог должен выигрывать", img.ID)
	}
}

// Нет права читать образы проекта — не повод врать про причину.
//
// Честный ответ в этом случае «такого образа нет», а не «не удалось прочитать
// проект»: с точки зрения вызывающего исход тот же, а вторая формулировка
// отправляет чинить права там, где, возможно, просто опечатка в имени.
func TestОтказВПравахНеМешаетОтвету(t *testing.T) {
	облако := &имитацияОблака{
		общие:  []*provision.Image{{ID: "id-1", Name: "ubuntu-22.04"}},
		ошибка: context.DeadlineExceeded,
	}
	_, err := selectImage(context.Background(), облако, "проект-1", "нет-такого")
	if err == nil {
		t.Fatal("ожидалась ошибка «образ не найден»")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("ошибка про другое: %v", err)
	}
}

// Выбор по умолчанию НЕ должен угадывать проектный образ: он подбирает базовую
// ОС, и продуктовый образ проекта там оказаться не может.
func TestПоУмолчаниюБерётсяТолькоОбщийКаталог(t *testing.T) {
	облако := &имитацияОблака{
		общие:     []*provision.Image{{ID: "id-общий", Name: "ubuntu-24.04"}},
		проектные: []*provision.Image{{ID: "id-проектный", Name: "openclaw-2026.8.1"}},
	}
	img, err := selectImage(context.Background(), облако, "проект-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if img.ID != "id-общий" {
		t.Errorf("по умолчанию выбран %q — продуктовый образ проекта угадывать нельзя", img.ID)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*имитацияОблака) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
