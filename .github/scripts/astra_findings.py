#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Сверка находок внешней проверки (план 1.9, строка «Результаты Астры не
# теряются», п. 3; решение владельца 14.11). Вызывается из
# .github/workflows/astra-findings.yml — по событиям issue, комментариев и
# ревью и раз в час. Полный список, а не одно событие: за 14.11 из 36
# находок у 12 не было ни строки плана, ни хозяина, а строка ленты,
# опубликованная ревью-комментарием, «релиза» не разбудила.
#
# Неразобранное — три вида:
#   - открытая находка (issue с меткой «внешняя-проверка») без ссылки #N в
#     docs/internal/release-plan.md или docs/internal/standalone/DECISIONS.md
#     на main — у неё нет хозяина и срока;
#   - находка, закрытая (как выполненная) после SINCE без «закрыто» от
#     аккаунта Астры в её комментариях — закрыта без проверки (п. 2);
#   - строка ленты (комментарий, ревью или ревью-комментарий аккаунта Астры
#     в FEED после SINCE) «задание #N — …», за которой нет более позднего
#     комментария в ленте от владельца или участника репозитория (не от
#     Астры) со словом «разобрано» и «#N»; строка «НЕ опубликовано» без
#     номера задания — так же, без «#N».
#
# Итог — check run ASTRA_CHECK на голову ленты: есть неразобранное —
# failure со списком, нет — success. «Релиз» подписан на ленту, красная
# проверка до него доходит. В итог идут только номера issue, заданий и
# комментариев, без текста issue и комментариев: метку на issue ставят
# только те, у кого есть запись, но текст ленты может написать любой.
# Находки в evo-internal этот токен не читает: их сверяет «релиз» (п. 4).
#
# --print — итог в stdout без check run (проверка и ручной запуск).
import json
import os
import re
import subprocess
import sys
import urllib.parse

LABEL = "внешняя-проверка"
PLAN_FILES = ["docs/internal/release-plan.md", "docs/internal/standalone/DECISIONS.md"]

# «закрыто», но не «не закрыто» и не часть другого слова.
CLOSED_RE = re.compile(r"(?<![А-Яа-яЁё])(?<!не )закрыто", re.IGNORECASE)
TASK_RE = re.compile(r"задание\s+#(\d+)\s+—\s+(\S+)")
NOT_PUBLISHED = "НЕ опубликовано"
# Who may mark a feed line «разобрано»: anyone can comment in a public
# repository, and a stranger's «разобрано» must not silence a line.
WRITERS = {"OWNER", "MEMBER", "COLLABORATOR"}


def gh_lines(*args):
    """gh api with --paginate and a --jq that prints one JSON per line."""
    r = subprocess.run(["gh", "api", *args], check=False, capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(f"gh api {args[0]}: {r.stderr.strip()[:300]}")
    return [json.loads(l) for l in r.stdout.splitlines() if l.strip()]


def plan_refs(root):
    text = ""
    for f in PLAN_FILES:
        with open(os.path.join(root, f), encoding="utf-8") as fh:
            text += fh.read()
    return {int(n) for n in re.findall(r"#(\d+)(?!\d)", text)}


def is_finding(issue):
    # «Аудит: находок нет» — сводка, а не находка; её закрывает «релиз».
    return issue.get("pull_request") is None and "находок нет" not in issue.get("title", "")


def unprocessed(repo, astra, feed, since, root):
    out = []
    refs = plan_refs(root)
    issues = gh_lines(
        "--paginate",
        f"repos/{repo}/issues?labels={urllib.parse.quote(LABEL)}&state=all&per_page=100",
        "--jq",
        ".[] | {number, title, state, state_reason, closed_at, pull_request}",
    )
    for i in sorted((i for i in issues if is_finding(i)), key=lambda i: i["number"]):
        n = i["number"]
        if i["state"] == "open":
            if n not in refs:
                out.append(f"#{n}: открыта, нет ссылки в release-plan.md или DECISIONS.md на main")
            continue
        # Закрыта как невыполнимая (дубль, опровергнута) — не проверка
        # исправления; решение о ней — в самом issue.
        if i.get("state_reason") == "not_planned" or (i.get("closed_at") or "") < since:
            continue
        comments = gh_lines(
            "--paginate",
            f"repos/{repo}/issues/{n}/comments?per_page=100",
            "--jq",
            ".[] | {login: .user.login, body}",
        )
        verdicts = [c["body"] for c in comments if c["login"] == astra and "закрыто" in c["body"].lower()]
        last = verdicts[-1] if verdicts else ""
        if not CLOSED_RE.search(last) or "не закрыто" in last.lower():
            out.append(f"#{n}: закрыта без «закрыто» Астры")

    # The feed: issue comments, review bodies and review comments of the
    # draft pull request, in time order.
    lines = []
    for path in (f"issues/{feed}/comments", f"pulls/{feed}/reviews", f"pulls/{feed}/comments"):
        when = ".submitted_at" if path.endswith("reviews") else ".created_at"
        lines += gh_lines(
            "--paginate",
            f"repos/{repo}/{path}?per_page=100",
            "--jq",
            f".[] | {{id, login: .user.login, assoc: (.author_association // \"\"), body: (.body // \"\"), at: ({when} // \"\")}}",
        )
    lines.sort(key=lambda c: c["at"])
    for k, c in enumerate(lines):
        if c["login"] != astra or c["at"] < since:
            continue
        m = TASK_RE.search(c["body"])
        if not m and NOT_PUBLISHED not in c["body"]:
            continue  # «жива» and other lines that are not results
        later = [
            d["body"]
            for d in lines[k + 1 :]
            if d["login"] != astra and d["assoc"] in WRITERS and "разобрано" in d["body"].lower()
        ]
        if m:
            task = m.group(1)
            if not any(re.search(rf"#{task}(?!\d)", b) for b in later):
                out.append(f"лента, задание #{task} (комментарий {c['id']}): нет «разобрано #{task}»")
        elif not later:
            out.append(f"лента, «НЕ опубликовано» (комментарий {c['id']}): нет «разобрано»")
    return out


def main():
    repo = os.environ["GH_REPO"]
    astra = os.environ["ASTRA_LOGIN"]
    feed = os.environ["FEED"]
    since = os.environ["SINCE"]
    root = os.environ.get("ROOT", ".")
    name = os.environ.get("ASTRA_CHECK", "astra-findings")
    try:
        left = unprocessed(repo, astra, feed, since, root)
    except RuntimeError as e:
        # A list not read is no verdict: red, with the refusal named.
        print(f"::error title=astra-findings::сверка не прочитала список: {e}", file=sys.stderr)
        return 1
    title = f"неразобранного: {len(left)}" if left else "всё разобрано"
    summary = "\n".join(f"- {l}" for l in left) or "Неразобранных находок и строк ленты нет."
    print(f"сверка: {title}")
    if left:
        print(summary)
    if "--print" in sys.argv:
        return 0
    head = gh_lines(f"repos/{repo}/pulls/{feed}", "--jq", "{sha: .head.sha}")[0]["sha"]
    body = {
        "name": name,
        "head_sha": head,
        "status": "completed",
        "conclusion": "failure" if left else "success",
        "output": {"title": title, "summary": summary},
    }
    r = subprocess.run(
        ["gh", "api", "-X", "POST", f"repos/{repo}/check-runs", "--input", "-"],
        input=json.dumps(body, ensure_ascii=False),
        check=False,
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        print(f"::error title=astra-findings::check run не поставлен: {r.stderr.strip()[:300]}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
