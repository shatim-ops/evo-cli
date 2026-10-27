#!/usr/bin/env bash
# Проверка начала qwen-review.sh (job ci) с подменёнными git и gh: нет
# refs/pull/N/head (GitHub не создал её, #412) — голова PR берётся по sha;
# git не получил ни того, ни другого — комментарий в PR, а не молчаливый
# красный job.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/git" <<'SH'
#!/usr/bin/env bash
case "$*" in
  *refs/pull/*) echo "fatal: couldn't find remote ref" >&2; exit 128 ;;
  fetch*) [ -z "${FAKE_SHA_FAIL:-}" ] || { echo "fatal: no such sha" >&2; exit 128; }; echo "$*" >>"$FAKE_LOG" ;;
  diff*) ;; # пустой diff: скрипт отвечает «diff пустой» и выходит
esac
SH
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
case "$1 $2" in
  "pr view") echo '{"title":"t","body":"","baseRefName":"main","headRefOid":"abcdef0123456789abcdef0123456789abcdef01"}' ;;
  "pr comment") while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cat "$2" >>"$FAKE_POST"; shift; done ;;
esac
SH
chmod +x "$work/bin/git" "$work/bin/gh"
fail=0
run() { # ИМЯ ждём-в-комментарии [FAKE_SHA_FAIL]
  : >"$work/post"
  : >"$work/log"
  PATH="$work/bin:$PATH" FAKE_LOG="$work/log" FAKE_POST="$work/post" FAKE_SHA_FAIL="${3:-}" \
    PR=412 REPO=o/r MODEL=m FM_BASE_URL=http://127.0.0.1:1 GH_TOKEN=x FM_API_KEY=x \
    "$here/qwen-review.sh" >/dev/null 2>&1 || true
  if ! grep -q "$2" "$work/post"; then
    printf 'FAIL %s\n  комментарий: %s\n' "$1" "$(cat "$work/post")"
    fail=1
  fi
}
run "нет refs/pull — голова по sha" 'diff пустой'
grep -q '+abcdef0123456789abcdef0123456789abcdef01:refs/review/head' "$work/log" || {
  echo "FAIL голова не по sha: $(cat "$work/log")"
  fail=1
}
run "git не получил голову — комментарий" 'git не получил' 1
[ "$fail" = 0 ] && echo "qwen_review_fetch: ok"
exit "$fail"
