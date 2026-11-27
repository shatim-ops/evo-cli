// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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

// A failed project catalogue read cannot establish absence. Preserve the
// original error, including the service status used by CLI and MCP.
func TestSelectImage_ExplicitProjectReadPreservesFailure(t *testing.T) {
	apiFailure := func(code string, status int) error {
		return clierrors.Wrap(code, "catalogue read failed", "catalogue hint", &provision.StatusError{Status: status})
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"forbidden", apiFailure(clierrors.CodeAuthFailed, http.StatusForbidden)},
		{"unauthorized", apiFailure(clierrors.CodeAuthExpired, http.StatusUnauthorized)},
		{"rate_limited", apiFailure(clierrors.CodeAPIRateLimited, http.StatusTooManyRequests)},
		{"service_failure", apiFailure(clierrors.CodeAPIError, http.StatusBadGateway)},
		{"gateway_route_404", clierrors.Wrap(clierrors.CodeAPIError, "route unavailable", clierrors.RouteNotFoundHint(),
			&provision.StatusError{Status: http.StatusNotFound})},
		{"deadline", context.DeadlineExceeded},
		{"cancelled", context.Canceled},
		{"incomplete_page", clierrors.New(clierrors.CodeAPIError, "page incomplete", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &имитацияОблака{ошибка: tc.err}
			img, err := selectImage(context.Background(), cloud, "project-for-test", "requested-image")
			if err != tc.err || img.ID != "" || cloud.спрошенПро != "project-for-test" {
				t.Fatalf("image %q, error %v (want original %v), project %q", img.ID, err, tc.err, cloud.спрошенПро)
			}
		})
	}
}

func TestSelectImage_ExplicitImageMissingAndMaskedHaveSameAnswer(t *testing.T) {
	const hidden = "hidden-catalogue-detail"
	const project = "project-for-test"
	lookup := func(readErr error) *clierrors.CLIError {
		t.Helper()
		_, err := selectImage(context.Background(), &имитацияОблака{ошибка: readErr}, project, "requested-image")
		var failure *clierrors.CLIError
		if !errors.As(err, &failure) || failure.Code != clierrors.CodeNotFound || clierrors.ExitCode(err) != clierrors.ExitNotFound {
			t.Fatalf("error %v, want NOT_FOUND / 4", err)
		}
		return failure
	}
	absent := lookup(nil)
	for _, masked := range []error{
		clierrors.New(clierrors.CodeNotFound, hidden, hidden),
		&provision.StatusError{Status: http.StatusNotFound, Body: `{"message":"` + hidden + `"}`},
	} {
		got := lookup(masked)
		if !reflect.DeepEqual(got, absent) || got.Err != nil || strings.Contains(got.Error()+got.Hint, project) || strings.Contains(got.Error()+got.Hint, hidden) {
			t.Fatalf("masked catalogue returned %+v, completed search returned %+v", got, absent)
		}
	}
}

func TestSelectImage_ExplicitProjectRowsMustIdentifyAnImage(t *testing.T) {
	for _, bad := range []*provision.Image{nil, {}, {Name: "without-id"}, {ID: "without-name"}} {
		cloud := &имитацияОблака{проектные: []*provision.Image{{ID: "id-matched", Name: "requested-image"}, bad}}
		img, err := selectImage(context.Background(), cloud, "project-for-test", "requested-image")
		if clierrors.Reason(err) != clierrors.CodeAPIError || img.ID != "" {
			t.Fatalf("catalogue with row %+v returned %q, %v", bad, img.ID, err)
		}
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
