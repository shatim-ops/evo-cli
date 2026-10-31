#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""Labels a feedback issue from the fields of its form.

The body of an issue made from a form is "### <field>" headings with the
answer under each. The text is the reporter's, not ours: it is only
matched against fixed answers, never executed, and only labels from the
tables below are ever set.

Prefixed labels (area, OS) follow the body when it is edited. Flags
(platform, money, security, question, the dogfood wave or the QA
committee) are only added: the triage
session may set them by hand, and an edit must not take them off. A form
cannot set a label the repository lacks, so the wave's label comes from
here, created on its first use.

Every form sets only "отзыв", the one label the repository is sure to
have: GitHub drops a label of a form the repository lacks. The kind of the
form (ошибка, идея, вопрос, чек-ин), the wave (догфуд-1.6, qa) and the flags
come from here and are created on first use. A check-in gets its kind and
wave only: the triage summary counts it, area and OS are not labelled.

Usage: issue_labels.py            reads NUMBER, BODY, LABELS (JSON list)
                                  from the environment and calls gh
       issue_labels.py --print    prints the labels for BODY on stdin
"""
import json
import os
import subprocess
import sys

AREA = {
    "Установка, configure, doctor": "обл: установка",
    "Развёртывание compose (deploy, status, logs, destroy)": "обл: deploy",
    "ВМ и диски": "обл: ВМ и диски",
    "Образы и Marketplace": "обл: образы",
    "Сеть: VPC, группы безопасности, floating IP, SNAT": "обл: сеть",
    "Балансировщик NLB": "обл: NLB",
    "DNS": "обл: DNS",
    "Kubernetes": "обл: Kubernetes",
    "PostgreSQL": "обл: PostgreSQL",
    "Kafka и Redis": "обл: Kafka и Redis",
    "Object Storage (S3)": "обл: S3",
    "Секреты и сертификаты": "обл: секреты и сертификаты",
    "Container Apps и Artifact Registry": "обл: Container Apps и AR",
    "Мониторинг, биллинг, аудит, IAM": "обл: мониторинг и биллинг",
    "Стеки stack_*": "обл: стеки",
    "MCP-агент (Cursor, Claude Code и др.)": "обл: MCP",
    "Другое": "обл: другое",
}
OS = {
    "macOS": "ос: macOS",
    "Windows": "ос: Windows",
    "Linux": "ос: Linux",
    "Windows + WSL": "ос: WSL",
    "Docker-образ evo": "ос: Docker",
}
FLAGS = {
    ("Где, по-вашему, проблема", "В платформе Cloud.ru"): "платформа",
    ("Ресурсы и деньги", "Создан лишний ресурс (дубль)"): "деньги",
    ("Ресурсы и деньги", "Ресурс остался после удаления или ошибки"): "деньги",
    ("Безопасность", "Да: секрет в выводе, действие без согласия, лишние права"): "безопасность",
    ("Что это", "Идея"): "идея",
    ("Что это", "Неудобство"): "идея",
    ("Что это", "Вопрос, ответа на который нет в документации"): "вопрос",
    # The wave of a dogfood report, a check-in or a QA report.
    ("Волна", "Догфуд 1.6"): "догфуд-1.6",
    ("Волна", "Комитет QA"): "qa",
}
# LEGACY are answers of FLAGS no form offers any more: issues of an earlier
# wave keep their labels when edited. The test checks every other answer of
# FLAGS against the forms.
LEGACY = {
    ("Волна", "1.4.0"): "догфуд",
    ("Тип", "Поведение платформы Cloud.ru"): "платформа",
    ("Тип", "Вопрос «как сделать»"): "вопрос",
}
FLAGS.update(LEGACY)
# MARKS label a form by a heading only it has: a form cannot set a label the
# repository lacks, so each carries only "отзыв" and the kind comes from
# here (the test checks each heading is in exactly one form).
MARKS = {"Повторяется": "ошибка", "Период": "чек-ин"}
# A check-in is counted by the triage summary, not labelled by area or OS.
CHECKIN = "Период"
PREFIXES = ("обл: ", "ос: ")
COLORS = {"обл: ": "c5def5", "ос: ": "e4e669", "платформа": "f9d0c4", "деньги": "b60205", "вопрос": "d876e3", "догфуд": "0e8a16", "qa": "1d76db", "безопасность": "5319e7", "ошибка": "d73a4a", "идея": "a2eeef", "чек-ин": "bfdadc"}


def fields(body):
    """Maps each "### heading" of a form body to its first answer line."""
    out, key = {}, None
    for line in (body or "").splitlines():
        line = line.strip()
        if line.startswith("### "):
            key = line[4:].strip()
            continue
        if key and line and key not in out:
            out[key] = line
    return out


def wanted(body):
    f = fields(body)
    prefixed = set()
    if CHECKIN not in f:
        if f.get("Область") in AREA:
            prefixed.add(AREA[f["Область"]])
        if f.get("ОС") in OS:
            prefixed.add(OS[f["ОС"]])
    flags = {label for (field, answer), label in FLAGS.items() if f.get(field) == answer}
    flags |= {label for heading, label in MARKS.items() if heading in f}
    return prefixed, flags


def gh(*args):
    return subprocess.run(["gh", *args], check=False, capture_output=True, text=True)


def color(label):
    for key, c in COLORS.items():
        if label.startswith(key):
            return c
    return "ededed"


def main():
    if sys.argv[1:] == ["--print"]:
        prefixed, flags = wanted(sys.stdin.read())
        print("\n".join(sorted(prefixed | flags)))
        return 0
    number = os.environ["NUMBER"]
    current = set(json.loads(os.environ.get("LABELS") or "[]"))
    prefixed, flags = wanted(os.environ.get("BODY", ""))
    add = (prefixed | flags) - current
    remove = {l for l in current if l.startswith(PREFIXES) and l not in prefixed}
    for label in sorted(add):
        gh("label", "create", label, "--color", color(label))  # exists: fails, fine
    args = []
    for label in sorted(add):
        args += ["--add-label", label]
    for label in sorted(remove):
        args += ["--remove-label", label]
    if not args:
        print("метки не меняются")
        return 0
    r = gh("issue", "edit", number, *args)
    print(r.stdout + r.stderr)
    return r.returncode


if __name__ == "__main__":
    sys.exit(main())
