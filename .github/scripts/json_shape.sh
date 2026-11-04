#!/usr/bin/env bash
# Форма JSON-ответа для публичного лога (api-probe P50): ключи и единицы
# без цен. Числа — «число»; строка-идентификатор (имя продукта, код,
# единица: до 40 знаков [A-Za-z0-9_.:/-]) — как есть, это публичный
# каталог; прочие строки — «строка». У массива — длина и форма первого
# элемента. Вывод — до 1500 знаков.

# json_shape — тело на stdin; ровно один документ JSON — его форма, иначе
# «не JSON: <первые 80 знаков>». jq -s читает тело целиком до вывода:
# тело, которое лишь начинается с числа («404 page not found»), не
# печатает "число" перед ошибкой.
json_shape() {
  local b out
  b=$(cat)
  if out=$(printf '%s' "$b" | jq -c -s '
      def sh: if type == "object" then with_entries(.value |= sh)
        elif type == "array" then (if length == 0 then [] else [length, (.[0] | sh)] end)
        elif type == "number" then "число"
        elif type == "string" then (if test("^[A-Za-z0-9_.:/-]{1,40}$") then . else "строка" end)
        else type end;
      if length == 1 then .[0] | sh else error("not one document") end' 2>/dev/null); then
    printf '%s' "${out:0:1500}"
  else
    printf 'не JSON: %.80s' "$b"
  fi
}
