// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

// allowInternetArg is the person's word for a rule that opens a shared
// group to the whole internet (D105): one argument for every tool the rule
// applies to.
const allowInternetArg = "allow_internet_on_shared_group"

// allowInternetDesc describes allowInternetArg in the schema of each tool
// that has it.
const allowInternetDesc = "true to open to the whole internet on a shared group: the person decides, not the model."

// sgGroupUse reads who a rule of group acts on: the interfaces of the
// project that are in it. A list that cannot be read is said as such, and
// the group is judged on the careful side (sgrules.Use).
func sgGroupUse(ctx context.Context, eng *engine.Engine, group *provision.SecurityGroup) sgrules.GroupUse {
	interfaces, err := eng.Cloud.ListInterfaces(ctx, eng.Config.ProjectID)
	return sgrules.Use(group, interfaces, err == nil)
}

// sgUseFields puts the use of the group into a preview: whether it is
// shared, and how many interfaces of which VMs a rule of it acts on.
func sgUseFields(preview map[string]any, u sgrules.GroupUse) {
	preview["shared"] = u.Shared
	if !u.Counted {
		preview["interfaces_unread"] = i18n.T("evo could not read how many interfaces are in the group",
			"сколько интерфейсов в группе, evo не узнал")
		return
	}
	preview["interfaces"] = u.Interfaces
	if len(u.VMs) > 0 {
		preview["vms"] = u.VMs
	}
	if u.MoreVMs > 0 {
		preview["more_vms"] = u.MoreVMs
	}
}

// sgUseWords says the use of a group in a sentence of a refusal: why it is
// shared, and who a rule of it acts on now.
func sgUseWords(group *provision.SecurityGroup, u sgrules.GroupUse) string {
	why := i18n.T("more than one interface is in it", "в ней больше одного интерфейса")
	if group.Default {
		why = i18n.T("it is the zone's default group, every VM placed in the zone gets it", "это группа зоны по умолчанию, её получает каждая ВМ зоны")
	}
	if !u.Counted {
		return why + i18n.T("; how many interfaces are in it now evo could not read", "; сколько в ней интерфейсов сейчас, evo не узнал")
	}
	return why + i18n.Tf("; a rule of it acts on %d interface(s) of %d VM(s) now", "; её правило сейчас действует на интерфейсы (%d) ВМ (%d)",
		u.Interfaces, len(u.VMs)+u.MoreVMs)
}

// internetOnSharedGroup is the one decision of D105 for a tool that adds
// an ingress rule to an existing group: a source that is the internet
// (sgrules.Internet), on a group that is shared (sgrules.Use), without the
// person's word for it (allowInternetArg) is refused before the question.
// what is the ports and the protocol in words. nil: the rule does not
// apply, or the word was given — the preview then says «open to the
// internet» and asks.
func internetOnSharedGroup(group *provision.SecurityGroup, u sgrules.GroupUse, what, source string, allowed bool) error {
	if allowed || !u.Shared || !sgrules.Internet(source) {
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("this would open %s to the whole internet (%s) on security group %s, which is shared: %s",
			"это откроет %s всему интернету (%s) в группе безопасности %s, а она общая: %s",
			what, source, quoteCloud(group.Name), sgUseWords(group, u)),
		i18n.Tf("Ask the person. Narrow cidr to the addresses that need the port; or, when the person says the whole internet is meant, call again with %s=true — the preview will say it and ask.",
			"Спросите человека. Сузьте cidr до адресов, которым нужен порт; либо, если человек подтверждает, что нужен весь интернет, повторите вызов с %s=true — превью скажет это и спросит.",
			allowInternetArg))
}

// portsWords is ports and their protocol in words: «80,443/tcp».
func portsWords(ports []string, proto string) string {
	return strings.Join(ports, ",") + "/" + proto
}

// upperFirst is s with its first letter in upper case: a line of a
// preview put at the start of a message.
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
