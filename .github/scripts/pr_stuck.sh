#!/usr/bin/env bash
# Открытый PR, который стоит молча, получает строку; вызывается из
# .github/workflows/pr-stuck.yml по расписанию. Два случая, у которых нет
# ни красной отметки, ни события для того, кто следит за PR (журнал 1.9,
# «Тихое красное»):
#   - конфликт с main: GitHub не запускает ci вовсе, проверок нет (#1574,
#     12.11);
#   - job прогона ci отменён без раннера: раннер не назначен, шагов нет —
#     не красный и не пройденный (#1536 — 2 часа, #1543 — 1,5 часа, 12.11).
# Строка — одна на случай и голову PR: метка в комментарии, по ней обход
# узнаёт, что уже сказал. Код PR не выполняется: скрипт — из main, о PR
# читаются только номер, голова и состояние слияния; текст PR в строку не
# идёт — и имена job тоже: их задаёт ci.yml ветки PR.
set -euo pipefail

: "${GH_REPO:?}"
# NOTICE closes every line: the line is a notice, the state is the list of
# checks of the pull request — anyone can write a comment in a public
# repository (risk note 173, the same rule as main_red.sh).
NOTICE="Эта строка — уведомление workflow pr-stuck, а не состояние PR: сверяйте по списку проверок PR на его голове."
err=$(mktemp)
trap 'rm -f "$err"' EXIT

# tell N МЕТКА ТЕКСТ — the line, unless the pull request already carries
# the mark in a comment of the bot itself: anyone can comment in a public
# repository, and a mark written by somebody else must not silence the
# line (risk note 174). Comments not read: no line (it would repeat every
# round), the refusal is named.
tell() {
  local n=$1 mark="<!-- pr-stuck:$2 -->" seen
  seen=$(gh api "repos/$GH_REPO/issues/$n/comments?per_page=100" --paginate \
    --jq '.[] | select(.user.login == "github-actions[bot]") | .body' 2>"$err") || {
    echo "::warning title=pr-stuck::комментарии PR $n не прочитаны — строка не оставлена: $(head -c 300 "$err" | tr '\n\r' '  ')" >&2
    return 0
  }
  case "$seen" in *"$mark"*) return 0 ;; esac
  gh pr comment "$n" --body "$3 $NOTICE

$mark" 2>"$err" ||
    echo "::warning title=pr-stuck::строка в PR $n не оставлена: $(head -c 300 "$err" | tr '\n\r' '  ')" >&2
}

# The request limit of the token is one for the repository and the hour,
# and main-red and the review spend it too. A round that meets it says so
# and ends green: a red round would have main-red open an issue with the
# same spent token (risk note 178). Any other refusal of the list is red.
prs=$(gh pr list --state open --base main --limit 100 --json number,isDraft,headRefOid,mergeable \
  --jq '.[] | select(.isDraft | not) | "\(.number)\t\(.headRefOid)\t\(.mergeable)"' 2>"$err") || {
  if grep -qi 'rate limit' "$err"; then
    echo "::warning title=pr-stuck::лимит запросов исчерпан — обход пропущен до следующего круга: $(head -c 300 "$err" | tr '\n\r' '  ')" >&2
    exit 0
  fi
  cat "$err" >&2
  exit 1
}

while IFS=$'\t' read -r n sha mergeable; do
  [ -n "$n" ] || continue
  # UNKNOWN is not a conflict: GitHub computes mergeable lazily, the next
  # round reads the computed value.
  if [ "$mergeable" = CONFLICTING ]; then
    tell "$n" "conflict:$sha" "У PR конфликт с main (голова \`${sha:0:8}\`): пока он есть, GitHub не запускает ci, и проверки на этой голове не появятся. Автор вливает main merge-коммитом (CLAUDE.md, §0, правило 2)."
    continue
  fi
  # The latest ci run of this head: a run of an older head was cut by the
  # push that replaced it, which is no «no runner».
  run=$(gh api "repos/$GH_REPO/actions/runs?head_sha=$sha&event=pull_request&per_page=100" \
    --jq '[.workflow_runs[] | select(.name == "ci")] | max_by(.run_number) | .id // empty' 2>"$err") || {
    echo "::warning title=pr-stuck::прогоны PR $n не прочитаны: $(head -c 300 "$err" | tr '\n\r' '  ')" >&2
    continue
  }
  [ -n "$run" ] || continue
  jobs=$(gh api "repos/$GH_REPO/actions/runs/$run/jobs?per_page=100" \
    --jq '[.jobs[] | select(.conclusion == "cancelled" and ((.runner_id // 0) == 0) and (((.steps // []) | length) == 0))] | length' 2>"$err") || {
    echo "::warning title=pr-stuck::job прогона $run (PR $n) не прочитаны: $(head -c 300 "$err" | tr '\n\r' '  ')" >&2
    continue
  }
  case "$jobs" in "" | 0 | *[!0-9]*) continue ;; esac
  # The count, not the names: a job is named in the ci.yml of the pull
  # request's own branch, so its name is text of the author of the pull
  # request, and this line is signed by the bot (risk note 174). «Похоже»:
  # the shape of «no runner» is known from one run so far.
  tell "$n" "norunner:$sha" "Прогон ci https://github.com/$GH_REPO/actions/runs/$run (голова \`${sha:0:8}\`): job без раннера: $jobs — отменены, раннер не назначен, ни одного шага; похоже на отмену без раннера. Проверка не красная, но и не пройдена: PR стоит. Какие job — в прогоне. Перезапуск таких job — за «N — релизом», в счёт перезапусков не идёт (CLAUDE.md, §0, правило 2)."
done <<<"$prs"
