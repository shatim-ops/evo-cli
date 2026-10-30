#!/usr/bin/env bash
# Мутационная проверка инвариантов (план 1.6): каждая строка
# .github/mutations.tsv ломает одно место продуктового кода, и названный
# тест должен покраснеть. Выжившая мутация — инвариант её не видит: код 1 и
# строка в сводке. Дерево после каждой мутации возвращается (git checkout).
#
#   mutate.sh [манифест]
set -euo pipefail

manifest=${1:-.github/mutations.tsv}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
survived=0 killed=0

while IFS=$'\t' read -r id file find replace pkg run; do
  [[ -z "$id" || "$id" == \#* ]] && continue
  if ! FIND=$find REPLACE=$replace python3 -I - "$file" <<'PY'
import os, sys
path = sys.argv[1]
src = open(path, encoding="utf-8").read()
find, repl = os.environ["FIND"], os.environ["REPLACE"]
if src.count(find) != 1:
    sys.exit(f"{path}: «{find}» встречается {src.count(find)} раз, нужно 1")
open(path, "w", encoding="utf-8").write(src.replace(find, repl))
PY
  then
    echo "| $id | не применилась | — |" >>"$summary"
    survived=$((survived + 1))
    continue
  fi
  if ! go build ./... </dev/null >/dev/null 2>&1; then
    # Не собирается — тест упал бы на сборке, а не на инварианте.
    echo "| $id | не собирается: мутация негодна | — |" >>"$summary"
    survived=$((survived + 1))
  elif go test -count=1 -run "$run" "$pkg" </dev/null >/dev/null 2>&1; then
    echo "| $id | **выжила**: $pkg -run $run зелёный | бэклог «Процесса» |" >>"$summary"
    survived=$((survived + 1))
  else
    echo "| $id | убита | $pkg -run $run |" >>"$summary"
    killed=$((killed + 1))
  fi
  git checkout -- "$file" </dev/null
done <"$manifest"

echo "убито: $killed, выжило или негодно: $survived" >>"$summary"
[ "$survived" -eq 0 ]
