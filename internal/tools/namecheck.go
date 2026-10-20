// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// takenName finds a listed resource (cluster, registry) named name that is
// not gone. kafka_create with a name in use made a second billable cluster
// of the same name (rc 0 "creating"), which the API would not even delete
// while it was DEPLOYING (release audit, L-32); ar_create did the same with
// a registry (L-18). Names in Cloud.ru are not unique, so the create tools
// check the list themselves. Returns the id and status of the first match.
func takenName(items []map[string]any, name string) (id, status string, ok bool) {
	for _, m := range items {
		if n, _ := m["name"].(string); n != name {
			continue
		}
		st, _ := m["status"].(string)
		if st == "" {
			st, _ = m["state"].(string)
		}
		switch strings.ToUpper(st) {
		case "DELETED", "ERASED":
			continue // gone: the name is free again
		}
		id, _ = m["id"].(string)
		return id, st, true
	}
	return "", "", false
}

// nameTakenResult is the answer of a create tool whose name is in use: USAGE
// (the call is wrong, nothing was sent to create anything) with the existing
// id in existing_id, so a stack step knows the resource is not its own.
func nameTakenResult(kind, name, id, status, getHint string) (string, bool) {
	if status == "" {
		status = "unknown"
	}
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a %s named %q already exists (id %s, status %s); a second one would be billed separately", "%s с именем %q уже есть (id %s, статус %s); второй оплачивался бы отдельно", kind, name, id, status),
		i18n.Tf("Use the existing one (%s), or pick another name.", "Используйте существующий (%s) или выберите другое имя.", getHint)),
		map[string]any{"existing_id": id}), true
}

// uuidArg refuses an id the API takes only as a UUID before the call: the
// API answered a malformed id with a 400 that read as API_ERROR, exit 5
// (release audit, L-15). Returns "" when id is a UUID.
func uuidArg(arg, id, from string) string {
	if isUUID(id) {
		return ""
	}
	return errResult(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("'%s' must be a UUID, got %q", "'%s' должен быть UUID, передано %q", arg, id),
		i18n.Tf("Take the id from %s.", "id — в выводе %s.", from)))
}

// needSpecResult is the answer of pg_create, kafka_create and redis_create
// when no specification matches the call: an error (USAGE) with the menu to
// choose from. It was status need_specification with rc 0, even with --yes:
// a create that created nothing read as success (release audit, L-30).
// fields carries the menu and what was resolved (version, zone).
func needSpecResult(specsTool, specID string, cpu, ram int, fields map[string]any) (string, bool) {
	msg := i18n.T("a specification is needed: pass specification_id, or cpu+ram matching one of available_specifications",
		"нужна конфигурация: укажите specification_id или cpu+ram одной из available_specifications")
	switch {
	case specID != "":
		msg = i18n.Tf("specification_id %q is not one of available_specifications",
			"specification_id %q нет среди available_specifications", specID)
	case cpu > 0 && ram > 0:
		msg = i18n.Tf("no specification with cpu=%d and ram=%d; pick one of available_specifications",
			"нет конфигурации с cpu=%d и ram=%d; выберите одну из available_specifications", cpu, ram)
	}
	fields["status"] = "need_specification"
	return errAnswer(clierrors.New(clierrors.CodeUsage, msg,
		i18n.Tf("Pass specification_id (or cpu+ram) of one of available_specifications; %s lists them all.",
			"Укажите specification_id (или cpu+ram) одной из available_specifications; полный список — %s.", specsTool)), fields), true
}
