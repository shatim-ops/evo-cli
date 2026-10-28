### Исправлено

- Журнал сессии (`evo diag`): `evo deploy` и `evo destroy` с `--yes` или
  ответом «да» записываются с `confirmed: true`, как MCP-вызов с
  `confirmed=true`; их `--dry-run` — с `preview: true`. Раньше обе команды
  писали `confirmed: false` и по журналу нельзя было отличить развёртывание
  от пробного запуска.
