// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package images holds the rules for marketplace and OS images that every
// VM-creating path shares. It prints nothing and knows nothing of cobra or
// MCP: callers turn its results into previews, errors and requests.
package images

import (
	"fmt"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Field types of image_metadata as the catalogue declares them
// (compute openapi-v3, checked live 16.10.2026).
const (
	TypeLinuxLogin    = "linux_login"
	TypePublicKey     = "public_key"
	TypeLinuxPassword = "linux_password"
	TypePlainPassword = "plain_password"
	TypeHostname      = "hostname"
	TypeText          = "text"
	TypeFlag          = "flag"
	TypeInfo          = "info"
)

// statusFields are read-only VM status flags some images declare: sending
// them makes the create API answer 500, whatever their type or default.
var statusFields = map[string]bool{"vmm_enabled": true, "guest_agent_ready": true}

// IsStatusField reports whether a metadata field must never be sent.
func IsStatusField(name string) bool { return statusFields[name] }

// Inputs are what the caller knows before the VM exists.
type Inputs struct {
	// PublicKey goes into every public-key field; empty leaves them unset
	// (an empty public_key makes the create API answer 500).
	PublicKey string
	// Hostname goes into every hostname field (fqdn of RED OS 7.3 included).
	Hostname string
	// Login is the fallback for login fields that have no default (when no
	// other login field of the image declares one).
	Login string
	// GenPassword makes the random access password. Nil defers it: the
	// fields are listed in Result.PasswordFields and left out of meta, for a
	// caller that must not keep the value (a saved deploy plan).
	GenPassword func() string
	// KeyLater says the key is made after consent (the CLI deploy): with an
	// empty PublicKey the key fields count as filled for Missing but are
	// left out of meta, for the caller to fill.
	KeyLater bool
	// Values are explicit values by field name (--meta, image_metadata).
	// They win over everything evo fills itself and may set any field
	// except the status flags.
	Values map[string]string
}

// FieldRef names a field for a message or a preview.
type FieldRef struct {
	Name        string `json:"name"`
	FieldType   string `json:"field_type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// MissingGroup is a mandatory group no field of which got a value.
type MissingGroup struct {
	Group       string     `json:"group"`
	DisplayName string     `json:"display_name,omitempty"`
	Fields      []FieldRef `json:"fields"`
}

// Result says what FillMetadata did, without any secret value.
type Result struct {
	// Login is the value of the login field that was filled ("" when the
	// image declares none).
	Login string
	// PasswordFields are the access-password fields evo fills with a random
	// value: in meta when Inputs.GenPassword is set, otherwise left to the
	// caller.
	PasswordFields []string
	// KeyFields are the public-key fields evo fills with Inputs.PublicKey;
	// KeyUsed says whether the key was actually put in meta.
	KeyFields []string
	KeyUsed   bool
	// Missing are the mandatory groups still unsatisfied, in field order.
	Missing []MissingGroup
	// Sensitive are the fields sent (or to be sent) whose value is a secret.
	Sensitive []string
}

type kind int

const (
	kindOther kind = iota
	kindStatus
	kindKey
	kindHostname
	kindLogin
	kindPassword // the access password of the VM
)

// typedKinds are the field types that decide a field's role by themselves.
// Any other type (empty, text, an unknown one) falls back to the field name,
// as older images carry no field_type.
var typedKinds = map[string]kind{
	TypeLinuxLogin:    kindLogin,
	TypePublicKey:     kindKey,
	TypeLinuxPassword: kindPassword,
	TypeHostname:      kindHostname,
}

var nameKinds = map[string]kind{
	"ssh_key": kindKey, "public_key": kindKey, "ssh_public_key": kindKey,
	"hostname": kindHostname, "instance_name": kindHostname,
	"linux_password": kindPassword,
	"name":           kindLogin, "user": kindLogin, "username": kindLogin, "default_user": kindLogin,
}

// loginPreference is the order the SSH user is read in when an image has
// several login fields (Jaeger, Zabbix_Proxy: "name" and "linux_login").
var loginPreference = []string{"name", "user", "username", "default_user"}

// NormGroup is the mandatory group as it is compared: the catalogue has
// " authentication" with a leading space and mixed case.
func NormGroup(g string) string { return strings.ToLower(strings.TrimSpace(g)) }

func classify(fields []provision.ImageMetadataField) []kind {
	kinds := make([]kind, len(fields))
	hasAccess := false
	for i, f := range fields {
		switch {
		case statusFields[f.Name]:
			kinds[i] = kindStatus
			continue
		case f.FieldType == TypePlainPassword, f.FieldType == TypeFlag, f.FieldType == TypeInfo:
			// plain_password is decided below; flags and info are only
			// ever sent with their default or an explicit value.
		default:
			if k, ok := typedKinds[f.FieldType]; ok {
				kinds[i] = k
			} else {
				kinds[i] = nameKinds[f.Name]
			}
		}
		if kinds[i] == kindKey || kinds[i] == kindPassword {
			hasAccess = true
		}
	}
	// plain_password is the VM's access password only when it is named so,
	// or when the image has no other way in (Windows: login + one password).
	// Otherwise it is a product secret (OpensearchDashboards) that evo must
	// not invent: the user would never learn it.
	for i, f := range fields {
		if kinds[i] != kindOther || f.FieldType != TypePlainPassword {
			continue
		}
		if f.Name == "linux_password" || !hasAccess {
			kinds[i] = kindPassword
			hasAccess = true
		}
	}
	return kinds
}

func preferredLoginDefault(fields []provision.ImageMetadataField, kinds []kind) string {
	for _, n := range loginPreference {
		for i, f := range fields {
			if kinds[i] == kindLogin && f.Name == n && f.DefaultValue != "" {
				return f.DefaultValue
			}
		}
	}
	for i, f := range fields {
		if kinds[i] == kindLogin && f.DefaultValue != "" {
			return f.DefaultValue
		}
	}
	return ""
}

func hasDefault(f provision.ImageMetadataField) bool {
	return strings.TrimSpace(f.DefaultValue) != ""
}

// FillMetadata builds the image_metadata of a create-VM request from the
// image's declared fields. Roles come from field_type; the field name is the
// fallback for images without one. An explicit value wins; then evo's own
// (key, hostname, login, random password); then a real default. Status
// flags, and flags or info without a default, are never sent. Password
// values appear only in meta.
func FillMetadata(fields []provision.ImageMetadataField, in Inputs) (map[string]string, Result, error) {
	var res Result
	var password string // generated once, shared by every password field
	declared := make(map[string]bool, len(fields))
	for _, f := range fields {
		declared[f.Name] = true
	}
	userKeys := make([]string, 0, len(in.Values))
	for k := range in.Values {
		userKeys = append(userKeys, k)
	}
	sort.Strings(userKeys)
	for _, k := range userKeys {
		switch {
		case statusFields[k]:
			return nil, res, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("image metadata field %q is a read-only status flag and cannot be set", "поле метаданных образа %q — служебный флаг состояния, задавать его нельзя", k),
				fieldsHint(fields))
		case !declared[k]:
			return nil, res, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("the image declares no metadata field %q", "в образе нет поля метаданных %q", k),
				fieldsHint(fields))
		case strings.TrimSpace(in.Values[k]) == "":
			return nil, res, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("image metadata field %q: empty value", "поле метаданных образа %q: пустое значение", k),
				fieldsHint(fields))
		}
	}

	kinds := classify(fields)
	// A login field without a default takes the image's own login when
	// another login field declares one (two login fields must not name two
	// users), else the caller's fallback.
	login := in.Login
	if d := preferredLoginDefault(fields, kinds); d != "" {
		login = d
	}
	meta := map[string]string{}
	pending := map[string]bool{} // password (and KeyLater key) fields left to the caller
	loginFrom := map[string]bool{}
	for i, f := range fields {
		if kinds[i] == kindStatus {
			continue
		}
		if v, ok := in.Values[f.Name]; ok {
			meta[f.Name] = v
			if kinds[i] == kindLogin {
				loginFrom[f.Name] = true
			}
			continue
		}
		switch kinds[i] {
		case kindKey:
			res.KeyFields = append(res.KeyFields, f.Name)
			switch {
			case in.PublicKey != "":
				meta[f.Name] = in.PublicKey
				res.KeyUsed = true
			case in.KeyLater:
				pending[f.Name] = true
			}
		case kindHostname:
			if in.Hostname != "" {
				meta[f.Name] = in.Hostname
			}
		case kindLogin:
			v := f.DefaultValue
			if v == "" {
				v = login
			}
			if v != "" {
				meta[f.Name] = v
				loginFrom[f.Name] = true
			}
		case kindPassword:
			// Set even when the key already satisfies the group: without it
			// the VM never leaves "creating" (seen live 2026-10-14).
			// One password for every password field: the person gets one
			// password back, and it must open the VM whichever field the
			// image reads.
			res.PasswordFields = append(res.PasswordFields, f.Name)
			if in.GenPassword != nil {
				if password == "" {
					password = in.GenPassword()
				}
				meta[f.Name] = password
			} else {
				pending[f.Name] = true
			}
		}
	}

	filled := func(name string) bool { _, ok := meta[name]; return ok || pending[name] }
	satisfied := map[string]bool{}
	for _, f := range fields {
		if g := NormGroup(f.MandatoryGroup); g != "" && filled(f.Name) {
			satisfied[g] = true
		}
	}
	// A default only where nothing else filled the field and its group is
	// not already covered (no stray value next to the key and password).
	for i, f := range fields {
		if kinds[i] == kindStatus || filled(f.Name) || !hasDefault(f) {
			continue
		}
		if g := NormGroup(f.MandatoryGroup); g != "" && satisfied[g] {
			continue
		}
		meta[f.Name] = f.DefaultValue
	}

	for _, n := range loginPreference {
		if loginFrom[n] {
			res.Login = meta[n]
			break
		}
	}
	if res.Login == "" {
		for _, f := range fields {
			if loginFrom[f.Name] {
				res.Login = meta[f.Name]
				break
			}
		}
	}

	groupIdx := map[string]int{}
	for i, f := range fields {
		if filled(f.Name) && (f.Sensitive || f.FieldType == TypeLinuxPassword || f.FieldType == TypePlainPassword || kinds[i] == kindPassword) {
			res.Sensitive = append(res.Sensitive, f.Name)
		}
		g := NormGroup(f.MandatoryGroup)
		if g == "" || kinds[i] == kindStatus {
			continue
		}
		if filled(f.Name) {
			satisfied[g] = true
		}
		idx, ok := groupIdx[g]
		if !ok {
			idx = len(res.Missing)
			groupIdx[g] = idx
			res.Missing = append(res.Missing, MissingGroup{
				Group:       strings.TrimSpace(f.MandatoryGroup),
				DisplayName: strings.TrimSpace(f.MandatoryGroupDisplayName),
			})
		}
		m := &res.Missing[idx]
		if m.DisplayName == "" {
			m.DisplayName = strings.TrimSpace(f.MandatoryGroupDisplayName)
		}
		m.Fields = append(m.Fields, FieldRef{Name: f.Name, FieldType: f.FieldType, DisplayName: f.DisplayName})
	}
	missing := res.Missing[:0]
	for _, m := range res.Missing {
		if !satisfied[NormGroup(m.Group)] {
			missing = append(missing, m)
		}
	}
	res.Missing = missing
	if len(res.Missing) == 0 {
		res.Missing = nil
	}
	return meta, res, nil
}

// settable are the fields a caller may give a value: all but the status
// flags.
func settable(fields []provision.ImageMetadataField) []string {
	var out []string
	for _, f := range fields {
		if !statusFields[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out
}

func fieldsHint(fields []provision.ImageMetadataField) string {
	names := settable(fields)
	if len(names) == 0 {
		return i18n.T("The image has no metadata fields to set.", "У образа нет полей метаданных, которые можно задать.")
	}
	return i18n.Tf("Fields of the image: %s.", "Поля образа: %s.", strings.Join(names, ", "))
}

// MissingError is the refusal to create a VM whose image has mandatory
// groups no value was found for, before anything is created. code is USAGE
// for a call (--meta, image_metadata) or CONFIG_INVALID for a project file;
// howTo shows how to pass one value and is formatted with the first field
// name ("--meta %s=<value>").
func MissingError(code string, missing []MissingGroup, howTo string) error {
	groups := make([]string, 0, len(missing))
	first := ""
	for _, m := range missing {
		names := make([]string, 0, len(m.Fields))
		for _, f := range m.Fields {
			names = append(names, f.Name)
			if first == "" {
				first = f.Name
			}
		}
		label := m.Group
		if m.DisplayName != "" && m.DisplayName != m.Group {
			label += " (" + m.DisplayName + ")"
		}
		groups = append(groups, label+": "+strings.Join(names, " | "))
	}
	return clierrors.New(code,
		i18n.Tf("the image needs values evo cannot fill: %s", "образу нужны значения, которые evo не заполнит сам: %s", strings.Join(groups, "; ")),
		i18n.Tf("Give one field of each group (a | b: either): %s. The fields: evo image get <image>.",
			"Задайте по одному полю из каждой группы (a | b — любое из них): %s. Поля образа: evo image get <образ>.",
			fmt.Sprintf(howTo, first)))
}
