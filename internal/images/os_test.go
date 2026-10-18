// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func TestNoDockerFamily(t *testing.T) {
	for in, want := range map[string]string{
		"Astra Linux SE 1.8":         "Astra Linux",
		"AstraLinuxSmolensk1.8.3KES": "Astra Linux",
		"alse-base-1-7-7":            "",
		"RED OS 7.3":                 "RED OS",
		"redos-7.3":                  "RED OS",
		"РЕД ОС 8":                   "RED OS",
		"Alt Linux p10":              "Alt Linux",
		"alt-server-10":              "Alt Linux",
		"Альт Сервер 10":             "Alt Linux",
		"Ubuntu-24.04":               "",
		"debian-12":                  "",
		"Shared OS image":            "",
		"Altair Studio":              "",
		"Альтернатива":               "",
	} {
		if got := NoDockerFamily(in); got != want {
			t.Errorf("NoDockerFamily(%q) = %q, ждали %q", in, got, want)
		}
	}
	// Любое из полей: имя, отображаемое имя, os.
	if NoDockerFamily("server-10", "", "altlinux") != "Alt Linux" {
		t.Error("os не учтено")
	}
}

// KeyLater: ключ сделают после согласия — его группа не недостающая, а
// в meta ключа нет.
func TestFillMetadata_KeyLater(t *testing.T) {
	fields := []provision.ImageMetadataField{
		{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication"},
		{Name: "product_token", FieldType: TypeText, MandatoryGroup: "product", MandatoryGroupDisplayName: "Продукт"},
	}
	meta, res, err := FillMetadata(fields, Inputs{KeyLater: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["public_key"]; ok || len(res.KeyFields) != 1 || res.KeyUsed {
		t.Errorf("meta %v, res %+v", meta, res)
	}
	if len(res.Missing) != 1 || res.Missing[0].Group != "product" {
		t.Fatalf("Missing = %+v", res.Missing)
	}
	err = MissingError(clierrors.CodeUsage, res.Missing, "--meta %s=<value>")
	ce, ok := err.(*clierrors.CLIError)
	if !ok || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Message, "product (Продукт): product_token") ||
		!strings.Contains(ce.Hint, "--meta product_token=<value>") {
		t.Errorf("MissingError: %#v", err)
	}
}
