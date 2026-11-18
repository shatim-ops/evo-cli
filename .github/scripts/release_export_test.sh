#!/usr/bin/env bash
# Проверка release_export.sh (job ci) на временном репозитории: в снимок
# идёт только список включения, без тестов, testdata, docs/internal и
# RELEASING; маркер внутреннего в документе — провал, в комментарии Go —
# только отчёт; снимок — один коммит без истории.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# Каталог снимка каждого случая — свой, новый и пустой (mktemp -d): имя
# из $RANDOM дважды за прогон давало «каталог не пуст», код 2 (CI #2406).
fail=0
repo=$tmp/repo
mkdir -p "$repo"/cmd/x "$repo"/internal/a/testdata "$repo"/docs/internal "$repo"/changelog.d "$repo"/.github/workflows
cd "$repo"
git init -q
printf 'module example.test/x\n\ngo 1.22\n' >go.mod
printf 'package main\n\nimport "example.test/x/internal/a"\n\nfunc main() { a.A() }\n' >cmd/x/main.go
printf 'package a\n\n// A does nothing.\nfunc A() {}\n' >internal/a/a.go
printf 'package a\n\nimport "testing"\n\nfunc TestA(t *testing.T) { A() }\n' >internal/a/a_test.go
echo data >internal/a/testdata/f
echo '# x' >README.md
echo license >LICENSE
echo '# guide' >docs/guide.md
echo plan >docs/internal/plan.md
echo releasing >docs/RELEASING.md
echo fragment >changelog.d/x.md
echo 'on: push' >.github/workflows/ci.yml
git add -A
git -c user.name=t -c user.email=t@t commit -q -m one
echo more >>README.md
git -c user.name=t -c user.email=t@t commit -qam two
git tag v0.1.0

# Чистая ревизия: снимок, состав, один коммит без родителя.
out=$tmp/snap
log=$("$here/release_export.sh" v0.1.0 "$out" 2>&1) || { echo "FAIL: чистая ревизия: $log"; fail=1; }
# Порядок состава — байтовый, при любой локали теста (Астра #2389: sort
# в en_US.UTF-8 ставит ./LICENSE после ./internal).
got=$(cd "$out" && find . -type f -not -path './.git/*' | LC_ALL=C sort | tr '\n' ' ')
want="./LICENSE ./README.md ./cmd/x/main.go ./docs/guide.md ./go.mod ./internal/a/a.go "
[ "$got" = "$want" ] || { echo "FAIL: состав «${got}», ждём «${want}»"; fail=1; }
[ "$(git -C "$out" rev-list --count HEAD 2>/dev/null)" = 1 ] || { echo "FAIL: в снимке не один коммит"; fail=1; }
[ "$(git -C "$out" log -1 --format=%s 2>/dev/null)" = "evo v0.1.0" ] || { echo "FAIL: тема коммита"; fail=1; }
case "$log" in *"исходный SHA $(git rev-parse v0.1.0)"*) ;; *) echo "FAIL: исходный SHA не назван: $log"; fail=1 ;; esac

# Маркер внутреннего в документе — провал, снимка нет.
echo 'см. docs/internal/plan.md' >>docs/guide.md
git -c user.name=t -c user.email=t@t commit -qam doc
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s2" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: маркер в документе — код $rc, ждём 1"; fail=1; }
case "$log" in *"docs/guide.md"*) ;; *) echo "FAIL: файл с маркером не назван: $log"; fail=1 ;; esac
[ ! -d "$tmp/s2/.git" ] || { echo "FAIL: снимок с маркером закоммичен"; fail=1; }
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# Маркер в комментарии Go — только отчёт.
printf 'package a\n\n// A follows «Риск», conclusion 1.\nfunc A() {}\n' >internal/a/a.go
git -c user.name=t -c user.email=t@t commit -qam gocomment
log=$("$here/release_export.sh" HEAD "$tmp/s3" 2>&1) || { echo "FAIL: маркер в комментарии Go — провал: $log"; fail=1; }
case "$log" in *"файлов Go с маркерами внутреннего в комментариях: 1"*) ;; *) echo "FAIL: отчёт о Go: $log"; fail=1 ;; esac

# Маркер в строке Go — текст продукта: провал, файл и строка названы.
printf 'package a\n\n// A does nothing.\nfunc A() string { return "see docs/internal/x.md" }\n' >internal/a/a.go
printf 'package main\n\nimport "example.test/x/internal/a"\n\nfunc main() { _ = a.A() }\n' >cmd/x/main.go
git -c user.name=t -c user.email=t@t commit -qam gostring
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s4" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: маркер в строке Go — код $rc, ждём 1"; fail=1; }
case "$log" in *"internal/a/a.go:4"*) ;; *) echo "FAIL: строка Go с маркером не названа: $log"; fail=1 ;; esac
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# Маркер кириллицей при локали C и документы разработки — провал.
for text in 'Решение владельца 17.11' 'по решению владельца' 'см. docs/RELEASING.md' 'решения D — DECISIONS.md' 'фрагмент changelog.d/x.md'; do
	echo "$text" >>docs/guide.md
	git -c user.name=t -c user.email=t@t commit -qam marker
	rc=0
	LC_ALL=C LANG=C "$here/release_export.sh" HEAD "$(mktemp -d "$tmp/s-XXXXXX")" >/dev/null 2>&1 || rc=$?
	[ "$rc" = 1 ] || { echo "FAIL: «${text}» при LC_ALL=C — код $rc, ждём 1"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done

# Ссылка в снимке — отказ до любого удаления: rm через ссылку docs удалил бы
# файлы вне снимка (Астра #2311).
mkdir -p "$tmp/outside/internal" && echo keep >"$tmp/outside/RELEASING.md"
git rm -q -r docs && ln -s "$tmp/outside" docs
git add docs && git -c user.name=t -c user.email=t@t commit -qm docs-link
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-link" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: ссылка в снимке — код $rc, ждём 1"; fail=1; }
case "$log" in *"символическая ссылка в снимке: docs"*) ;; *) echo "FAIL: ссылка не названа: $log"; fail=1 ;; esac
[ -e "$tmp/outside/RELEASING.md" ] && [ -d "$tmp/outside/internal" ] || { echo "FAIL: удалены файлы вне снимка через ссылку"; fail=1; }
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# Файл Go, который сканер не прочёл до конца, — провал «строки Go не
# проверены» до сборки, а не «чисто» («Риск», 337).
printf 'package a\n\nvar s = "unterminated\n' >internal/a/broken.go
git add internal/a/broken.go && git -c user.name=t -c user.email=t@t commit -qm broken-go
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-broken" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: неразобранный файл Go — код $rc, ждём 1"; fail=1; }
case "$log" in *"строки Go не проверены"*"снимок не чист"*) ;; *) echo "FAIL: ошибка сканера не провалила снимок до сборки: $log"; fail=1 ;; esac
git rm -q internal/a/broken.go && git -c user.name=t -c user.email=t@t commit -qm no-broken

# Строка Go с маркером через escape — по значению (Астра #2307).
printf 'package a\n\n// A does nothing.\nfunc A() string { return "docs\\x2finternal/x.md" }\n' >internal/a/a.go
git -c user.name=t -c user.email=t@t commit -qam goescape
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-esc" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: маркер в строке Go через escape — код $rc, ждём 1"; fail=1; }
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# Документ не в UTF-8 (CP1251, UTF-16) и маркер на двух строках — провал
# (Астра #2309).
for enc in cp1251 utf16 split; do
	case "$enc" in
	cp1251) printf 'по \xf0\xe5\xf8\xe5\xed\xe8\xfe \xe2\xeb\xe0\xe4\xe5\xeb\xfc\xf6\xe0\n' >docs/guide.md ;;
	utf16) printf '\xff\xfed\x00o\x00c\x00s\x00/\x00i\x00n\x00t\x00e\x00r\x00n\x00a\x00l\x00' >docs/guide.md ;;
	split) printf 'по решению\nвладельца\n' >docs/guide.md ;;
	esac
	git -c user.name=t -c user.email=t@t commit -qam "enc-$enc"
	rc=0
	"$here/release_export.sh" HEAD "$tmp/s-$enc" >/dev/null 2>&1 || rc=$?
	[ "$rc" = 1 ] || { echo "FAIL: документ $enc — код $rc, ждём 1"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done

# Файл незнакомого типа не пропускается молча: текст — как документ,
# не текст — по байтам; неразрывный пробел, мягкий перенос и знаки нулевой
# ширины не прячут маркер («Риск», 342, У1 и У2).
nb=$(printf '\302\240')
shy=$(printf '\302\255')
zw=$(printf '\342\200\213')
for c in "docs/note.tpl:см. docs/internal/x" "internal/a/skill.tmpl:по решению владельца" "docs/Makefile:see release-plan" \
	"docs/bin.dat:$(printf '\377\376')docs/internal" "docs/guide.md:по решению${nb}владельца" \
	"docs/guide.md:по реше${shy}нию владельца" "docs/guide.md:docs/inter${zw}nal" "docs/guide.md:Астра${nb}#2307"; do
	file=${c%%:*}
	printf '%s\n' "${c#*:}" >"$file"
	git add "$file" && git -c user.name=t -c user.email=t@t commit -qm "odd $file"
	for loc in C C.UTF-8 ""; do
		rc=0
		LC_ALL=$loc "$here/release_export.sh" HEAD "$(mktemp -d "$tmp/s-XXXXXX")" >/dev/null 2>&1 || rc=$?
		[ "$rc" = 1 ] || { echo "FAIL: «${c#*:}» в $file при LC_ALL=${loc:-по умолчанию} — код $rc, ждём 1"; fail=1; }
	done
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done
# Корректный UTF-8 с буквой на границе буфера iconv macOS (1023, 4095,
# 8191 байт ASCII перед «я») — не провал: кодировку проверяет помощник на Go,
# одинаково на всех системах (Астра #2365).
for n in 1023 4095 8191; do
	{ head -c "$n" /dev/zero | tr '\0' a; printf 'я\n'; } >docs/edge.md
	git add docs/edge.md && git -c user.name=t -c user.email=t@t commit -qm "edge $n"
	rc=0
	log=$("$here/release_export.sh" HEAD "$tmp/s-edge-$n" 2>&1) || rc=$?
	[ "$rc" = 0 ] || { echo "FAIL: корректный UTF-8 ($n байт перед «я») — код $rc: $log"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done
# Документ с NUL — провал: маркер за NUL grep может не увидеть.
printf 'guide\000docs/internal\n' >docs/guide.md
git -c user.name=t -c user.email=t@t commit -qam nul
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-nul" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: документ с NUL — код $rc, ждём 1"; fail=1; }
case "$log" in *"не в UTF-8 (или с NUL)"*"docs/guide.md"*) ;; *) echo "FAIL: документ с NUL не назван: $log"; fail=1 ;; esac
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
# Окончание в три-четыре буквы при поиске по байтам (LC_ALL=C) — маркер.
for text in 'по решениями владельца' 'решениях владельца'; do
	echo "$text" >>docs/guide.md
	git -c user.name=t -c user.email=t@t commit -qam ending
	rc=0
	LC_ALL=C "$here/release_export.sh" HEAD "$(mktemp -d "$tmp/s-XXXXXX")" >/dev/null 2>&1 || rc=$?
	[ "$rc" = 1 ] || { echo "FAIL: «${text}» при LC_ALL=C — код $rc, ждём 1"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done
# Ошибка поиска маркера — провал, а не «маркеров нет» (Астра #2364).
mkdir -p "$tmp/badgrep"
printf '#!/bin/sh\nexit 2\n' >"$tmp/badgrep/grep" && chmod +x "$tmp/badgrep/grep"
rc=0
log=$(PATH="$tmp/badgrep:$PATH" "$here/release_export.sh" HEAD "$tmp/s-badgrep" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: grep с ошибкой — код $rc, ждём 1"; fail=1; }
case "$log" in *"поиск маркера не выполнен"*) ;; *) echo "FAIL: ошибка grep не названа: $log"; fail=1 ;; esac

# Не текст без маркера и неразрывный пробел без маркера — не провал.
printf '\377\376\000\001' >docs/logo.bin
printf 'обычный%sтекст\n' "$nb" >>docs/guide.md
git add docs/logo.bin && git -c user.name=t -c user.email=t@t commit -qam clean-odd
"$here/release_export.sh" HEAD "$tmp/s-clean-odd" >/dev/null 2>&1 || { echo "FAIL: двоичный файл без маркера или неразрывный пробел — провал"; fail=1; }
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# Строки Go, склеенные +, — одна строка («Риск», 342).
for v in '"docs/" + "internal/x.md"' '"по решению" +\n\t\t"\\u00a0владельца"' '"docs/" + "inter" + "nal"'; do
	printf 'package a\n\n// A does nothing.\nfunc A() string { return %b }\n' "$v" >internal/a/a.go
	git -c user.name=t -c user.email=t@t commit -qam goconcat
	rc=0
	"$here/release_export.sh" HEAD "$(mktemp -d "$tmp/s-XXXXXX")" >/dev/null 2>&1 || rc=$?
	[ "$rc" = 1 ] || { echo "FAIL: склейка строк Go $v — код $rc, ждём 1"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done
# Склейка через вызов — две строки, не одна.
printf 'package a\n\n// A does nothing.\nfunc A() string { return f("docs/") + "internal" }\n\nfunc f(s string) string { return s }\n' >internal/a/a.go
git -c user.name=t -c user.email=t@t commit -qam gocall
"$here/release_export.sh" HEAD "$tmp/s-call" >/dev/null 2>&1 || { echo "FAIL: строки по разные стороны вызова склеены"; fail=1; }
git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null

# CHANGELOG снимка — без внутренних ссылок разработки (решение владельца
# 18.11, #2380): решения, аудиты Fable, прогоны Grok, коды их находок.
# Часть скобки без ссылки остаётся, скобка без остатка уходит, отступ
# строки — тоже остаётся.
cat >CHANGELOG.md <<'MD'
## 9.9.9

- **Отказ до вопроса** (DECISIONS D47). Было: код 5.
- **Список инструментов** (`a`, `b`: `x`; аудит Fable 1.6, FB160-4; DECISIONS D79).
- **Мелкие находки полного аудита Fable 1.6** (FB160-2, -3).
- **Мелочи прогона Grok на 1.6.0** (агентный прогон Grok на 1.6.0, F9).
- **Ответ** как `whoami` по D88, — проверка
  (D94) — в обоих путях; повтор по `next_args` D76 (DECISIONS D40;
  исправление: живой прогон Grok на 1.3.0, G130-15). Конец.
- Согласно D47 отказ до вопроса; запись (исправление: прогон G13-31).
MD
cat >"$tmp/changelog.want" <<'MD'
## 9.9.9

- **Отказ до вопроса**. Было: код 5.
- **Список инструментов** (`a`, `b`: `x`).
- **Мелкие находки полного аудита 1.6**.
- **Мелочи прогона на 1.6.0**.
- **Ответ** как `whoami`, — проверка
   — в обоих путях; повтор по `next_args`. Конец.
- отказ до вопроса; запись.
MD
git add CHANGELOG.md && git -c user.name=t -c user.email=t@t commit -qm changelog
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-changelog" 2>&1) || rc=$?
[ "$rc" = 0 ] || { echo "FAIL: CHANGELOG со ссылками разработки — код $rc: $log"; fail=1; }
diff -u "$tmp/changelog.want" "$tmp/s-changelog/CHANGELOG.md" >"$tmp/changelog.diff" ||
	{ echo "FAIL: CHANGELOG снимка не тот:"; sed 's/^/  | /' "$tmp/changelog.diff"; fail=1; }
git -C "$tmp/s-changelog" show HEAD:CHANGELOG.md | grep -qE 'DECISIONS|Fable|Grok|D[0-9]' &&
	{ echo "FAIL: в коммите снимка осталась ссылка разработки"; fail=1; }
# Ссылка, которую правило не вычищает (код находки вне скобок), — провал
# снимка с названной строкой, а не молчаливая публикация.
printf -- '- находка FB150-2 закрыта.\n' >>CHANGELOG.md
git -c user.name=t -c user.email=t@t commit -qam changelog-left
rc=0
log=$("$here/release_export.sh" HEAD "$(mktemp -d "$tmp/s-XXXXXX")" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: невычищенная ссылка в CHANGELOG — код $rc, ждём 1"; fail=1; }
case "$log" in *"внутренняя ссылка в CHANGELOG.md:"*"FB150-2"*) ;; *) echo "FAIL: невычищенная ссылка не названа: $log"; fail=1 ;; esac
git rm -q CHANGELOG.md && git -c user.name=t -c user.email=t@t commit -qm no-changelog

# Латинское Astra (Astra Linux) — не маркер.
echo 'образ Astra Linux' >>docs/guide.md
git -c user.name=t -c user.email=t@t commit -qam astra
"$here/release_export.sh" HEAD "$tmp/s5" >/dev/null 2>&1 || { echo "FAIL: Astra Linux принят за маркер"; fail=1; }

# Снимок не собирается — провал, коммита нет.
printf 'package a\n\nfunc A() { undefined() }\n' >internal/a/a.go
git -c user.name=t -c user.email=t@t commit -qam broken
rc=0
"$here/release_export.sh" HEAD "$tmp/s6" >/dev/null 2>&1 || rc=$?
[ "$rc" = 1 ] && [ ! -d "$tmp/s6/.git" ] || { echo "FAIL: несобираемый снимок — код $rc"; fail=1; }

# Единственный пакет main: go build ./... положил бы бинарь x в корень
# снимка, и он ушёл бы в коммит (Астра #2310).
git rm -q -r internal && printf 'package main\n\nfunc main() {}\n' >cmd/x/main.go
git -c user.name=t -c user.email=t@t commit -qam onemain
"$here/release_export.sh" HEAD "$tmp/s-main" >/dev/null 2>&1 || { echo "FAIL: снимок с одним пакетом main не создан"; fail=1; }
if [ -e "$tmp/s-main/x" ] || git -C "$tmp/s-main" ls-files 2>/dev/null | grep -qvE '^(README.md|LICENSE|go.mod|cmd/x/main.go|docs/guide.md)$'; then
	echo "FAIL: в снимке лишний файл после сборки: $(git -C "$tmp/s-main" ls-files 2>/dev/null | tr '\n' ' ')"
	fail=1
fi

# Ошибки вызова: нет ревизии, каталог не пуст, нет аргументов — код 2.
for args in "nope $tmp/s7" "HEAD $out" ""; do
	rc=0
	# shellcheck disable=SC2086
	"$here/release_export.sh" $args >/dev/null 2>&1 || rc=$?
	[ "$rc" = 2 ] || { echo "FAIL: вызов «${args}» — код $rc, ждём 2"; fail=1; }
done
[ "$fail" = 0 ] && echo "release_export: ok"
exit "$fail"
