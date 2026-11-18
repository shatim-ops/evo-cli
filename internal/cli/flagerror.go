// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	stderrors "errors"
	"strings"

	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// flagParseError is a refusal of the command line's flags worded by evo:
// it names a flag evo knows and the place of the argument, never the text
// a person typed — a value, or what pflag took for a flag's name, may be a
// secret given in the wrong place (#2343, as #2333 and #2341 for
// arguments).
type flagParseError struct{ msg string }

func (e *flagParseError) Error() string { return e.msg }

// flagError words err, a parsing error of pflag (or of leadingUnknownFlag),
// by the flag and its place among args; any other error is returned as it
// is. The one place these refusals are worded.
func flagError(err error, args []string) error {
	var fe *flagParseError
	if stderrors.As(err, &fe) {
		return fe
	}
	var notExist *pflag.NotExistError
	var required *pflag.ValueRequiredError
	var invalid *pflag.InvalidValueError
	var syntax *pflag.InvalidSyntaxError
	switch {
	case stderrors.As(err, &notExist):
		if short := notExist.GetSpecifiedShortnames(); short != "" {
			// short is the rest of the group from the unknown letter on:
			// -yQ… gives Q….
			return unknownShorthandError(notExist.GetSpecifiedName(), placeOf(args, func(a string) bool {
				return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.HasSuffix(a, short)
			}))
		}
		name := notExist.GetSpecifiedName()
		return unknownFlagError(placeOf(args, func(a string) bool { return a == "--"+name || strings.HasPrefix(a, "--"+name+"=") }))
	case stderrors.As(err, &required):
		f := required.GetFlag()
		return &flagParseError{at(i18n.Tf("flag --%s needs a value", "флагу --%s нужно значение", f.Name), placeOfFlag(args, f))}
	case stderrors.As(err, &invalid):
		f := invalid.GetFlag()
		return &flagParseError{at(i18n.Tf("flag --%s: the value is not %s", "флаг --%s: значение — не %s", f.Name, flagKind(f)), placeOfFlag(args, f))}
	case stderrors.As(err, &syntax):
		spec := syntax.GetSpecifiedFlag()
		return &flagParseError{at(i18n.T("bad flag syntax", "неверная запись флага"), placeOf(args, func(a string) bool { return a == spec }))}
	}
	return err
}

func unknownFlagError(place int) error {
	return &flagParseError{at(i18n.T("unknown flag", "неизвестный флаг"), place)}
}

func unknownShorthandError(letter string, place int) error {
	return &flagParseError{at(i18n.Tf("unknown shorthand flag -%s", "неизвестный флаг -%s", letter), place)}
}

// at adds the place of the argument, when it is known.
func at(msg string, place int) string {
	if place == 0 {
		return msg
	}
	return msg + i18n.Tf(" (argument %d)", " (аргумент %d)", place)
}

// placeOf is the place, from 1, of the first argument before "--" that
// match accepts; 0 when none does.
func placeOf(args []string, match func(string) bool) int {
	for i, a := range args {
		if a == "--" {
			break
		}
		if match(a) {
			return i + 1
		}
	}
	return 0
}

// placeOfFlag is the place of the argument that names f: --name,
// --name=…, or a group of shorthands with its letter.
func placeOfFlag(args []string, f *pflag.Flag) int {
	return placeOf(args, func(a string) bool {
		if a == "--"+f.Name || strings.HasPrefix(a, "--"+f.Name+"=") {
			return true
		}
		return f.Shorthand != "" && len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.Contains(a[1:], f.Shorthand)
	})
}

// flagKind names what a value of f must be, by its type, not by the value.
func flagKind(f *pflag.Flag) string {
	switch t := f.Value.Type(); {
	case t == "bool":
		return i18n.T("true or false", "true или false")
	case strings.HasPrefix(t, "int") || strings.HasPrefix(t, "uint") || strings.HasPrefix(t, "float"):
		return i18n.T("a number", "число")
	case t == "duration":
		return i18n.T("a duration such as 30s or 5m", "длительность вида 30s или 5m")
	default:
		return i18n.Tf("of type %s", "типа %s", t)
	}
}
