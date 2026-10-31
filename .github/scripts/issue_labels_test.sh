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

check "ошибка: область, ОС, платформа, деньги, безопасность" 'безопасность|деньги|обл: Kubernetes|ос: Windows|ошибка|платформа' <<'B'
### Версия

evo CMCP 1.6.0

### ОС

Windows

### Область

Kubernetes

### Где, по-вашему, проблема

В платформе Cloud.ru

### Ресурсы и деньги

Ресурс остался после удаления или ошибки

### Безопасность

Да: секрет в выводе, действие без согласия, лишние права

### Повторяется

Всегда
B

check "ошибка в evo без денег" 'обл: сеть|ос: macOS|ошибка' <<'B'
### ОС

macOS

### Область

Сеть: VPC, группы безопасности, floating IP, SNAT

### Где, по-вашему, проблема

В evo

### Ресурсы и деньги

Ничего лишнего не создано

### Безопасность

Нет

### Повторяется

Один раз
B

check "идея" 'идея|обл: DNS' <<'B'
### Что это

Неудобство

### Трек

Т2 — агент, только MCP

### Область

DNS
B

check "вопрос" 'вопрос|обл: MCP' <<'B'
### Что это

Вопрос, ответа на который нет в документации

### Область

MCP-агент (Cursor, Claude Code и др.)
B

check "отчёт о сценарии: волна, деньги" 'деньги|догфуд-1.6|ос: Linux' <<'B'
### Волна

Догфуд 1.6

### Сценарий

С4 — убрать всё созданное

### ОС

Linux

### Ресурсы и деньги

Создан лишний ресурс (дубль)

### Безопасность

Не знаю
B

check "отчёт QA" 'qa|ос: macOS' <<'B'
### Волна

Комитет QA

### Группа каталога

vpc

### ОС

macOS

### Ресурсы и деньги

Ничего лишнего не создано
B

check "чек-ин: только вид и волна" 'догфуд-1.6|чек-ин' <<'B'
### Волна

Догфуд 1.6

### Период

Неделя

### ОС

Windows

### Область

DNS
B

check "issue волны 1.4.0 при правке сохраняет метки" 'догфуд|платформа' <<'B'
### Волна

1.4.0

### Тип

Поведение платформы Cloud.ru
B

check "чужой текст не даёт меток" '' <<'B'
### Область

admin; gh issue close 1

### ОС

ос: Windows

### Метки

деньги
B

# The tables against the forms, word for word: a form edited alone would
# silently stop getting labels.
if python3 - <<'PY'
import glob, json, os, re, sys
sys.path.insert(0, ".")
import issue_labels as il

def parse(path):
    """The labels of a form and its fields with their options, from the
    layout of these forms (no YAML module on the runner)."""
    labels, fields, cur = None, {}, None
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        val = lambda v: json.loads(v) if v.startswith('"') else v
        if line.startswith("labels:"):
            labels = json.loads(line.split(":", 1)[1])
        elif line.startswith("  - type:"):
            cur = None
        elif m := re.match(r"^      label: (.+)$", line):
            cur = val(m.group(1).strip())
            fields[cur] = []
        elif cur is not None and (m := re.match(r"^        - (?:label: )?(.+)$", line)):
            fields[cur].append(val(m.group(1).strip()))
    return {"labels": labels, "fields": fields}

forms = {os.path.basename(f): parse(f)
         for f in sorted(glob.glob("../ISSUE_TEMPLATE/*.yml")) if not f.endswith("config.yml")}
miss = []
def fields(form):
    return form["fields"]
answers = {}
for name, form in forms.items():
    # GitHub drops a label of a form the repository lacks; "отзыв" is the one
    # it has, the rest come from the script.
    if form.get("labels") != ["отзыв"]:
        miss.append(f"{name}: labels {form.get('labels')}, want [\"отзыв\"]")
    for label, opts in fields(form).items():
        answers.setdefault(label, set()).update(opts)
    f = fields(form)
    for table, field in ((il.AREA, "Область"), (il.OS, "ОС")):
        if field in f and set(table) != set(f[field]):
            miss.append(f"{name}: {field} differs from the table")
    # Every form gets a kind: a mark, or a field whose every answer has the
    # same flag.
    kinds = {l for h, l in il.MARKS.items() if h in f}
    for fl, opts in f.items():
        got = {il.FLAGS.get((fl, o)) for o in opts}
        if opts and len(got) == 1 and None not in got:
            kinds |= got
    if not kinds and name != "dogfood.yml":
        miss.append(f"{name}: no kind label (MARKS or a single-answer flag)")
for (field, answer) in il.FLAGS:
    legacy = (field, answer) in il.LEGACY
    if legacy and answer in answers.get(field, ()):
        miss.append(f"LEGACY {field}: {answer} — снова вариант формы")
    if not legacy and answer not in answers.get(field, ()):
        miss.append(f"флаг {field}: {answer} — нет в формах")
for heading in il.MARKS:
    n = sum(heading in fields(f) for f in forms.values())
    if n != 1:
        miss.append(f"MARKS {heading}: в {n} формах, нужна одна")
print("\n".join(miss)); sys.exit(1 if miss else 0)
PY
then echo "ok   таблицы совпадают с формами"; else echo "FAIL таблицы и формы разошлись"; fail=1; fi

[ "$fail" = 0 ] && echo "issue_labels: все случаи верны"
exit "$fail"
