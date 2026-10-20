// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// MaxNameLen is the longest name of a VM, a disk or a floating IP
// (maxLength 64 in the compute spec: PublicDiskCreateRequest,
// PublicFloatingIpCreateRequest, VmCreateNewDiskItem).
const MaxNameLen = 64

// nameRe is the compute name pattern (PublicVmCreateRequest,
// PublicDiskCreateRequest, PublicNATGatewayCreateRequest, …).
var nameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.\-_]*$`)

// ValidName refuses a VM, disk, image or gateway name the compute API
// rejects with 422 — before anything billable is made: a boot floating IP
// created first and released after the 422 can stick in error_deleting
// (release QA, C-api 4; register NET-7).
func ValidName(kind, name string) error {
	if len(name) <= MaxNameLen && nameRe.MatchString(name) {
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s name %q is not accepted by Cloud.ru", "Cloud.ru не примет имя %s %q", kind, name),
		i18n.Tf("Use 1-%d characters: a Latin letter first, then Latin letters, digits, '.', '-', '_'.",
			"Допустимо 1–%d символов: первая — латинская буква, дальше латинские буквы, цифры, '.', '-', '_'.", MaxNameLen))
}

// FitName is base+suffix within MaxNameLen: a long base is cut and gets a
// short hash of itself, so two long names that differ only at the end do
// not become one name. The suffix ("-boot") is kept whole.
func FitName(base, suffix string) string {
	return fitBase(base, len(suffix)) + suffix
}

// fitBase cuts base so that reserve more characters still fit.
func fitBase(base string, reserve int) string {
	if len(base)+reserve <= MaxNameLen {
		return base
	}
	sum := sha256.Sum256([]byte(base))
	h := hex.EncodeToString(sum[:3])
	cut := MaxNameLen - reserve - len(h) - 1
	if cut < 1 {
		cut = 1
	}
	return strings.TrimRight(base[:cut], "-._") + "-" + h
}

// bootFIPStampLen bounds the base-36 Unix time of BootFIPName (7 digits
// last until the year 4400).
const bootFIPStampLen = 7

// BootFIPName is the name of the floating IP made for VM vmName at t:
// BootFIPPrefix(vmName) and the base-36 Unix time, within MaxNameLen.
func BootFIPName(vmName string, t time.Time) string {
	return BootFIPPrefix(vmName) + strconv.FormatInt(t.Unix(), 36)
}

// BootFIPPrefix is what every BootFIPName of vmName starts with: destroy
// finds the VM's floating IP by it.
func BootFIPPrefix(vmName string) string {
	return fitBase("fip-"+vmName, bootFIPStampLen+1) + "-"
}

// IsBootFIPOf reports whether name is a BootFIPName of vmName: the prefix
// and only the base-36 time after it. A prefix alone also matches the FIP
// of VM "<vmName>-2" ("fip-web-" starts "fip-web-2-…"), which left the FIP
// of "web" unfound and billed when both existed (ревью Fable, A6).
func IsBootFIPOf(name, vmName string) bool {
	rest, ok := strings.CutPrefix(name, BootFIPPrefix(vmName))
	if !ok || rest == "" || len(rest) > bootFIPStampLen {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// IsEvoFIPOf reports a floating IP evo made for the VM: deploy's
// "fip-<vm id>" after boot, provision's and deploy's BootFIPName, and
// deploy_apply's "<vm name>-fip". destroy releases an address it finds only
// on the VM when it is one of these: another address on the VM may be one
// the user reserved (release QA, B-lifecycle 5).
func IsEvoFIPOf(fipName, vmID, vmName string) bool {
	if fipName == "" {
		return false
	}
	if vmID != "" && fipName == "fip-"+vmID {
		return true
	}
	return vmName != "" && (IsBootFIPOf(fipName, vmName) || fipName == FitName(vmName, "-fip"))
}
