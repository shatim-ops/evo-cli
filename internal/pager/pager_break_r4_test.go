// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import "testing"

// Повтор токена обрывает список. Это не конец выборки: ошибка, и запросов
// конечное число.
func TestBreakR4_R4_6_RepeatedTokenIsError(t *testing.T) {
	n := 0
	more, err := Upto(10, func(token string) (string, error) {
		n++
		return "page-a", nil
	})
	if err == nil {
		t.Fatalf("repeated token ended the list: more=%v after %d fetches", more, n)
	}
	if n > 3 {
		t.Fatalf("fetches = %d, the walk is not bounded", n)
	}
}
