#!/usr/bin/env bash
# Проверка release_export.sh (job ci) на временном репозитории: в снимок
# идёт только список включения, без тестов, testdata, docs/internal и
# RELEASING; маркер внутреннего в документе — провал, в комментарии Go —
# только отчёт; снимок — один коммит без истории.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
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
got=$(cd "$out" && find . -type f -not -path './.git/*' | sort | tr '\n' ' ')
want="./LICENSE ./README.md ./cmd/x/main.go ./docs/guide.md ./go.mod ./internal/a/a.go "
[ "$got" = "$want" ] || { echo "FAIL: состав «$got», ждём «$want»"; fail=1; }
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
	LC_ALL=C LANG=C "$here/release_export.sh" HEAD "$tmp/s-$RANDOM" >/dev/null 2>&1 || rc=$?
	[ "$rc" = 1 ] || { echo "FAIL: «$text» при LC_ALL=C — код $rc, ждём 1"; fail=1; }
	git -c user.name=t -c user.email=t@t revert --no-edit HEAD >/dev/null
done

# Файл Go, который сканер не прочёл (битая ссылка), — провал «строки Go не
# проверены», а не «чисто» («Риск», 337).
ln -s nowhere.go internal/a/broken.go
git add internal/a/broken.go && git -c user.name=t -c user.email=t@t commit -qm broken-link
rc=0
log=$("$here/release_export.sh" HEAD "$tmp/s-link" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: непрочитанный файл Go — код $rc, ждём 1"; fail=1; }
case "$log" in *"строки Go не проверены"*"снимок не чист"*) ;; *) echo "FAIL: ошибка сканера не провалила снимок до сборки: $log"; fail=1 ;; esac
git rm -q internal/a/broken.go && git -c user.name=t -c user.email=t@t commit -qm no-link

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

# Ошибки вызова: нет ревизии, каталог не пуст, нет аргументов — код 2.
for args in "nope $tmp/s7" "HEAD $out" ""; do
	rc=0
	# shellcheck disable=SC2086
	"$here/release_export.sh" $args >/dev/null 2>&1 || rc=$?
	[ "$rc" = 2 ] || { echo "FAIL: вызов «$args» — код $rc, ждём 2"; fail=1; }
done
[ "$fail" = 0 ] && echo "release_export: ok"
exit "$fail"
