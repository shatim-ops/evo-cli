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
#     в FEED после SINCE) «задание #N — готово», у которой названная
#     находка этого репозитория не разобрана (как выше) или issue/PR из
#     «комментарий в #M» открыт и не назван в плане; «сбой» или
#     «НЕ опубликовано» — пока позже нет «готово» того же задания; без
#     номера задания — всегда (из данных не выводится). Ручной отметки нет
#     (решение «1.9 — релиза (4)», 14.11).
#
# Итог — check run ASTRA_CHECK на голову ленты: есть неразобранное —
# failure со списком, нет — success. «Релиз» подписан на ленту, красная
# проверка до него доходит. В итог идут только номера issue, заданий и
# комментариев, без текста issue и комментариев: метку на issue ставят
# только те, у кого есть запись, но текст ленты может написать любой.
# Находки в evo-internal этот токен не читает: итог их перечисляет, не
# краснея, сверяет «релиз» (п. 4).
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
ISSUE_URL = re.compile(r"https://github\.com/([\w.-]+)/([\w.-]+)/issues/(\d+)")
COMMENT_IN = re.compile(r"комментарий в #(\d+)")


def gh_lines(*args):
    """gh api with --paginate and a --jq that prints one JSON per line."""
    r = subprocess.run(["gh", "api", *args], check=False, capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(f"gh api {args[0]}: {r.stderr.strip()[:300]}")
    try:
        return [json.loads(l) for l in r.stdout.splitlines() if l.strip()]
    except ValueError as e:
        # Not JSON (an error page): a list not read, said without the text.
        raise RuntimeError(f"gh api {args[0]}: ответ не JSON ({type(e).__name__})") from None


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
    """The unprocessed findings and feed lines, and what the data cannot tell."""
    out, unchecked = [], []
    refs = plan_refs(root)
    issues = {
        i["number"]: i
        for i in gh_lines(
            "--paginate",
            f"repos/{repo}/issues?labels={urllib.parse.quote(LABEL)}&state=all&per_page=100",
            "--jq",
            ".[] | {number, title, state, state_reason, closed_at, pull_request}",
        )
    }

    def issue(n):
        # An issue or pull request named by a feed line but not labelled.
        if n not in issues:
            issues[n] = gh_lines(
                f"repos/{repo}/issues/{n}",
                "--jq",
                "{number, title, state, state_reason, closed_at, pull_request}",
            )[0]
        return issues[n]

    verdict = {}

    def astra_closed(n):
        if n not in verdict:
            comments = gh_lines(
                "--paginate",
                f"repos/{repo}/issues/{n}/comments?per_page=100",
                "--jq",
                ".[] | {login: .user.login, body}",
            )
            said = [c["body"] for c in comments if c["login"] == astra and "закрыто" in c["body"].lower()]
            last = said[-1] if said else ""
            verdict[n] = bool(CLOSED_RE.search(last)) and "не закрыто" not in last.lower()
        return verdict[n]

    def finding_problem(n):
        """Why finding n is not processed; "" when it is."""
        i = issue(n)
        if i["state"] == "open":
            return "" if n in refs else "открыта, нет ссылки в release-plan.md или DECISIONS.md на main"
        # Закрыта как невыполнимая (дубль, опровергнута) — не проверка
        # исправления; решение о ней — в самом issue. Закрытые до правила —
        # тоже.
        if i.get("state_reason") == "not_planned" or (i.get("closed_at") or "") < since:
            return ""
        return "" if astra_closed(n) else "закрыта без «закрыто» Астры"

    for n in sorted(n for n, i in list(issues.items()) if is_finding(i)):
        if why := finding_problem(n):
            out.append(f"#{n}: {why}")

    # The feed: issue comments, review bodies and review comments of the
    # draft pull request, in time order. A line is processed when what it
    # names is: each finding of this repository processed (above), each
    # issue or pull request it commented on named in the plan or closed; a
    # failed or unpublished task — when a later line of the same task is
    # done and published. Nothing is marked by hand (decision of «1.9 —
    # релиз (4)», 14.11).
    lines = []
    for path in (f"issues/{feed}/comments", f"pulls/{feed}/reviews", f"pulls/{feed}/comments"):
        when = ".submitted_at" if path.endswith("reviews") else ".created_at"
        lines += gh_lines(
            "--paginate",
            f"repos/{repo}/{path}?per_page=100",
            "--jq",
            f".[] | {{id, login: .user.login, body: (.body // \"\"), at: ({when} // \"\")}}",
        )
    lines = sorted((c for c in lines if c["login"] == astra and c["at"] >= since), key=lambda c: c["at"])
    for k, c in enumerate(lines):
        body = c["body"]
        m = TASK_RE.search(body)
        failed = NOT_PUBLISHED in body or (m is not None and m.group(2).startswith("сбой"))
        if not m and not failed:
            continue  # «жива» and other lines that are not results
        where = f"лента, задание #{m.group(1)}" if m else "лента"
        where += f" (комментарий {c['id']})"
        if failed:
            if not m:
                out.append(f"{where}: «НЕ опубликовано» без номера задания — из данных не выводится")
                continue
            redone = any(
                (d := TASK_RE.search(e["body"])) and d.group(1) == m.group(1) and d.group(2).startswith("готово")
                and NOT_PUBLISHED not in e["body"]
                for e in lines[k + 1 :]
            )
            if not redone:
                out.append(f"{where}: сбой или «НЕ опубликовано», позже нет «готово» того же задания")
            continue
        for owner, name, n in ISSUE_URL.findall(body):
            n = int(n)
            # GitHub names are not case-sensitive.
            if f"{owner}/{name}".lower() != repo.lower():
                unchecked.append(f"{where}: находка в {owner}/{name} #{n} — этот токен её не читает")
                continue
            if why := finding_problem(n):
                out.append(f"{where}: находка #{n} — {why}")
        for n in sorted({int(n) for n in COMMENT_IN.findall(body)}):
            if n not in refs and issue(n)["state"] != "closed":
                out.append(f"{where}: комментарий в #{n} — #{n} открыт и не назван в плане")
    return out, unchecked


def main():
    repo = os.environ["GH_REPO"]
    astra = os.environ["ASTRA_LOGIN"]
    feed = os.environ["FEED"]
    since = os.environ["SINCE"]
    root = os.environ.get("ROOT", ".")
    name = os.environ.get("ASTRA_CHECK", "astra-findings")
    try:
        left, unchecked = unprocessed(repo, astra, feed, since, root)
    except RuntimeError as e:
        # A list not read is no verdict: red, with the refusal named.
        print(f"::error title=astra-findings::сверка не прочитала список: {e}", file=sys.stderr)
        return 1
    title = f"неразобранного: {len(left)}" if left else "всё разобрано"
    summary = "\n".join(f"- {l}" for l in left) or "Неразобранных находок и строк ленты нет."
    if unchecked:
        # Not red: the token of this repository cannot read them; «релиз»
        # reconciles them by hand (plan, item 4).
        summary += "\n\nНе выводится из данных, сверяет «релиз»:\n" + "\n".join(f"- {l}" for l in unchecked)
    print(f"сверка: {title}")
    if left or unchecked:
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
