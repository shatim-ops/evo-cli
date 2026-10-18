// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package i18n

import (
	"sync"
	"testing"
)

func TestSetLang(t *testing.T) {
	defer SetLang(Lang())
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", RU},
		{"ru", RU},
		{"ru_RU.UTF-8", RU},
		{"de", RU},
		{"english", RU},
		{"en", EN},
		{"EN", EN},
		{" en ", EN},
		{"en_US", EN},
		{"en-GB", EN},
		{"en_US.UTF-8", EN},
	} {
		SetLang(tc.in)
		if got := Lang(); got != tc.want {
			t.Errorf("SetLang(%q): Lang() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestT(t *testing.T) {
	defer SetLang(Lang())
	SetLang("en")
	if got := T("yes", "да"); got != "yes" {
		t.Errorf("en: %q", got)
	}
	if got := Tf("%d files", "файлов: %d", 3); got != "3 files" {
		t.Errorf("en: %q", got)
	}
	SetLang("ru")
	if got := T("yes", "да"); got != "да" {
		t.Errorf("ru: %q", got)
	}
	if got := Tf("%d files", "файлов: %d", 3); got != "файлов: 3" {
		t.Errorf("ru: %q", got)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{
		0: "проектов", 1: "проект", 2: "проекта", 4: "проекта", 5: "проектов", 11: "проектов",
		12: "проектов", 14: "проектов", 21: "проект", 22: "проекта", 25: "проектов", 111: "проектов", 101: "проект",
	} {
		if got := Plural(n, "проект", "проекта", "проектов"); got != want {
			t.Errorf("Plural(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestConcurrent runs under -race: SetLang and T from many goroutines.
func TestConcurrent(t *testing.T) {
	defer SetLang(Lang())
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if i%2 == 0 {
					SetLang("en")
				} else {
					SetLang("ru")
				}
				_ = T("a", "б")
			}
		}()
	}
	wg.Wait()
}

func TestUnsupported(t *testing.T) {
	for lang, want := range map[string]bool{
		"": false, "en": false, "en_US.UTF-8": false, "ru": false, "RU_ru": false,
		"fr": true, "de_DE": true, "english": true,
	} {
		if got := Unsupported(lang); got != want {
			t.Errorf("Unsupported(%q) = %v, want %v", lang, got, want)
		}
	}
}
