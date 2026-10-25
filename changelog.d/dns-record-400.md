### Миграция

- **`evo dns record-get`, `evo dns record-delete` и MCP `dns_record_get`,
  `dns_record_delete` читают ответ 400 «precondition» DNS API на id записи как
  «такой записи нет»** (DECISIONS D40; исправление: живой прогон Grok на 1.3.0,
  G130-07, и волна 2, G13-31; реестр NET-15). Было: запись приватной зоны без
  `type` — `API_ERROR`, код 5, от публичного API, приватный не спрашивался;
  `record-delete` такой записи спрашивал согласие и падал; повторное удаление
  удалённой записи — `API_ERROR`, код 5. Стало: 400 на id записи — как 404:
  без `type` запись ищется и в приватном API; записи нет ни в одном —
  `record-get` `NOT_FOUND`, код 4, `record-delete` — `already_deleted`, код 0,
  без вопроса (D26); DELETE, ответивший 400, перепроверяется чтением: записи
  нет — `already_deleted`, есть — прежняя ошибка. Что поменять: скрипт,
  который ждал код 5 на удалённую запись, получает код 0 и
  `status: already_deleted` (`record-delete`) или код 4 (`record-get`).
