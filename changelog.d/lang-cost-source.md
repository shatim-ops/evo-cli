### Изменено

- **Источник цены в превью и плане (`evo deploy plan`, `evo stack plan`,
  `evo pg create` и другие) — по-русски.** Поле `source` у цены
  («estimate: rate-card …; not a billed price», «price-calculation API
  (Cloud.ru)», тариф PostgreSQL) в таблице CLI выводилось по-английски.
  Теперь CLI показывает его по-русски («оценка по тарифной карте …; не
  сумма счёта»). Значение `source` в `--json` и в ответах MCP не меняется:
  его сравнивает код (D6; опись Астры #1835).
