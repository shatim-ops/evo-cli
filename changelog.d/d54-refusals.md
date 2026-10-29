### Миграция

- **Версия или подсеть не из списка каталога и второй шлюз SNAT в зоне отказывают до вопроса** (DECISIONS D54).
  - Затронуты `evo pg create`, `evo kafka create`, `evo redis create`, `evo k8s create`, `evo k8s plan`, `evo snat create` и MCP `pg_create`, `kafka_create`, `redis_create`, `k8s_create`, `k8s_plan`, `snat_create`.
  - Было: evo спрашивал согласие (без `--yes` — код 3), а после «да» платформа отказывала: `API_ERROR`, код 5.
  - Стало: версия, которой нет в `pg_versions`, `kafka_versions`, `redis_versions` или в каналах `k8s_options`, и `subnet_id`, которого нет среди подсетей проекта, — `NOT_FOUND`, код 4, до вопроса. Ошибка называет, что есть в каталоге. Шлюз SNAT в зоне, где у проекта уже есть шлюз, — `USAGE`, код 1, с `existing_id` этого шлюза.
  - Если список не прочитался или пуст, решения нет: вызов идёт как раньше. Версия k8s с `v` и без него — одна и та же.
  - Что поменять: скрипт, который ждал код 5 после согласия, проверяет `reason: NOT_FOUND` (или `USAGE` и `existing_id` у SNAT) уже на превью.
