// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Checks of arguments the handlers make before the preview and before the
// cloud (QA 20.10): a value the API would refuse, or worse take, is USAGE
// with the argument named.

// aliasArg reads an argument that has an alias (pg_get id / cluster_id).
// Both given and different was the id silently: `evo pg delete A --id B`
// deleted B (QA-1). Given is as in oneOfArgs: " " and the alias was the
// alias, and pg_delete sent DELETE for it (break r4, R4-11). A value of
// only spaces is named first: id=" " cluster_id=a is "empty value id",
// not "id and cluster_id disagree" (release audit, REL-10).
func aliasArg(args map[string]any, name, alias string) (string, error) {
	if err := blankArgs(args, name, alias); err != nil {
		return "", err
	}
	rv, ra := stringArg(args, name), stringArg(args, alias)
	v, a := strings.TrimSpace(rv), strings.TrimSpace(ra)
	if rv != "" && ra != "" && v != a {
		return "", clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s and %s disagree (%q, %q)", "%s и %s расходятся (%q, %q)", name, alias, rv, ra),
			i18n.Tf("Pass one of them: %s is an alias of %s.", "Укажите один из них: %s — другое имя %s.", alias, name))
	}
	return firstNonEmptyStr(v, a), nil
}

// oneOfArgs reads the arguments of which a call passes one (vm_get vm_id
// or name, image_get image or image_id, release_floating_ip id or
// ip_address): the value of the one given, trimmed, and its key. An
// argument is given when its key holds a string that is not empty; an
// absent key, null and "" are not given. Two given is notBoth, whatever
// they hold; a given value of only spaces is USAGE "empty value <arg>".
// " " was trimmed away and the other argument taken without a word
// (break r4, R4-11). None given is "", "", nil: the caller says what is
// required.
func oneOfArgs(args map[string]any, notBoth error, keys ...string) (value, key string, err error) {
	for _, k := range keys {
		if stringArg(args, k) == "" {
			continue
		}
		if key != "" {
			return "", "", notBoth
		}
		key = k
	}
	if err := blankArgs(args, keys...); err != nil {
		return "", "", err
	}
	if key == "" {
		return "", "", nil
	}
	return strings.TrimSpace(stringArg(args, key)), key, nil
}

// blankArgs refuses a given argument of only spaces: "empty value <arg>".
func blankArgs(args map[string]any, keys ...string) error {
	for _, k := range keys {
		if v := stringArg(args, k); v != "" && strings.TrimSpace(v) == "" {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("empty value of %s: it is only spaces", "пустое значение %s: в нём только пробелы", k),
				i18n.Tf("Pass a value or leave %s out.", "Передайте значение или не передавайте %s вовсе.", k))
		}
	}
	return nil
}

// portsArg reads a list of ports: a JSON array of integers (or of strings
// holding one), or a comma-separated string. An element that is not a
// whole number, or a port outside 1-65535, is USAGE naming the element:
// "8000-8010" was dropped without a word, 0 and 70000 went to the API
// (QA-3). A range is not taken: a security group rule of open_ports and
// deploy_apply opens one port. A port given twice is kept once.
func portsArg(arg string, v any) ([]int, error) {
	var elems []any
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []any:
		elems = t
	case []int:
		for _, n := range t {
			elems = append(elems, n)
		}
	case string:
		for _, part := range strings.Split(t, ",") {
			if strings.TrimSpace(part) != "" {
				elems = append(elems, part)
			}
		}
	default:
		return nil, badPortsArg(arg, v)
	}
	out := make([]int, 0, len(elems))
	seen := map[int]bool{}
	for _, e := range elems {
		n, ok := 0, false
		switch x := e.(type) {
		case float64:
			n, ok = int(x), x == math.Trunc(x) && math.Abs(x) < 1e9
		case int:
			n, ok = x, true
		case json.Number:
			i, err := x.Int64()
			n, ok = int(i), err == nil
		case string:
			i, err := strconv.Atoi(strings.TrimSpace(x))
			n, ok = i, err == nil
		}
		if !ok {
			return nil, badPortsArg(arg, e)
		}
		if n < 1 || n > 65535 {
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s: port %d is out of range 1-65535", "%s: порт %d вне диапазона 1–65535", arg, n),
				i18n.T("A port is a whole number 1-65535, e.g. [80, 443].", "Порт — целое число 1–65535, например [80, 443]."))
		}
		// A port listed twice is one port: [22, 22] made two rules
		// (break r4, R4-19). The order of the first mention stays.
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

func badPortsArg(arg string, e any) error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s: %s is not a port number", "%s: %s — не номер порта", arg, describeValue(e)),
		i18n.T("A port is a whole number 1-65535, e.g. [80, 443]; ranges (8000-8010) are not taken: list the ports.",
			"Порт — целое число 1–65535, например [80, 443]; диапазоны (8000-8010) не принимаются: перечислите порты."))
}

// portArg checks an integer port argument that was passed: 1-65535.
func portArg(args map[string]any, key string) error {
	if v, ok := args[key]; !ok || v == nil {
		return nil
	}
	if p := intArg(args, key, 0); p < 1 || p > 65535 {
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s %d is out of range 1-65535", "%s %d вне диапазона 1–65535", key, p), "")
	}
	return nil
}

// minIntArg checks an integer argument that was passed: at least min.
func minIntArg(args map[string]any, key string, min int, hint string) error {
	if v, ok := args[key]; !ok || v == nil {
		return nil
	}
	if n := intArg(args, key, 0); n < min {
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s must be at least %d, got %d", "%s должен быть не меньше %d, передано %d", key, min, n), hint)
	}
	return nil
}

// dateArg parses a YYYY-MM-DD argument. "2026-13-45", "yesterday" and a
// timestamp went to the billing API as they were and the answer was 0
// (QA-5).
func dateArg(key, v string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s %q is not a date YYYY-MM-DD", "%s %q — не дата ГГГГ-ММ-ДД", key, v),
			i18n.T("Pass a calendar date, e.g. 2026-07-01.", "Передайте календарную дату, например 2026-07-01."))
	}
	return t, nil
}

// periodArgs checks a period: both dates YYYY-MM-DD, end not before start.
func periodArgs(start, end string) (time.Time, time.Time, error) {
	st, err := dateArg("start", start)
	if err != nil {
		return st, st, err
	}
	et, err := dateArg("end", end)
	if err != nil {
		return st, et, err
	}
	if et.Before(st) {
		return st, et, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("end %s is before start %s", "end %s раньше start %s", end, start),
			i18n.T("Swap the dates: start is the first day of the period, end the last.",
				"Поменяйте даты местами: start — первый день периода, end — последний."))
	}
	return st, et, nil
}

// maxPlainNameLen bounds a name no documented rule covers.
const maxPlainNameLen = 255

// plainName refuses only names that are wrong whatever the service's rule:
// empty once trimmed, spaces at either end, control characters, longer
// than 255 characters. The specifications of VPC, Managed PostgreSQL and
// Secret Manager give no rule for the name (QA-15); a stricter one would
// be made up.
func plainName(kind, arg, name string) error {
	why := ""
	switch {
	case strings.TrimSpace(name) == "":
		why = i18n.T("it is empty", "оно пустое")
	case strings.TrimSpace(name) != name:
		why = i18n.T("it starts or ends with a space", "в начале или в конце пробел")
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		why = i18n.T("it has a control character", "в нём управляющий символ")
	case utf8.RuneCountInString(name) > maxPlainNameLen:
		why = i18n.Tf("it is longer than %d characters", "оно длиннее %d символов", maxPlainNameLen)
	default:
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s %s %q: %s", "%s %s %q: %s", kind, arg, name, why),
		i18n.T("Pass a name without leading or trailing spaces and control characters.",
			"Передайте имя без пробелов по краям и управляющих символов."))
}

// k8sNameRe is the rule of the mk8s specification for a cluster name:
// «Может содержать буквы, цифры, дефис, подчеркивание. Допустимое
// количество символов от 3 до 60» (openapi-v3, CreateClusterRequest).
var k8sNameRe = regexp.MustCompile(`^[\p{L}\p{N}_-]{3,60}$`)

// k8sClusterName checks the name a new cluster gets (the argument, or the
// default k8s-<product>).
func k8sClusterName(name string) error {
	if k8sNameRe.MatchString(name) {
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("cluster name %q is not accepted by Managed Kubernetes", "имя кластера %q Managed Kubernetes не примет", name),
		i18n.T("3-60 characters: letters, digits, '-' and '_'.", "3–60 символов: буквы, цифры, '-' и '_'."))
}

// bucketNameRe is the rule s3_bucket_create states for a new bucket:
// 3-63 lowercase letters, digits, dots and hyphens.
var bucketNameRe = regexp.MustCompile(`^[a-z0-9.-]{3,63}$`)

func bucketName(name string) error {
	if bucketNameRe.MatchString(name) {
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("bucket name %q is not accepted", "имя бакета %q не подходит", name),
		i18n.T("3-63 characters: lowercase Latin letters, digits, '.' and '-'.",
			"3–63 символа: строчные латинские буквы, цифры, '.' и '-'."))
}
