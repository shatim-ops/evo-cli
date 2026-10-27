#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# issue_labels.py on form bodies as GitHub renders them, and on a body that
# tries to smuggle a label in: only labels from its tables come out.
set -euo pipefail
export PYTHONDONTWRITEBYTECODE=1
cd "$(dirname "$0")"
fail=0
check() {
	local name=$1 want=$2 got
	got=$(python3 issue_labels.py --print | paste -sd '|' -)
	if [ "$got" = "$want" ]; then echo "ok   $name"; else echo "FAIL $name: got '$got', want '$want'"; fail=1; fi
}

check "ошибка: область, ОС, платформа, деньги" 'деньги|обл: Kubernetes|ос: Windows|платформа' <<'B'
### Версия

evo 1.0.4

### ОС

Windows

### Область

Kubernetes

### Где, по-вашему, проблема

В платформе Cloud.ru

### Ресурсы и деньги

Ресурс остался после удаления или ошибки
B

check "ошибка в CLI без денег" 'обл: сеть|ос: macOS' <<'B'
### ОС

macOS

### Область

Сеть: VPC, группы безопасности, floating IP, SNAT

### Где, по-вашему, проблема

В CLI

### Ресурсы и деньги

Ничего лишнего не создано
B

check "вопрос" 'вопрос|обл: MCP' <<'B'
### Что это

Вопрос, ответа на который нет в документации

### Область

MCP-агент (Cursor, Claude Code и др.)
B

check "догфуд: волна, платформа, деньги" 'деньги|догфуд|обл: PostgreSQL|ос: Linux|платформа' <<'B'
### Волна

1.4.0

### ОС

Linux

### Область

PostgreSQL

### Тип

Поведение платформы Cloud.ru

### Ресурсы и деньги

Ресурс остался после удаления или ошибки
B

check "догфуд: вопрос" 'вопрос|догфуд|обл: MCP' <<'B'
### Волна

1.4.0

### Область

MCP-агент (Cursor, Claude Code и др.)

### Тип

Вопрос «как сделать»
B

check "чужой текст не даёт меток" '' <<'B'
### Область

admin; gh issue close 1

### ОС

ос: Windows

### Метки

деньги
B

# The answers in the tables must be the forms' options word for word:
# a form edited alone would silently stop getting labels.
if python3 - <<'PY'
import json, re, sys
sys.path.insert(0, ".")
import issue_labels as il
def options(form):
    return set(json.loads(m) for m in re.findall(r'^ +- (?:label: )?("[^"]*")$', open(form, encoding="utf-8").read(), re.M))
miss = []
for form, table in (("bug.yml", il.AREA), ("idea.yml", il.AREA), ("dogfood.yml", il.AREA),
                    ("bug.yml", il.OS), ("checkin.yml", il.OS), ("dogfood.yml", il.OS)):
    have = options("../ISSUE_TEMPLATE/" + form)
    miss += [f"{form}: {k}" for k in table if k not in have]
# Every answer a flag reads is an option of some form.
def answers(form):  # options with or without quotes
    lines = re.findall(r'^ +- (?:label: )?(.+?)\s*$', open(form, encoding="utf-8").read(), re.M)
    return set(json.loads(l) if l.startswith('"') else l for l in lines)
every = set().union(*(answers("../ISSUE_TEMPLATE/" + f) for f in ("bug.yml", "idea.yml", "checkin.yml", "dogfood.yml")))
miss += [f"флаг {field}: {answer}" for field, answer in il.FLAGS if answer not in every]
print("\n".join(miss)); sys.exit(1 if miss else 0)
PY
then echo "ok   таблицы совпадают с формами"; else echo "FAIL таблицы и формы разошлись"; fail=1; fi

[ "$fail" = 0 ] && echo "issue_labels: все случаи верны"
exit "$fail"
