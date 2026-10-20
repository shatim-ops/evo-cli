// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/bootstrap"
)

// REL-6: ожидание cloud-init в скрипте установки — не больше 5 минут из 15
// общего ожидания Docker: иначе на зеркала и повторы остаётся треть срока.
func TestInstallScript_CloudInitWaitFitsDockerWait(t *testing.T) {
	m := regexp.MustCompile(`(?m)^cloud_init_wait=(\d+)$`).FindStringSubmatch(bootstrap.InstallScript())
	if m == nil {
		t.Fatal("cloud_init_wait not found in install.sh")
	}
	sec, _ := strconv.Atoi(m[1])
	wait := time.Duration(sec) * time.Second
	if wait != 5*time.Minute {
		t.Errorf("cloud_init_wait = %v, want 5m", wait)
	}
	if wait > dockerWaitTimeout/3 {
		t.Errorf("cloud_init_wait %v is more than a third of the Docker wait %v", wait, dockerWaitTimeout)
	}
}
