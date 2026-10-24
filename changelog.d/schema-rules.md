### Миграция

- **Опечатка в id при удалении — `USAGE`, а не «удалено»** (DECISIONS D36,
  G13-21, G13-22). Было: id не той формы уходил в облако. С ключом ответ был
  `API_ERROR` (код 5) или, если облако отвечало 404, `already_deleted` с
  кодом 0 — опечатку можно было принять за успешное удаление. Стало: id, который
  API принимает только как UUID, проверяется до облака; значение другой формы —
  `USAGE`, код 1, ни одного запроса. Затронуты `evo ar delete`,
  `evo cert delete`, `evo dns record-delete`, `evo dns zone-delete`,
  `evo k8s delete`, `evo kafka delete`, `evo lb delete`, `evo pg delete`,
  `evo pg db-delete` и `evo pg user-delete` (`--cluster-id`),
  `evo redis delete`, `evo tg delete`, `evo vpc delete`,
  `evo vpc route-delete` и MCP `ar_delete`, `cert_delete`,
  `dns_record_delete`, `dns_zone_delete`, `k8s_delete`, `kafka_delete`,
  `lb_delete`, `pg_delete`, `pg_db_delete`, `pg_user_delete`, `redis_delete`,
  `tg_delete`, `vpc_delete`, `vpc_route_delete`. Что поменять: передавать id
  из списка (`… list`); `already_deleted` для id верной формы остаётся, как
  в D26.
- **Значение, которое не примет ни одно API, — `USAGE` до превью** (D36,
  G13-3). Было: превью и вопрос о согласии, а после «да» — отказ облака
  (код 5). Стало: `USAGE`, код 1, до превью: `evo kafka create
  --storage-gib` меньше 1, `evo redis create --storage-gb` меньше 1,
  `evo dns record-create --ttl` меньше 0, `evo vpc open-ports --cidr` и
  `evo vpc route-create --subnet` не в виде CIDR (адрес/длина префикса,
  например `10.0.0.0/16`; один адрес — `/32`); MCP `kafka_create`,
  `redis_create`, `dns_record_create`, `open_ports`, `vpc_route_create`. Что
  поменять: передавать значение в этих границах.

### Изменено

- Правила аргументов, которые видно без облака, объявлены в схеме
  инструмента: `minimum`, `maximum` и `format` (`uuid`, `cidr`) в
  `inputSchema`. Их проверяет одна функция до вызова, одинаково для MCP и CLI.
  Агент видит правило в схеме, а не узнаёт его из отказа. Прежние отказы
  (границы `disk_size`, `days`, `storage_gb`, портов балансировщика, id
  Compute и др.) — те же `USAGE` и код 1, меняется только текст сообщения.
