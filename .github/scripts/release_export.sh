#!/usr/bin/env bash
# release_export.sh РЕВИЗИЯ КАТАЛОГ — чистый снимок релиза для официального
# репозитория (D116; ТЗ docs/internal/briefs/cloudru-publish.md, п. 0.1 и 1).
#
# Из РЕВИЗИИ (тег или SHA) берётся только список включения: код продукта
# (cmd/, internal/ без тестов и тестовых данных, go.mod, go.sum),
# документы пользователя (README.md, LICENSE, CHANGELOG.md, SECURITY.md,
# docs/ без docs/internal/ и без docs/RELEASING.md — процесс разработки),
# установка (install.sh, install.ps1, .goreleaser.yaml). Всё прочее —
# changelog.d/, .github/, срезы, брифы, планы — в снимок не идёт.
#
# Снимок — один коммит без истории («evo РЕВИЗИЯ») в новом репозитории
# КАТАЛОГ; исходный SHA печатается для описания релиза. Проверки:
#   - путь вне списка включения — провал (код 1);
#   - маркер внутреннего (docs/internal, роли, заключения «Риска», Астра,
#     решения владельца, evo-internal, handoff/, документы разработки
#     RELEASING.md, DECISIONS.md, changelog.d) в файле не на Go — провал
#     (код 1); в строке Go (тексте продукта) — тоже провал;
#   - тот же маркер в комментарии Go — только число файлов в отчёте:
#     комментарии кода уже публичны в evo-cli (решение «релиза» 17.11, ТЗ
#     п. 1);
#   Маркеры — литеральные последовательности, классы — только из ASCII
#   (окончание падежа — [^ ]{0,4}): совпадают при любой локали, LC_ALL=C
#   тоже («Риск», 335, У1).
#   - go build ./... и go vet ./... на снимке.
# КАТАЛОГ не существует или пуст. Код 2 — ошибка вызова.
set -euo pipefail
ref=${1:-}
out=${2:-}
[ -n "$ref" ] && [ -n "$out" ] || { echo "usage: release_export.sh РЕВИЗИЯ КАТАЛОГ" >&2; exit 2; }
src=$(git rev-parse --show-toplevel)
sha=$(git -C "$src" rev-parse --verify --quiet "$ref^{commit}") || { echo "release_export: ревизии «${ref}» нет" >&2; exit 2; }
if [ -e "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ]; then
	echo "release_export: каталог $out не пуст" >&2
	exit 2
fi
mkdir -p "$out"
# Абсолютный путь и не корень: ниже из каталога удаляются исключения.
out=$(cd "$out" && pwd -P)
[ -n "$out" ] && [ "$out" != / ] || { echo "release_export: каталог «${out}» не годится" >&2; exit 2; }

# Список включения: каталоги и файлы верхнего уровня.
include=(cmd internal go.mod go.sum README.md LICENSE CHANGELOG.md SECURITY.md docs install.sh install.ps1 .goreleaser.yaml)
present=()
for p in "${include[@]}"; do
	if git -C "$src" cat-file -e "$sha:$p" 2>/dev/null; then present+=("$p"); fi
done
git -C "$src" archive --format=tar "$sha" -- "${present[@]}" | tar -x -C "$out"

# Ссылка в снимке — отказ до любого удаления: rm через ссылку docs удалил
# бы файлы вне снимка (Астра #2311), а в публичном релизе ссылок нет.
links=$(find "$out" -type l)
if [ -n "$links" ]; then
	while IFS= read -r l; do echo "::error::символическая ссылка в снимке: ${l#"$out"/}"; done <<<"$links"
	echo "release_export: снимок не чист — не коммичу" >&2
	exit 1
fi

# Исключения внутри списка: документы разработки, тесты и их данные.
rm -rf "$out/docs/internal" "$out/docs/RELEASING.md"
find "$out" -name '*_test.go' -type f -delete
find "$out" -type d -name testdata -prune -exec rm -rf {} +

# Инвариант: путь — только из списка включения и вне исключений. Ещё раз —
# после сборки, перед коммитом: сборка не должна ничего добавить (Астра
# #2310).
check_paths() {
	while IFS= read -r f; do
		rel=${f#"$out"/}
		top=${rel%%/*}
		case " ${include[*]} " in *" $top "*) ;; *) echo "::error::вне списка включения: $rel"; bad=1 ;; esac
		case "$rel" in
		docs/internal/* | docs/RELEASING.md | *_test.go | */testdata/*) echo "::error::исключённый путь в снимке: $rel"; bad=1 ;;
		esac
	done < <(find "$out" -not -type d -not -path "$out/.git/*")
}
bad=0
check_paths

# Маркеры внутреннего.
# Астра — внешняя проверка, кириллицей; латинское Astra — это Astra Linux.
markers='docs/internal|«Риск|«Риска»|Risk», conclusion|conclusion [0-9]|«Процесс|«разработк|— релиз|Астр(а|ы|е|у|ой)[ ,.)#]|evo-internal|(Р|р)ешени[^ ]{0,4} владельца|owner'"'"'s decision|release-plan|handoff/|RELEASING\.md|DECISIONS\.md|changelog\.d'
# Текст документа проверяется после схлопывания пробелов и переводов строк
# (маркер на двух строках), и только в UTF-8 без NUL: иначе байты маркера в
# другой кодировке не совпадают (Астра #2309).
text_doc() {
	case "$1" in
	*.md | *.txt | *.html | *.htm | *.yaml | *.yml | *.json | *.sh | *.ps1 | *.toml | *.css | *.js | *.go | *.mod | *.sum | *.xml | *.svg) return 0 ;;
	README* | LICENSE* | SECURITY* | CHANGELOG* | */README* | */LICENSE* | */NOTICE*) return 0 ;;
	esac
	return 1
}
gofiles=0
while IFS= read -r f; do
	rel=${f#"$out"/}
	text_doc "$rel" || continue
	if ! iconv -f UTF-8 -t UTF-8 "$f" >/dev/null 2>&1 || [ "$(tr -d '\000' <"$f" | wc -c)" != "$(wc -c <"$f")" ]; then
		echo "::error::документ не в UTF-8 (или с NUL), маркеры не проверить: $rel"
		bad=1
		continue
	fi
	hit=$(tr -s ' \t\r\n' '    ' <"$f" | grep -m1 -oE "$markers" || true)
	[ -n "$hit" ] || continue
	case "$rel" in
	*.go) gofiles=$((gofiles + 1)) ;;
	*) echo "::error::маркер внутреннего в $rel: $hit"; bad=1 ;;
	esac
done < <(find "$out" -type f -not -path "$out/.git/*")
# Строки Go — текст продукта, который видит пользователь или агент: маркер
# в них — провал; комментарии — отчёт выше («Риск», 335, У2). Разбор —
# go/scanner, вне снимка.
scan=$(mktemp -d)
trap 'rm -rf "$scan"' EXIT
cat >"$scan/scan.go" <<'GO'
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	re := regexp.MustCompile(os.Args[2])
	root := os.Args[1]
	bad := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		var s scanner.Scanner
		// A file the scanner cannot read through may hide a string: not
		// checked is not clean.
		broken := false
		s.Init(fset.AddFile(path, -1, len(src)), src, func(p token.Position, msg string) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", p, msg)
			broken = true
		}, 0)
		for {
			pos, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING && tok != token.CHAR {
				continue
			}
			// The value, not only the spelling: "docs\x2finternal" is
			// docs/internal (Астра #2307).
			text := lit
			if v, err := strconv.Unquote(lit); err == nil {
				text = lit + "\n" + v
			}
			if m := re.FindString(text); m != "" {
				rel, _ := filepath.Rel(root, path)
				fmt.Printf("%s:%d: %s\n", rel, fset.Position(pos).Line, m)
				bad++
			}
		}
		if broken {
			return fmt.Errorf("%s: not read through", path)
		}
		return nil
	})
	if walkErr != nil {
		// A file not read is a file not checked: not «clean» («Риск», 335).
		fmt.Fprintln(os.Stderr, walkErr)
		os.Exit(2)
	}
	if bad > 0 {
		os.Exit(1)
	}
}
GO
rc=0
# Бинарь, а не go run: go run сводит код программы к 1, а ошибка чтения
# (2) — не то же, что маркер (1).
(cd "$scan" && go build -o scan scan.go) || { echo "release_export: сканер строк Go не собрался" >&2; exit 2; }
found=$("$scan/scan" "$out" "$markers") || rc=$?
case "$rc" in
0) ;;
1)
	while IFS= read -r l; do echo "::error::маркер внутреннего в строке Go $l"; done <<<"$found"
	bad=1
	;;
*)
	echo "::error::строки Go не проверены (сканер, код $rc)"
	bad=1
	;;
esac
echo "файлов Go с маркерами внутреннего в комментариях: $gofiles"
[ "$bad" = 0 ] || { echo "release_export: снимок не чист — не коммичу" >&2; exit 1; }

# Сборка снимка.
(cd "$out" && go build -o /dev/null ./... && go vet ./...) || { echo "release_export: снимок не собирается" >&2; exit 1; }
check_paths
[ "$bad" = 0 ] || { echo "release_export: сборка оставила файл в снимке — не коммичу" >&2; exit 1; }

# Один коммит без истории.
git -C "$out" init -q
git -C "$out" add -A
git -C "$out" -c user.name="evo release" -c user.email="release@localhost" commit -q -m "evo $ref"
echo "снимок: $out, коммит $(git -C "$out" rev-parse --short HEAD) «evo ${ref}», исходный SHA $sha"
