### Миграция

- **Вызов, который не пройдёт, отказывает до вопроса о согласии — DBaaS, S3,
  DNS, сертификаты, floating IP** (DECISIONS D35, G13-3). Было: превью и
  вопрос (без `--yes` — код 3), отказ после «да». Стало: отказ до превью, с
  `--yes` и без одинаково: `evo kafka create --storage-gib`, `evo redis
  create --storage-gb`, `evo pg create --storage-gb` меньше минимума
  выбранной спецификации — `USAGE`, код 1; `evo s3 bucket-delete` бакета с
  объектами — `USAGE`, код 1, с подсказкой удалить объекты через `evo s3
  rm`; `evo cert private-key` с сертификатом, которого нет, `evo dns
  record-create` с зоной, которой нет, `evo vpc fip-attach` с адресом или
  ВМ, которых нет, — `NOT_FOUND`, код 4; `evo dns record-create` с занятыми
  именем и типом записи — `USAGE`, код 1, `existing_id` (было: после
  согласия, D28). То же в MCP `kafka_create`, `redis_create`, `pg_create`,
  `s3_bucket_delete`, `cert_private_key`, `dns_record_create`, `fip_attach`.
  Что поменять: проверять код отказа без `--yes` так же, как с ним.
- **Превью `evo cert private-key`, `evo dns record-create` и `evo vpc
  fip-attach` читают облако** (оговорка D31, D35). Было: превью без ключа и
  проекта, код 3 без `--yes`. Стало: превью ищет сертификат, зону с её
  записями или адрес с ВМ — без ключа или проекта
  `AUTH_REQUIRED`/`CONFIG_INVALID`, код 2; так же MCP без `confirmed`. Что
  поменять: задать ключ и проект до превью.

### Изменено

- `kafka_create`, `redis_create`, `pg_create`, `pg_user_create`,
  `ca_deploy`, `s3_bucket_delete`, `cert_private_key`, `dns_record_create` и
  `fip_attach` — на гейте согласия (D35): вызов с согласием выполняет тот же
  план, что показало превью. Превью `dns_record_create` называет видимость
  зоны (публичная или приватная), найденную по id.
