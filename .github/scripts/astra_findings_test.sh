#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Проверка astra_findings.py (workflow astra-findings): неразобранная
# находка или строка ленты красит check run, разобранное — нет. gh подменён:
# отдаёт JSON мира через --jq самого скрипта и записывает тело check run.
set -euo pipefail
export PYTHONDONTWRITEBYTECODE=1
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
q=.
args=("$@")
path=
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[i]}" in
    --jq) q=${args[i + 1]} ;;
    repos/*) path=${args[i]} ;;
  esac
done
give() { # ФАЙЛ УМОЛЧАНИЕ
  if [ -f "$FAKE_DIR/$1" ]; then jq -c "$q" "$FAKE_DIR/$1"; else jq -cn "$2 | $q"; fi
}
case "$path" in
  repos/o/r/check-runs) cat >"$FAKE_DIR/check.json" ;;
  repos/o/r/issues\?labels=*)
    [ ! -f "$FAKE_DIR/issues.fail" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
    case "$path" in *labels=%D0%B2%D0%BD%D0%B5%D1%88%D0%BD%D1%8F%D1%8F-*) ;; *) echo "метка не закодирована: $path" >&2; exit 1 ;; esac
    give issues.json '[]' ;;
  repos/o/r/issues/20/comments*) give feed.comments.json '[]' ;;
  repos/o/r/pulls/20/reviews*) give feed.reviews.json '[]' ;;
  repos/o/r/pulls/20/comments*) give feed.review_comments.json '[]' ;;
  repos/o/r/pulls/20) jq -cn '{head: {sha: "feedhead"}} | '"$q" ;;
  repos/o/r/issues/*/comments*)
    n=${path#repos/o/r/issues/}
    give "comments.${n%%/*}.json" '[]' ;;
  *) echo "неизвестный путь $path" >&2; exit 1 ;;
esac
SH
chmod +x "$work/bin/gh"
fail=0
world() {
  export FAKE_DIR="$work/$1"
  mkdir -p "$FAKE_DIR/docs/internal/standalone"
  printf '| строка | #101 |\n' >"$FAKE_DIR/docs/internal/release-plan.md"
  printf '| D1 | #102 |\n' >"$FAKE_DIR/docs/internal/standalone/DECISIONS.md"
}
run() {
  PATH="$work/bin:$PATH" GH_REPO=o/r ASTRA_LOGIN=astra FEED=20 SINCE=2026-11-14T00:00:00Z ROOT="$FAKE_DIR" \
    python3 "$here/astra_findings.py" >"$FAKE_DIR/out" 2>"$FAKE_DIR/err" || echo $? >"$FAKE_DIR/code"
}
has() { grep -qF -- "$3" "$FAKE_DIR/$2" 2>/dev/null || { echo "FAIL $1: в $2 нет «$3»"; sed 's/^/  | /' "$FAKE_DIR/$2" 2>/dev/null || true; fail=1; }; }
lacks() { if grep -qF -- "$3" "$FAKE_DIR/$2" 2>/dev/null; then echo "FAIL $1: в $2 есть «$3»"; fail=1; fi; }

# Открытые находки: со ссылкой в плане или в DECISIONS — разобраны; без
# ссылки — красная; #1010 не считается ссылкой на #101; сводка «находок
# нет» и PR находкой не являются; текст issue в итог не идёт.
world open
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 101, "title": "[Астра] a", "state": "open"},
 {"number": 102, "title": "[Астра] b", "state": "open"},
 {"number": 1010, "title": "[Астра] СЕКРЕТНЫЙ ТЕКСТ", "state": "open"},
 {"number": 104, "title": "[Астра] Аудит #9: находок нет", "state": "open"},
 {"number": 105, "title": "[Астра] pr", "state": "open", "pull_request": {"url": "x"}}]
J
run open
has open check.json '"conclusion": "failure"'
has open check.json '"head_sha": "feedhead"'
has open check.json '#1010: открыта, нет ссылки'
lacks open check.json '#101:'
lacks open check.json '#102:'
lacks open check.json '#104'
lacks open check.json '#105'
lacks open check.json 'СЕКРЕТНЫЙ'
has open out 'неразобранного: 1'

# Закрытые находки: «закрыто» Астры — разобрана; «не закрыто» последним,
# «закрыто» не от Астры, ни слова — красные; закрытая до SINCE и закрытая
# как невыполнимая не проверяются.
world closed
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 201, "title": "a", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 202, "title": "b", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 203, "title": "c", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 204, "title": "d", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 205, "title": "e", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-13T10:00:00Z"},
 {"number": 206, "title": "f", "state": "closed", "state_reason": "not_planned", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 207, "title": "g", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"}]
J
echo '[{"user": {"login": "astra"}, "body": "Проверено пробами: закрыто."}]' >"$FAKE_DIR/comments.201.json"
echo '[{"user": {"login": "astra"}, "body": "закрыто"}, {"user": {"login": "astra"}, "body": "Не закрыто: остаток в соседнем состоянии"}]' >"$FAKE_DIR/comments.202.json"
echo '[{"user": {"login": "someone"}, "body": "закрыто"}]' >"$FAKE_DIR/comments.203.json"
echo '[{"user": {"login": "astra"}, "body": "Закрыто (пробы 1–3)."}]' >"$FAKE_DIR/comments.207.json"
run closed
has closed check.json '#202: закрыта без «закрыто» Астры'
has closed check.json '#203: закрыта без «закрыто» Астры'
has closed check.json '#204: закрыта без «закрыто» Астры'
lacks closed check.json '#201'
lacks closed check.json '#205'
lacks closed check.json '#206'
lacks closed check.json '#207'

# Лента: строка задания — из комментария, ревью и ревью-комментария; без
# более позднего «разобрано #N» от владельца или участника — красная.
# «разобрано» от самой Астры, от постороннего, раньше строки или с другим
# номером не считается. «жива» и строки до SINCE — не результаты.
world feed
cat >"$FAKE_DIR/feed.comments.json" <<'J'
[{"id": 1, "user": {"login": "astra"}, "body": "[Астра] жива: заданий 2", "created_at": "2026-11-14T08:00:00Z"},
 {"id": 2, "user": {"login": "astra"}, "body": "[Астра] задание #301 — готово", "created_at": "2026-11-14T08:10:00Z"},
 {"id": 3, "user": {"login": "owner"}, "author_association": "OWNER", "body": "разобрано: задание #301", "created_at": "2026-11-14T08:20:00Z"},
 {"id": 4, "user": {"login": "astra"}, "body": "[Астра] задание #302 — сбой", "created_at": "2026-11-14T08:30:00Z"},
 {"id": 5, "user": {"login": "astra"}, "body": "разобрано #302", "created_at": "2026-11-14T08:40:00Z"},
 {"id": 6, "user": {"login": "owner"}, "author_association": "OWNER", "body": "разобрано #3020", "created_at": "2026-11-14T08:41:00Z"},
 {"id": 7, "user": {"login": "astra"}, "body": "[Астра] задание #300 — готово", "created_at": "2026-11-13T08:00:00Z"},
 {"id": 8, "user": {"login": "owner"}, "author_association": "OWNER", "body": "разобрано #304", "created_at": "2026-11-14T08:45:00Z"},
 {"id": 13, "user": {"login": "stranger"}, "author_association": "NONE", "body": "разобрано #303", "created_at": "2026-11-14T09:10:00Z"}]
J
cat >"$FAKE_DIR/feed.reviews.json" <<'J'
[{"id": 9, "user": {"login": "astra"}, "body": "[Астра] задание #303 — готово", "submitted_at": "2026-11-14T09:00:00Z"},
 {"id": 10, "user": {"login": "astra"}, "body": "", "submitted_at": null}]
J
cat >"$FAKE_DIR/feed.review_comments.json" <<'J'
[{"id": 11, "user": {"login": "astra"}, "body": "[Астра] задание #304 — готово\nНЕ опубликовано: x", "created_at": "2026-11-14T08:44:00Z"},
 {"id": 12, "user": {"login": "astra"}, "body": "НЕ опубликовано: находка 2", "created_at": "2026-11-14T09:30:00Z"}]
J
run feed
has feed check.json 'задание #302 (комментарий 4)'
has feed check.json 'задание #303 (комментарий 9)'
has feed check.json '«НЕ опубликовано» (комментарий 12)'
lacks feed check.json '#301'
lacks feed check.json '#300'
lacks feed check.json '#304'
lacks feed check.json 'комментарий 1)'

# Всё разобрано — зелёная проверка, итог называет это словами.
world green
run green
has green check.json '"conclusion": "success"'
has green check.json 'всё разобрано'

# Список не прочитан — не вердикт: красный job с названным отказом, без
# check run.
world unread
touch "$FAKE_DIR/issues.fail"
run unread
has unread code '1'
has unread err 'сверка не прочитала список'
[ ! -e "$FAKE_DIR/check.json" ] || { echo "FAIL unread: check run поставлен"; fail=1; }

[ "$fail" = 0 ] && echo "astra_findings: ok"
exit "$fail"
