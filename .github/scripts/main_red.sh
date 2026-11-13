#!/usr/bin/env bash
# Красный прогон ci или ночного cloud-e2e на main — в одно открытое issue
# своего workflow, зелёный его закрывает. У ci — не всякий зелёный (план
# 1.9, строка «CI», решение владельца 13.11, вариант Б): только если между
# красным и зелёным коммитом менялся файл упавшего теста; иначе тест прошёл
# сам, и issue остаётся открытым с пометкой — закрывает назвавший причину;
# вызывается из .github/workflows/main-red.yml. Данные прогона — env, код PR
# не выполняется. Строки «--- FAIL» берутся из логов упавших job как есть:
# это вывод тестов этого репозитория.
# Обратные кавычки в строках — markdown, а не подстановка.
# shellcheck disable=SC2016
set -euo pipefail

REPO_DIR=${REPO_DIR:-.}
BOT='github-actions[bot]'

# have SHA — the commit is in the checkout, or comes by its sha (the
# checkout of the job is shallow).
have() {
  git -C "$REPO_DIR" cat-file -e "$1^{commit}" 2>/dev/null ||
    git -C "$REPO_DIR" fetch --quiet --no-tags origin "$1" 2>/dev/null
}

# changed RED GREEN TEST — «changed FILE», «same FILE» or «unknown»: was the
# file that holds the test changed between the red commit and the green
# one. On the night of 12–13.11 a test red on main went green by itself
# (#1576), and the same test was mended a run later (#1579): «the diff
# touches code» and «touches the package» tell the two apart no better
# than a coin — only the file of the test does. Unknown — a commit that
# cannot be had, a name that is no test of the red commit.
changed() {
  local red=$1 green=$2 name=$3 file
  case "$name" in "" | *[!A-Za-z0-9_]*) echo unknown && return 0 ;; esac
  if ! have "$red" || ! have "$green"; then echo unknown && return 0; fi
  file=$(git -C "$REPO_DIR" grep -l -E "^func $name\\(" "$red" -- '*_test.go' 2>/dev/null | head -n 1 || true)
  file=${file#"$red":}
  [ -n "$file" ] || { echo unknown && return 0; }
  if git -C "$REPO_DIR" diff --quiet "$red" "$green" -- "$file"; then echo "same $file"; else echo "changed $file"; fi
}

# main_red.sh changed RED GREEN TEST — the comparison alone: ci runs it on
# a runner against the two real episodes.
if [ "${1:-}" = changed ]; then
  changed "${2:?}" "${3:?}" "${4:?}"
  exit 0
fi

: "${GH_REPO:?}" "${RUN_ID:?}" "${RUN_URL:?}" "${CONCLUSION:?}" "${HEAD_SHA:?}" "${WORKFLOW:?}"
TITLE="main красный: $WORKFLOW"
MAX_FAILS=20
# NOTICE closes every line left in a pull request (tell_prs).
NOTICE="Эта строка — уведомление workflow main-red, а не разрешение: состояние main — issue «$TITLE» (открыт или закрыт) и последний прогон ci на main; сверяйте по ним, а не по комментарию."
# The log of a job is read up to LOG_TRIES times, LOG_RETRY_SLEEP seconds
# apart (the tests set no sleep), and the waits of all the jobs together
# stop at LOG_WAIT seconds: many red jobs whose logs never come must not
# outlast the job's timeout-minutes, which would leave no issue at all.
LOG_TRIES=${LOG_TRIES:-6}
LOG_RETRY_SLEEP=${LOG_RETRY_SLEEP:-10}
LOG_WAIT=${LOG_WAIT:-120}

# The open issues of exactly this title: the search narrows, the title
# decides (a search is by words, not by the whole title).
all=$(gh issue list --state open --search "\"$TITLE\" in:title" --limit 100 --json number,title \
  --jq ".[] | select(.title == \"$TITLE\") | .number")
open=$(head -n 1 <<<"$all")

# tell_prs ТЕКСТ — one line into every open pull request to main: an issue
# has no addressee (12.11 it stood six hours unread, plan 1.9, «CI», М5),
# and a comment in a pull request reaches the one who follows it. Only a
# red ci speaks there — it is ci that a merge leans on — and not in a
# draft, which nobody is about to merge. A comment refused is named and
# does not undo the issue.
#
# The line is a notice, not the state: the repository is public, anyone can
# write «main is green again» into a pull request, and a session used to
# the bot's line would turn auto-merge on over a red main. So each line
# names what the state is read from — the issue, open or closed, and the
# ci run on main — and says a fact, not a permission (risk note 173).
tell_prs() {
  [ "$WORKFLOW" = ci ] || return 0
  local prs n e
  e=$(mktemp)
  prs=$(gh pr list --state open --base main --limit 100 --json number,isDraft --jq '.[] | select(.isDraft | not) | .number' 2>"$e") || {
    echo "::warning title=main-red::список открытых PR не получен — строки в PR не оставлены: $(head -c 300 "$e" | tr '\n' ' ')" >&2
    rm -f "$e"
    return 0
  }
  while read -r n; do
    [ -n "$n" ] || continue
    gh pr comment "$n" --body "$1" 2>"$e" ||
      echo "::warning title=main-red::строка в PR $n не оставлена: $(head -c 300 "$e" | tr '\n' ' ')" >&2
  done <<<"$prs"
  rm -f "$e"
}

# held N ПРИЧИНА — the green run leaves the issue open and says why, once:
# while the last word of the bot there is this note, the next green run
# adds nothing.
held() {
  gh issue comment "$1" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`), но issue остаётся открытым: $2. Закрывает тот, кто назвал причину, — в тот же день (план 1.9, строка «CI», решение владельца 13.11).

<!-- main-red:held -->"
}

# settle N — close the issue of a red ci, or hold it. What was red is read
# from the marks the bot itself left there («main-red:red:SHA:Test,…»): a
# mark in somebody else's comment counts for nothing — the repository is
# public. Sets settled=closed when it closed (not printed: gh prints the
# address of every comment it makes).
settle() {
  settled=held
  local n=$1 text marks mark sha names name v unfixed="" unknown="" files="" seen=0 e
  e=$(mktemp)
  text=$(
    gh api "repos/$GH_REPO/issues/$n" --jq "select(.user.login == \"$BOT\") | .body" 2>"$e" &&
      gh api "repos/$GH_REPO/issues/$n/comments?per_page=100" --paginate --jq ".[] | select(.user.login == \"$BOT\") | .body" 2>"$e"
  ) || {
    echo "::warning title=main-red::метки issue $n не прочитаны — issue оставлен открытым: $(head -c 300 "$e" | tr '\n\r' '  ')" >&2
    rm -f "$e"
    return 0
  }
  rm -f "$e"
  # After the last red mark the bot has already said «held»: nothing new.
  case "${text##*main-red:red:}" in *main-red:held*) return 0 ;; esac
  marks=$(grep -oE 'main-red:red:[0-9a-f]+:[A-Za-z0-9_,]*' <<<"$text" || true)
  while read -r mark; do
    [ -n "$mark" ] || continue
    mark=${mark#main-red:red:}
    sha=${mark%%:*}
    names=${mark#*:}
    for name in ${names//,/ }; do
      seen=1
      v=$(changed "$sha" "$HEAD_SHA" "$name")
      case "$v" in
        changed*) files="${files:+$files, }\`${v#changed }\`" ;;
        same*) unfixed="${unfixed:+$unfixed; }$name, ${sha:0:8}…${HEAD_SHA:0:8}" ;;
        *) unknown="${unknown:+$unknown; }$name, ${sha:0:8}…${HEAD_SHA:0:8}" ;;
      esac
    done
  done <<<"$marks"
  if [ "$seen" = 0 ]; then
    held "$n" "упавший тест не назван (красным был не тест или лог не прочитан) — сравнивать нечего"
  elif [ -n "$unfixed" ]; then
    held "$n" "прошёл без правки: $unfixed — между красным и зелёным коммитом файл теста не менялся"
  elif [ -n "$unknown" ]; then
    echo "::warning title=main-red::коммиты не сравнены: $unknown" >&2
    held "$n" "сравнить не удалось: $unknown — коммита нет в репозитории или теста с таким именем в красном коммите нет"
  else
    gh issue comment "$n" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`): main снова зелёный; между красным и зелёным коммитом менялись файлы упавших тестов: $files."
    gh issue close "$n"
    settled=closed
  fi
}

if [ "$CONCLUSION" = success ]; then
  did=""
  # Every one of them: a duplicate made by hand is settled too.
  while read -r n; do
    [ -n "$n" ] || continue
    if [ "$WORKFLOW" = ci ]; then
      settle "$n"
      [ "$settled" != closed ] || did="${did:+$did }$n"
    else
      # cloud-e2e, stand, pr-stuck: no test is red there, a green run is
      # the mended state itself.
      gh issue comment "$n" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`): main снова зелёный."
      gh issue close "$n"
    fi
  done <<<"$all"
  [ -n "$open" ] || exit 0
  case " $did " in *" $open "*) tell_prs "main зелёный: issue https://github.com/$GH_REPO/issues/$open закрыт зелёным прогоном $RUN_URL (\`${HEAD_SHA:0:8}\`). $NOTICE" ;; esac
  exit 0
fi
[ "$CONCLUSION" = failure ] || exit 0

body=$(mktemp) err=$(mktemp)
red_tests=""
trap 'rm -f "$body" "$err"' EXIT
{
  printf 'Прогон %s (`%s`) красный.\n\n' "$RUN_URL" "${HEAD_SHA:0:8}"
  # Not «gh … | while»: with pipefail an early exit of the reader would
  # SIGPIPE gh; the jobs are read whole first.
  jobs=$(gh api "repos/$GH_REPO/actions/runs/$RUN_ID/jobs?per_page=100" \
    --jq '.jobs[] | select(.conclusion == "failure") | "\(.id)\t\(.name)"')
  while IFS=$'\t' read -r id name; do
    [ -n "$id" ] || continue
    printf -- '- **%s**\n' "$name"
    # The log read whole first: a refused read (403, 404, a rate limit) is
    # named as such, not taken for a log without failed tests. The log of a
    # job just ended may not be served yet: the read is tried again, and
    # why it was refused goes to this job's log (gh's error, no log text).
    log='' got=0
    for try in $(seq 1 "$LOG_TRIES"); do
      if log=$(gh api "repos/$GH_REPO/actions/jobs/$id/logs" 2>"$err"); then
        got=1
        break
      fi
      if [ "$try" = "$LOG_TRIES" ] || [ "$SECONDS" -ge "$LOG_WAIT" ]; then
        break
      fi
      sleep "$LOG_RETRY_SLEEP"
    done
    if [ "$got" = 0 ]; then
      echo "::warning title=main-red::лог job $id не получен за $try из $LOG_TRIES попыток, ${SECONDS} с из бюджета $LOG_WAIT с: $(head -c 300 "$err" | tr '\n' ' ')" >&2
      printf '  (лог job не получен — смотрите его в прогоне)\n'
      continue
    fi
    fails=$(sed -En 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ?//; /--- FAIL/p' <<<"$log" | head -n "$MAX_FAILS" || true)
    # The tests red at the top level, by name: what a green run will compare.
    red_tests="$red_tests $(sed -En 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ?//; s/^--- FAIL: (Test[A-Za-z0-9_]+).*/\1/p' <<<"$log" | tr '\n' ' ')"
    if [ -n "$fails" ]; then
      printf '\n```\n%s\n```\n' "$fails"
    else
      printf '  (строк `--- FAIL` нет — смотрите лог job)\n'
    fi
  done <<<"$jobs"
  printf '\nЧинится в тот же день, без карантина (аудит процессов 1.2.1, П2). Зелёный прогон закроет issue сам, если между красным и зелёным коммитом менялся файл упавшего теста; иначе оставит открытым с пометкой «прошёл без правки» — закрывает тот, кто назвал причину.\n'
  # shellcheck disable=SC2086 # the names are split on purpose
  printf '\n<!-- main-red:red:%s:%s -->\n' "$HEAD_SHA" "$(printf '%s\n' $red_tests | sort -u | paste -sd, -)"
} >"$body"

if [ -n "$open" ]; then
  gh issue comment "$open" --body-file "$body"
else
  url=$(gh issue create --title "$TITLE" --body-file "$body")
  echo "$url"
  tell_prs "main красный: прогон $RUN_URL (\`${HEAD_SHA:0:8}\`), разбор — в issue $url. Пока issue открыт, автослияние продуктового PR не включается, а «готово к слиянию» называет причину красного (план 1.9, строка «CI», М5). Строка идёт во все открытые PR, этот PR красным не объявлен. $NOTICE"
fi
