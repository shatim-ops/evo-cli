#!/usr/bin/env bash
# Проверка, на каких PR qwen-review.sh запускается сам (решение владельца
# 29.10, аудит #578, п. 9): только на PR, который трогает .github/,
# internal/workflows/ или код секретов и согласия; остальное — без ревью и
# без комментария; команда /qwen review (COMMENT_ID) — на любом PR; push в
# PR (synchronize, решение владельца 03.11) — если push меняет
# чувствительный путь из diff PR. git и gh подменены:
# пройдя отбор, скрипт пишет «diff пустой» — значит, ревью пошло.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/git" <<'SH'
#!/usr/bin/env bash
case "$*" in
  "diff --name-only refs/review/before"*) printf '%s\n' $FAKE_PUSHED ;;
  "diff --name-only"*) printf '%s\n' $FAKE_NAMES ;;
  *refs/review/before*) [ -z "${FAKE_NO_BEFORE:-}" ] || exit 1 ;;
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
    ACTION="${ACTION:-}" BEFORE="${BEFORE:-}" AFTER="${AFTER:-}" FAKE_PUSHED="${FAKE_PUSHED:-}" FAKE_NO_BEFORE="${FAKE_NO_BEFORE:-}" \
    PR=485 REPO=o/r MODEL=m FM_BASE_URL=http://127.0.0.1:1 GH_TOKEN=x FM_API_KEY=x \
    "$here/qwen-review.sh" >/dev/null 2>&1 || true
  got=skip
  ! grep -q 'diff пустой' "$work/post" || got=review
  if [ "$got" = "$4" ]; then echo "ok   $1"; else echo "FAIL $1: $got, ждали $4"; fail=1; fi
}
run "продуктовый PR без секретов и согласия" "Tab (продукт, 1.5)" "internal/tools/completion.go" skip
run "продуктовый PR: гейт согласия" "Гейт (продукт, 1.6)" "internal/tools/completion.go internal/tools/gate.go" review
run "продуктовый PR: маскирование HTTP" "H7 (продукт, 1.6)" "internal/mcp/http_redact.go" review
run "продуктовый PR: объявления согласия" "Согласие (продукт, 1.6)" "internal/tools/registry.go" review
run "продуктовый PR: токен HTTP" "H1 (продукт, 1.6)" "internal/mcp/http.go" review
run "продуктовый PR: журнал" "Журнал (продукт, 1.6)" "internal/journal/journal.go" review
run "продуктовый PR: ключи" "Ключ (продукт, 1.6)" "internal/auth/iam.go" review
run "только тесты кода согласия" "Инвариант (процесс)" "internal/tools/gate_test.go" skip
run "процесс, только тесты и документы" "Инвариант (процесс)" "internal/tools/x_test.go docs/a.md README.md" skip
run "процесс, правит .github/" "CI (процесс)" ".github/workflows/ci.yml" review
run "процесс, тест правил workflows" "Инвариант (процесс)" "internal/workflows/x_test.go" review
run "команда /qwen review на любом PR" "Tab (продукт, 1.5)" "internal/tools/completion.go" review 123
# push в PR (synchronize): пути PR — FAKE_NAMES, пути push — FAKE_PUSHED.
b=1111111111111111111111111111111111111111 a=2222222222222222222222222222222222222222
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED=".github/workflows/ci.yml internal/vmstage/x_test.go" \
  run "push добавил в PR .github/" "Тест (процесс)" ".github/workflows/ci.yml internal/vmstage/x_test.go" review
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED="internal/tools/x_test.go" \
  run "push в чувствительный PR без чувствительных путей" "CI (процесс)" ".github/workflows/ci.yml internal/tools/x_test.go" skip
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED=".github/workflows/ci.yml internal/tools/completion.go" \
  run "push — влитый main с правкой .github/, PR её не меняет" "Tab (продукт, 1.7)" "internal/tools/completion.go" skip
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED="internal/tools/gate.go" \
  run "push меняет код согласия" "Гейт (продукт, 1.7)" "internal/tools/gate.go" review
ACTION=synchronize BEFORE=0000000000000000000000000000000000000000 AFTER=$a FAKE_PUSHED="" \
  run "push без прежнего SHA — по diff PR" "CI (процесс)" ".github/workflows/ci.yml" review
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED="" FAKE_NO_BEFORE=1 \
  run "прежний SHA не получен (force-push) — по diff PR" "Tab (продукт, 1.7)" "internal/tools/completion.go" skip
ACTION=synchronize BEFORE=$b AFTER=$a FAKE_PUSHED="" FAKE_NO_BEFORE=1 \
  run "прежний SHA не получен, PR чувствительный — по diff PR" "CI (процесс)" ".github/workflows/ci.yml" review
exit "$fail"
