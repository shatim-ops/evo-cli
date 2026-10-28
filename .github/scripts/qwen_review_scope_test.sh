#!/usr/bin/env bash
# Проверка, на каких PR qwen-review.sh запускается сам (решение владельца
# #485, П1 (б)): на всех, кроме PR, где только тесты и документы и нет
# .github/ и internal/workflows/ — там без ревью и без комментария; код
# продукта под меткой «(процесс)» — ревью (§5: по изменению); команда
# /qwen review (COMMENT_ID) — на любом PR. git и gh подменены:
# пройдя отбор, скрипт пишет «diff пустой» — значит, ревью пошло.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/git" <<'SH'
#!/usr/bin/env bash
case "$*" in
  "diff --name-only"*) printf '%s\n' $FAKE_NAMES ;;
  fetch*|diff*) ;;
esac
SH
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
case "$1 $2" in
  "pr view") jq -n --arg t "$FAKE_TITLE" '{title:$t, body:"", baseRefName:"main", headRefOid:"abcdef0123456789abcdef0123456789abcdef01"}' ;;
  "pr comment") while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cat "$2" >>"$FAKE_POST"; shift; done ;;
esac
SH
chmod +x "$work/bin/git" "$work/bin/gh"
fail=0
run() { # ИМЯ заголовок пути ждём(review|skip) [COMMENT_ID]
  : >"$work/post"
  PATH="$work/bin:$PATH" FAKE_POST="$work/post" FAKE_TITLE="$2" FAKE_NAMES="$3" COMMENT_ID="${5:-}" \
    PR=485 REPO=o/r MODEL=m FM_BASE_URL=http://127.0.0.1:1 GH_TOKEN=x FM_API_KEY=x \
    "$here/qwen-review.sh" >/dev/null 2>&1 || true
  got=skip
  ! grep -q 'diff пустой' "$work/post" || got=review
  if [ "$got" = "$4" ]; then echo "ok   $1"; else echo "FAIL $1: $got, ждали $4"; fail=1; fi
}
run "продуктовый PR" "Tab (продукт, 1.5)" "internal/tools/completion.go" review
run "процесс, только тесты и документы" "Инвариант (процесс)" "internal/tools/x_test.go docs/a.md README.md" skip
run "процесс, правит .github/" "CI (процесс)" ".github/workflows/ci.yml" review
run "процесс, тест правил workflows" "Инвариант (процесс)" "internal/workflows/x_test.go" review
run "код продукта под меткой «(процесс)»" "Правка (процесс)" "internal/tools/x_test.go internal/tools/tools_s3.go" review
run "процесс, команда /qwen review" "Инвариант (процесс)" "internal/tools/x_test.go" review 123
exit "$fail"
