#!/usr/bin/env bash
# Проверка answers_catalogue.py (строка плана 1.9 «Запись ответов», #1716):
# из сырого файла EVO_RECORD_ANSWERS наружу идёт только то, что совпало с
# описью — запрос с шаблоном пути из scripts/api-contract.json или
# scripts/probe-requests.json (хост и сегменты пути — из шаблона, не из
# записи), поля — из scripts/api-contract-fields.json; остальное
# отброшено со счётом. Условие У2 заключения «Риска» 206: имена из пути
# (бакет, ключ S3, зона DNS) и ключи меток в выход не попадают.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

cat >"$work/raw.jsonl" <<'J'
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":{"status":"name","versionUpgrade.availableVersions.[]":"name","labels.team":"name","labels.*":"name"}}
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":{"status":"name","controlPlane.version":"name"}}
{"request":"DELETE https://mk8s.api.cloud.ru/v3/nodePools/{id}","status":404,"form":"json","paths":{"code":"number","message":"name"}}
{"request":"GET https://dns.api.cloud.ru/v1/zones/secret-zone.example","status":200,"form":"json","paths":{"name":"name"}}
{"request":"GET https://s3.cloud.ru/my-private-bucket/key.txt","status":200,"form":"not-json","paths":{}}
{"request":"GET https://unknown.example/v1/x","status":200,"form":"json","paths":{"a":"name"}}
not json at all
J

rc=0
out=$(python3 -I "$here/answers_catalogue.py" --root "$root" --source "cloud-e2e run 999" --date 2026-11-14 "$work/raw.jsonl" 2>"$work/err") || rc=$?
[ "$rc" = 0 ] || { echo "FAIL: код $rc"; cat "$work/err"; fail=1; }
printf '%s' "$out" >"$work/out.json"
jq -e 'type == "array"' "$work/out.json" >/dev/null || { echo "FAIL: выход не массив JSON"; fail=1; }

# Карточка кластера: две строки слиты в одну запись; путь из записи
# заменён шаблоном описи; поле меток (ключ-идентификатор и «*») отброшено.
c=$(jq -c '.[] | select(.request == "GET https://mk8s.api.cloud.ru/v3/clusters/{clusterId}" and .status == 200)' "$work/out.json")
[ -n "$c" ] || { echo "FAIL: нет записи карточки кластера"; cat "$work/out.json"; fail=1; }
want='{"controlPlane.version":"name","status":"name","versionUpgrade.availableVersions.[]":"name"}'
[ "$(jq -cS .paths <<<"$c")" = "$want" ] || { echo "FAIL: поля карточки: $(jq -cS .paths <<<"$c"), ждали $want"; fail=1; }
[ "$(jq -r .source <<<"$c")" = "cloud-e2e run 999" ] || { echo "FAIL: источник"; fail=1; }
[ "$(jq -r .recorded <<<"$c")" = "2026-11-14" ] || { echo "FAIL: дата"; fail=1; }
[ "$(jq -r .id <<<"$c")" = "mk8s-get-v3-clusters-clusterid-200" ] || { echo "FAIL: id $(jq -r .id <<<"$c")"; fail=1; }

# Отказ — своя запись по коду.
jq -e '.[] | select(.request == "DELETE https://mk8s.api.cloud.ru/v3/nodePools/{nodePoolId}" and .status == 404)' "$work/out.json" >/dev/null ||
	{ echo "FAIL: нет записи отказа удаления группы"; fail=1; }

# Имена из пути и чужие хосты наружу не выходят.
for leak in secret-zone my-private-bucket key.txt unknown.example labels team; do
	if grep -q "$leak" "$work/out.json"; then echo "FAIL: в выходе «$leak»"; fail=1; fi
done

# Счёт отброшенного — в stderr, по причинам.
for why in 'строк: 7' 'принято: 3' 'хост вне описи: 2' 'путь вне описи: 1' 'не JSON: 1' 'полей вне описи: 4'; do
	grep -q "$why" "$work/err" || { echo "FAIL: в счёте нет «$why»"; cat "$work/err"; fail=1; }
done

# Два ответа с разным типом одного поля — «?» и счёт таких полей.
cat >"$work/kinds.jsonl" <<'J'
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":{"status":"name"}}
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":{"status":"number"}}
J
k=$(python3 -I "$here/answers_catalogue.py" --root "$root" --source s --date 2026-11-14 "$work/kinds.jsonl" 2>"$work/kerr")
[ "$(jq -r '.[0].paths.status' <<<"$k")" = "?" ] || { echo "FAIL: разный тип поля — не «?»"; fail=1; }
grep -q 'полей с разным типом: 1' "$work/kerr" || { echo "FAIL: нет счёта полей с разным типом"; cat "$work/kerr"; fail=1; }

# Вид поля — только из закрытого набора AnswerShape («Риск», заключение
# 226, У1): строка и объект на месте вида в выход не идут, счёт — в stderr.
# Строка с paths не объектом — «не JSON», а не падение всего каталога.
cat >"$work/kind.jsonl" <<'J'
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":{"status":"LEAK-my-project-name","controlPlane.version":{"x":"LEAK-nested"},"versionUpgrade.availableVersions.[]":"name"}}
{"request":"GET https://mk8s.api.cloud.ru/v3/clusters/{id}","status":200,"form":"json","paths":["status"]}
J
rc=0
v=$(python3 -I "$here/answers_catalogue.py" --root "$root" --source s --date 2026-11-14 "$work/kind.jsonl" 2>"$work/verr") || rc=$?
[ "$rc" = 0 ] || { echo "FAIL: строка с paths не объектом уронила генератор (код $rc)"; cat "$work/verr"; fail=1; }
if grep -q 'LEAK' <<<"$v"; then echo "FAIL: вид вне набора вышел в каталог"; printf '%s\n' "$v"; fail=1; fi
[ "$(jq -cS '.[0].paths' <<<"$v" 2>/dev/null)" = '{"versionUpgrade.availableVersions.[]":"name"}' ] || { echo "FAIL: поля при виде вне набора: $v"; fail=1; }
grep -q 'видов вне набора: 2' "$work/verr" || { echo "FAIL: нет счёта видов вне набора"; cat "$work/verr"; fail=1; }
grep -q 'не JSON: 1' "$work/verr" || { echo "FAIL: paths не объектом — не «не JSON»"; cat "$work/verr"; fail=1; }

# Сырой файл не изменён и не удалён генератором: удаляет его job.
[ -s "$work/raw.jsonl" ] || { echo "FAIL: сырой файл тронут"; fail=1; }

# Пустой или отсутствующий файл — пустой массив, код 0: прогон без
# вызовов evo не ошибка.
: >"$work/empty.jsonl"
[ "$(python3 -I "$here/answers_catalogue.py" --root "$root" --source s --date 2026-11-14 "$work/empty.jsonl" 2>/dev/null)" = "[]" ] || { echo "FAIL: пустой файл"; fail=1; }
[ "$(python3 -I "$here/answers_catalogue.py" --root "$root" --source s --date 2026-11-14 "$work/nope.jsonl" 2>/dev/null)" = "[]" ] || { echo "FAIL: нет файла"; fail=1; }

[ "$fail" = 0 ] || exit 1
echo "answers-catalogue: ok"
