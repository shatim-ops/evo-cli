// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"strings"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Who fills a metadata field when evo creates a VM from the image.
const (
	// FilledByEvo: evo sets it itself (SSH key, VM name, login, random
	// password).
	FilledByEvo = "evo"
	// FilledByDefault: the image's default value is sent.
	FilledByDefault = "default"
	// FilledByUser: a mandatory group no field of which evo can fill — the
	// person has to give a value.
	FilledByUser = "user"
	// FilledByNone: optional and left out (no default, not mandatory).
	FilledByNone = "none"
	// FilledByStatus: a read-only status flag, never sent.
	FilledByStatus = "status"
)

// What evo puts into a field it fills itself.
const (
	RoleKey      = "ssh_key"
	RoleHostname = "vm_name"
	RoleLogin    = "login"
	RolePassword = "password"
)

// FieldInfo is one metadata field of an image as a card shows it. It holds
// no secret: a default value of a sensitive field is left out.
type FieldInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	FieldType   string `json:"field_type,omitempty"`
	// Group is the mandatory group (trimmed); a field with one is required
	// through it.
	Group     string `json:"mandatory_group,omitempty"`
	Required  bool   `json:"required"`
	Sensitive bool   `json:"sensitive,omitempty"`
	// Default is the image's default value (empty for a sensitive field).
	Default  string `json:"default_value,omitempty"`
	FilledBy string `json:"filled_by"`
	// Role says what evo puts there when FilledBy is "evo".
	Role string `json:"role,omitempty"`
}

// Placeholders FillMetadata is run with to see which fields evo fills;
// they never leave this function.
const (
	probeKey      = "\x00probe-key"
	probeHostname = "\x00probe-hostname"
	probeLogin    = "\x00probe-login"
)

// Fields classifies the image's metadata fields by who fills them, running
// FillMetadata — the rule VM creation uses — with placeholder inputs.
func Fields(fields []provision.ImageMetadataField) []FieldInfo {
	meta, res, err := FillMetadata(fields, Inputs{PublicKey: probeKey, Hostname: probeHostname, Login: probeLogin})
	if err != nil { // only explicit values can fail, and there are none
		meta = map[string]string{}
	}
	password := map[string]bool{}
	for _, n := range res.PasswordFields {
		password[n] = true
	}
	missing := map[string]bool{}
	for _, g := range res.Missing {
		for _, f := range g.Fields {
			missing[f.Name] = true
		}
	}
	out := make([]FieldInfo, 0, len(fields))
	for _, f := range fields {
		info := FieldInfo{
			Name: f.Name, DisplayName: f.DisplayName, FieldType: f.FieldType,
			Group: trimGroup(f.MandatoryGroup), Sensitive: f.Sensitive,
		}
		info.Required = info.Group != ""
		sensitive := f.Sensitive || f.FieldType == TypeLinuxPassword || f.FieldType == TypePlainPassword
		if !sensitive {
			info.Default = f.DefaultValue
		}
		v, sent := meta[f.Name]
		switch {
		case IsStatusField(f.Name):
			info.FilledBy = FilledByStatus
		case password[f.Name]:
			info.FilledBy, info.Role = FilledByEvo, RolePassword
		case sent && v == probeKey:
			info.FilledBy, info.Role = FilledByEvo, RoleKey
		case sent && v == probeHostname:
			info.FilledBy, info.Role = FilledByEvo, RoleHostname
		case sent && v == probeLogin:
			info.FilledBy, info.Role = FilledByEvo, RoleLogin
		case sent:
			info.FilledBy = FilledByDefault
		case missing[f.Name]:
			info.FilledBy = FilledByUser
		default:
			info.FilledBy = FilledByNone
		}
		out = append(out, info)
	}
	return out
}

// NeedsUser lists the fields the person must fill: every field of a
// mandatory group evo cannot satisfy.
func NeedsUser(fields []FieldInfo) []string {
	var out []string
	for _, f := range fields {
		if f.FilledBy == FilledByUser {
			out = append(out, f.Name)
		}
	}
	return out
}

// trimGroup is the group as declared, without the catalogue's stray
// spaces (NormGroup also lowers the case, for comparing).
func trimGroup(g string) string { return strings.TrimSpace(g) }
