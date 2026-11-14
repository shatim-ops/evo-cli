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
# Списки — только с --paginate: на заглушке всё умещается в одну страницу,
# и забытая пагинация иначе прошла бы (ревью Qwen #1873).
case "$path" in
  repos/o/r/check-runs | repos/o/r/pulls/20) ;;
  repos/o/r/issues/*/* | repos/o/r/issues\?* | repos/o/r/pulls/*/*) [[ " $* " == *" --paginate "* ]] || { echo "нет --paginate: $path" >&2; exit 1; } ;;
  repos/o/r/issues/*) ;;
  *) [[ " $* " == *" --paginate "* ]] || { echo "нет --paginate: $path" >&2; exit 1; } ;;
esac
case "$path" in
  repos/o/r/check-runs) cat >"$FAKE_DIR/check.json" ;;
  repos/o/r/issues\?labels=*)
    [ ! -f "$FAKE_DIR/issues.fail" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
    case "$path" in *labels=%D0%B2%D0%BD%D0%B5%D1%88%D0%BD%D1%8F%D1%8F-*) ;; *) echo "метка не закодирована: $path" >&2; exit 1 ;; esac
    give issues.json '[]' ;;
  repos/o/r/issues/20/comments*)
    if [ -f "$FAKE_DIR/feed.comments.raw" ]; then cat "$FAKE_DIR/feed.comments.raw"; else give feed.comments.json '[]'; fi ;;
  repos/o/r/pulls/20/reviews*) give feed.reviews.json '[]' ;;
  repos/o/r/pulls/20/comments*) give feed.review_comments.json '[]' ;;
  repos/o/r/pulls/20) jq -cn '{head: {sha: "feedhead"}} | '"$q" ;;
  repos/o/r/issues/*/comments*)
    n=${path#repos/o/r/issues/}
    give "comments.${n%%/*}.json" '[]' ;;
  repos/o/r/issues/*)
    n=${path#repos/o/r/issues/}
    give "issue.$n.json" '{"number": 0, "title": "", "state": "open"}' ;;
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
  PATH="$work/bin:$PATH" GH_REPO=o/r ASTRA_LOGIN=astra FEED=20 SINCE=2026-11-14T00:00:00Z \
    ASTRA_ACCOUNT_SINCE=2026-11-14T07:30:00Z ROOT="$FAKE_DIR" \
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
has closed check.json '#202: закрыта, последний вердикт Астры — «не закрыто»'
has closed check.json '#203: закрыта без «закрыто» Астры'
has closed check.json '#204: закрыта без «закрыто» Астры'
lacks closed check.json '#201'
lacks closed check.json '#205'
lacks closed check.json '#206'
lacks closed check.json '#207'

# Признаки «релиза (4)» на данных 14.11.
# (1) До своего аккаунта Астра публиковала от владельца, «[Астра] …»: её
# вердикт — в отчёте задания (метка астра-задание), на строке с номером
# находки; владелец в находке называет задание. После ASTRA_ACCOUNT_SINCE
# «[Астра]» от владельца вердиктом не считается.
# (2) «Не закрыто» с остатком — открытой находкой из плана — разобрано.
# (3) Отчёт задания с меткой «отчёт» — не находка.
world relay
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 701, "title": "a", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 702, "title": "b", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 703, "title": "c", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 704, "title": "d", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 101, "title": "rest", "state": "open", "labels": [{"name": "внешняя-проверка"}]},
 {"number": 1011, "title": "rest off plan", "state": "open", "labels": [{"name": "внешняя-проверка"}]},
 {"number": 706, "title": "report", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z", "labels": [{"name": "внешняя-проверка"}, {"name": "отчёт"}]}]
J
printf '| строка | #101 |\n' >"$FAKE_DIR/docs/internal/release-plan.md"
echo '{"number": 800, "title": "task", "state": "closed", "labels": [{"name": "астра-задание"}]}' >"$FAKE_DIR/issue.800.json"
echo '{"number": 801, "title": "not a task", "state": "closed", "labels": []}' >"$FAKE_DIR/issue.801.json"
cat >"$FAKE_DIR/comments.800.json" <<'J'
[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T06:00:00Z", "body": "[Астра] Отчёт по заданию.\n- **#701 — закрыто по результатам проверки**\n- #702 — не закрыто: остаток"}]
J
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T06:00:00Z", "body": "[Астра] #703 — закрыто"}]' >"$FAKE_DIR/comments.801.json"
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T07:00:00Z", "body": "Закрыто исправлением. Проверила Астра, задание #800."}]' >"$FAKE_DIR/comments.701.json"
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T07:00:00Z", "body": "Закрыто. Задание #800."}]' >"$FAKE_DIR/comments.702.json"
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T08:00:00Z", "body": "[Астра] закрыто"}, {"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T08:01:00Z", "body": "Закрыто, задание #801"}]' >"$FAKE_DIR/comments.703.json"
echo '[{"user": {"login": "astra"}, "author_association": "NONE", "created_at": "2026-11-14T09:00:00Z", "body": "Не закрыто полностью: остаток — #101"}]' >"$FAKE_DIR/comments.704.json"
run relay
lacks relay check.json '#701'
has relay check.json '#702: закрыта без «закрыто» Астры'
has relay check.json '#703: закрыта без «закрыто» Астры'
lacks relay check.json '#704'
lacks relay check.json '#706'
# Остаток вне плана — не разобрано.
world rest
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 704, "title": "d", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"},
 {"number": 1011, "title": "rest off plan", "state": "open", "labels": [{"name": "внешняя-проверка"}]}]
J
echo '[{"user": {"login": "astra"}, "author_association": "NONE", "created_at": "2026-11-14T09:00:00Z", "body": "Не закрыто полностью: остаток — #1011"}]' >"$FAKE_DIR/comments.704.json"
run rest
has rest check.json '#704: закрыта, последний вердикт Астры — «не закрыто»'

# (4) «НЕ опубликовано» в задании, которое уже «готово»: разобрано, когда
# после строки участник назвал в задании находку, куда она легла, и та
# разобрана (#1866 → #1830); находка вне плана или имя постороннего — нет.
world landed
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 101, "title": "landed", "state": "open", "labels": [{"name": "внешняя-проверка"}]},
 {"number": 1012, "title": "off plan", "state": "open", "labels": [{"name": "внешняя-проверка"}]}]
J
cat >"$FAKE_DIR/feed.comments.json" <<'J'
[{"id": 31, "user": {"login": "astra"}, "body": "[Астра] задание #900 — готово\nНЕ опубликовано: finding.md", "created_at": "2026-11-14T10:00:00Z"},
 {"id": 32, "user": {"login": "astra"}, "body": "[Астра] задание #901 — готово\nНЕ опубликовано: finding.md", "created_at": "2026-11-14T10:00:00Z"},
 {"id": 33, "user": {"login": "astra"}, "body": "[Астра] задание #902 — готово\nНЕ опубликовано: finding.md", "created_at": "2026-11-14T10:00:00Z"}]
J
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T11:00:00Z", "body": "Разбор queue.log: находка легла в #101."}]' >"$FAKE_DIR/comments.900.json"
echo '[{"user": {"login": "owner"}, "author_association": "OWNER", "created_at": "2026-11-14T11:00:00Z", "body": "Легла в #1012."}]' >"$FAKE_DIR/comments.901.json"
echo '[{"user": {"login": "stranger"}, "author_association": "NONE", "created_at": "2026-11-14T11:00:00Z", "body": "Легла в #101."}]' >"$FAKE_DIR/comments.902.json"
run landed
lacks landed check.json 'задание #900'
has landed check.json 'задание #901 (комментарий 32): сбой'
has landed check.json 'задание #902 (комментарий 33): сбой'

# Лента: строка разобрана, когда разобрано то, что она называет (решение
# «релиза (4)», ручной отметки нет). Строки — из комментария, ревью и
# ревью-комментария; «жива» и строки до SINCE — не результаты, как и
# строки не от Астры.
world feed
cat >"$FAKE_DIR/issues.json" <<'J'
[{"number": 101, "title": "a", "state": "open"},
 {"number": 401, "title": "b", "state": "open"},
 {"number": 402, "title": "c", "state": "closed", "state_reason": "completed", "closed_at": "2026-11-14T10:00:00Z"}]
J
echo '[{"user": {"login": "astra"}, "body": "закрыто"}]' >"$FAKE_DIR/comments.402.json"
echo '{"number": 501, "title": "t", "state": "open"}' >"$FAKE_DIR/issue.501.json"
echo '{"number": 502, "title": "t", "state": "closed"}' >"$FAKE_DIR/issue.502.json"
cat >"$FAKE_DIR/feed.comments.json" <<'J'
[{"id": 1, "user": {"login": "astra"}, "body": "[Астра] жива: заданий 2", "created_at": "2026-11-14T08:00:00Z"},
 {"id": 2, "user": {"login": "astra"}, "body": "[Астра] задание #301 — готово\n- находка: https://github.com/o/r/issues/101\n- находка: https://github.com/o/r/issues/402\n- комментарий в #101\n- комментарий в #502", "created_at": "2026-11-14T08:10:00Z"},
 {"id": 3, "user": {"login": "astra"}, "body": "[Астра] задание #302 — готово\n- находка: https://github.com/o/r/issues/401\n- комментарий в #501", "created_at": "2026-11-14T08:20:00Z"},
 {"id": 4, "user": {"login": "astra"}, "body": "[Астра] задание #303 — сбой", "created_at": "2026-11-14T08:30:00Z"},
 {"id": 5, "user": {"login": "astra"}, "body": "[Астра] задание #304 — сбой", "created_at": "2026-11-14T08:31:00Z"},
 {"id": 6, "user": {"login": "astra"}, "body": "[Астра] задание #304 — готово", "created_at": "2026-11-14T08:40:00Z"},
 {"id": 7, "user": {"login": "astra"}, "body": "[Астра] задание #300 — готово\n- комментарий в #501", "created_at": "2026-11-13T08:00:00Z"},
 {"id": 8, "user": {"login": "owner"}, "body": "[Астра] задание #309 — готово\n- комментарий в #501", "created_at": "2026-11-14T08:45:00Z"}]
J
cat >"$FAKE_DIR/feed.reviews.json" <<'J'
[{"id": 9, "user": {"login": "astra"}, "body": "[Астра] задание #305 — готово\n- находка: https://github.com/o/internal/issues/7", "submitted_at": "2026-11-14T09:00:00Z"},
 {"id": 10, "user": {"login": "astra"}, "body": "", "submitted_at": null}]
J
cat >"$FAKE_DIR/feed.review_comments.json" <<'J'
[{"id": 11, "user": {"login": "astra"}, "body": "[Астра] задание #306 — готово\nНЕ опубликовано: находка 2", "created_at": "2026-11-14T08:44:00Z"},
 {"id": 12, "user": {"login": "astra"}, "body": "НЕ опубликовано: находка 2", "created_at": "2026-11-14T09:30:00Z"}]
J
run feed
# Разобрано: находка в плане, находка закрыта с «закрыто», комментарий в
# issue из плана и в закрытый.
lacks feed check.json 'задание #301'
# Находка без строки плана и комментарий в открытый issue вне плана.
has feed check.json 'задание #302 (комментарий 3): находка #401 — открыта, нет ссылки'
has feed check.json 'задание #302 (комментарий 3): комментарий в #501 — #501 открыт и не назван в плане'
# Сбой без повтора — красный; сбой, за которым «готово» того же задания, — нет.
has feed check.json 'задание #303 (комментарий 4): сбой или «НЕ опубликовано»'
lacks feed check.json 'задание #304'
# «НЕ опубликовано» в строке задания — красная, пока нет «готово» без него.
has feed check.json 'задание #306 (комментарий 11): сбой или «НЕ опубликовано»'
# Без номера задания — из данных не выводится.
has feed check.json 'лента (комментарий 12): «НЕ опубликовано» без номера задания'
# Находка в другом репозитории — названа, но проверку не красит.
has feed check.json 'находка в o/internal #7 — этот токен её не читает'
lacks feed check.json 'находка #7'
# До SINCE и не от Астры — не строки ленты.
lacks feed check.json 'задание #300'
lacks feed check.json 'задание #309'

# Имя репозитория в ссылке — без учёта регистра (ревью Qwen): находка
# этого репозитория проверяется, а не уходит в непроверяемые.
world case
cat >"$FAKE_DIR/feed.comments.json" <<'J'
[{"id": 21, "user": {"login": "astra"}, "body": "[Астра] задание #310 — готово\n- находка: https://github.com/O/R/issues/601", "created_at": "2026-11-14T08:10:00Z"}]
J
echo '{"number": 601, "title": "t", "state": "open"}' >"$FAKE_DIR/issue.601.json"
run case
has case check.json 'задание #310 (комментарий 21): находка #601 — открыта'
lacks case check.json 'этот токен её не читает'

# Ответ не JSON (страница ошибки) — красный job с названным отказом, без
# traceback и без check run (ревью Qwen).
world html
echo '<html>503</html>' >"$FAKE_DIR/feed.comments.raw"
run html
has html err 'ответ не JSON'
lacks html err 'Traceback'
[ ! -e "$FAKE_DIR/check.json" ] || { echo "FAIL html: check run поставлен"; fail=1; }

# Только непроверяемое — проверка зелёная, но список в итоге есть.
world external
cat >"$FAKE_DIR/feed.reviews.json" <<'J'
[{"id": 9, "user": {"login": "astra"}, "body": "[Астра] задание #305 — готово\n- находка: https://github.com/o/internal/issues/7", "submitted_at": "2026-11-14T09:00:00Z"}]
J
run external
has external check.json '"conclusion": "success"'
has external check.json 'Не выводится из данных'

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
