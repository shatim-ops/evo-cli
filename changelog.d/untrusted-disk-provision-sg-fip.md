### Безопасность

- **Значения облака в ответах `disk_create`, `provision`, `sg_rule_delete`
  и `release_floating_ip` помечены или взяты в кавычки.** Было:
  `disk_id`, который дало облако, и тот же id в `next_args` у
  `disk_create`; адрес, `ssh`, `flavor_name` и `region_name` у
  `provision`; `id` и `ip_address` в отказе `release_floating_ip` для
  адреса SNAT-шлюза приходили без `_untrusted`. Id и адрес ВМ в `message`
  у `provision`, поля правила и его состояние в тексте согласия
  `sg_rule_delete`, адрес в отказе `release_floating_ip` стояли в тексте
  evo без кавычек. Стало: `disk_id` — в центральном списке полей,
  которые `_untrusted` называет у любого инструмента; остальные поля
  объявлены у инструментов; значения облака в тексте evo — в кавычках,
  на одной строке. Значения и форма ответа не меняются.
