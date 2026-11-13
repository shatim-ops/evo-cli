// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package floatingip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one point of the rule (У4 of «Риск», conclusion 180): no product
// code but Reserve sends the create of a floating IP — provision, deploy,
// deploy_apply and the re-run of deploy reach it through Reserve, and a
// new path that called the cloud itself would reserve a second address
// after a lost answer. The paths that use it are named here: a path gone
// from the list is a reason to read it again.
func TestCreateFloatingIP_OnlyThroughReserve(t *testing.T) {
	root := filepath.Join("..")
	var outside []string
	users := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.Contains(src, ".CreateFloatingIP(") && rel != "floatingip/floatingip.go" {
			outside = append(outside, rel)
		}
		if strings.Contains(src, "floatingip.Reserve(") {
			users[rel] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outside) > 0 {
		t.Errorf("CreateFloatingIP is called outside floatingip.Reserve: %v", outside)
	}
	for _, path := range []string{"engine/deploy.go", "deployplan/apply.go"} {
		if !users[path] {
			t.Errorf("%s no longer reserves its address through floatingip.Reserve", path)
		}
	}
}
