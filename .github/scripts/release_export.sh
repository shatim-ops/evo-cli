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
#     решения владельца, evo-internal, handoff/) в файле не на Go — провал
#     (код 1);
#   - тот же маркер в файле Go — только число файлов в отчёте: комментарии
#     кода уже публичны в evo-cli (решение «релиза» 17.11, ТЗ п. 1);
#   - go build ./... и go vet ./... на снимке.
# КАТАЛОГ не существует или пуст. Код 2 — ошибка вызова.
set -euo pipefail
ref=${1:-}
out=${2:-}
[ -n "$ref" ] && [ -n "$out" ] || { echo "usage: release_export.sh РЕВИЗИЯ КАТАЛОГ" >&2; exit 2; }
src=$(git rev-parse --show-toplevel)
sha=$(git -C "$src" rev-parse --verify --quiet "$ref^{commit}") || { echo "release_export: ревизии «$ref» нет" >&2; exit 2; }
if [ -e "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ]; then
	echo "release_export: каталог $out не пуст" >&2
	exit 2
fi
mkdir -p "$out"
# Абсолютный путь и не корень: ниже из каталога удаляются исключения.
out=$(cd "$out" && pwd -P)
[ -n "$out" ] && [ "$out" != / ] || { echo "release_export: каталог «$out» не годится" >&2; exit 2; }

# Список включения: каталоги и файлы верхнего уровня.
include=(cmd internal go.mod go.sum README.md LICENSE CHANGELOG.md SECURITY.md docs install.sh install.ps1 .goreleaser.yaml)
present=()
for p in "${include[@]}"; do
	if git -C "$src" cat-file -e "$sha:$p" 2>/dev/null; then present+=("$p"); fi
done
git -C "$src" archive --format=tar "$sha" -- "${present[@]}" | tar -x -C "$out"

# Исключения внутри списка: документы разработки, тесты и их данные.
rm -rf "$out/docs/internal" "$out/docs/RELEASING.md"
find "$out" -name '*_test.go' -type f -delete
find "$out" -type d -name testdata -prune -exec rm -rf {} +

# Инвариант: путь — только из списка включения и вне исключений.
bad=0
while IFS= read -r f; do
	rel=${f#"$out"/}
	top=${rel%%/*}
	case " ${include[*]} " in *" $top "*) ;; *) echo "::error::вне списка включения: $rel"; bad=1 ;; esac
	case "$rel" in
	docs/internal/* | docs/RELEASING.md | *_test.go | */testdata/*) echo "::error::исключённый путь в снимке: $rel"; bad=1 ;;
	esac
done < <(find "$out" -type f -not -path "$out/.git/*")

# Маркеры внутреннего.
# Астра — внешняя проверка, кириллицей; латинское Astra — это Astra Linux.
markers='docs/internal|«Риск|«Риска»|Risk», conclusion|conclusion [0-9]|«Процесс|«разработк|— релиз|Астр(а|ы|е|у|ой)[ ,.)#]|evo-internal|решени[ея] владельца|owner'"'"'s decision|release-plan|handoff/'
gofiles=0
while IFS= read -r f; do
	rel=${f#"$out"/}
	case "$rel" in
	*.go) gofiles=$((gofiles + 1)) ;;
	*) echo "::error::маркер внутреннего в $rel: $(grep -m1 -oE "$markers" "$f")"; bad=1 ;;
	esac
done < <(grep -rlE "$markers" "$out" --exclude-dir=.git || true)
echo "файлов Go с маркерами внутреннего в комментариях: $gofiles"
[ "$bad" = 0 ] || { echo "release_export: снимок не чист — не коммичу" >&2; exit 1; }

# Сборка снимка.
(cd "$out" && go build ./... && go vet ./...) || { echo "release_export: снимок не собирается" >&2; exit 1; }

# Один коммит без истории.
git -C "$out" init -q
git -C "$out" add -A
git -C "$out" -c user.name="evo release" -c user.email="release@localhost" commit -q -m "evo $ref"
echo "снимок: $out, коммит $(git -C "$out" rev-parse --short HEAD) «evo $ref», исходный SHA $sha"
