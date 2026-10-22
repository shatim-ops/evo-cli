#!/usr/bin/env bash
# Классы PR по diff реестра (аудит процессов 1.2.1, П5 часть 2, этап 6.5):
# точнее путей — сравнивается то, что видит агент, ответ tools/list сервера
# evo mcp на базе и на голове PR. Вызов: classify_registry.sh <база.json>
# <голова.json>, файлы — массивы инструментов tools/list. Печатает классы по
# одному в строке:
#   new-tool    — у головы есть инструмент, которого нет у базы;
#   breaking    — инструмент базы пропал (агент, который его зовёт, сломан);
#   policy      — у инструмента изменились annotations, обязательные
#                 аргументы или наличие confirmed (согласие);
#   tool-change — у инструмента изменилась остальная схема аргументов.
# Описания инструмента и аргументов классом не считаются.
set -euo pipefail

[ $# -eq 2 ] || { echo "вызов: $0 <база.json> <голова.json>" >&2; exit 2; }

jq -rn --slurpfile base "$1" --slurpfile head "$2" '
  def nodesc: walk(if type == "object" then del(.description) else . end);
  def byname: map({key: .name, value: (.inputSchema |= nodesc)}) | from_entries;
  def consent: .inputSchema.properties | has("confirmed");
  def policy: [.annotations, (.inputSchema.required // [] | sort), consent];
  ($base[0] | byname) as $b | ($head[0] | byname) as $h |
  [ ($h | keys - ($b | keys) | select(length > 0) | "new-tool"),
    ($b | keys - ($h | keys) | select(length > 0) | "breaking"),
    ($h | keys[] | select($b[.]) as $n
      | if ($b[$n] | policy) != ($h[$n] | policy) then "policy"
        elif $b[$n].inputSchema != $h[$n].inputSchema then "tool-change"
        else empty end)
  ] | unique[]'
