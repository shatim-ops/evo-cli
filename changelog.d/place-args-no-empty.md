### Миграция

- **Пустая строка в аргументе места внутри проекта — `USAGE`** (DECISIONS D100, расширение по решению владельца в
  #1597). Аргументы: `subnet_id` у `provision`, `k8s_create`, `lb_create`, `pg_create`, `kafka_create`,
  `redis_create`; `vpc_id` у `subnet_create`, `dns_zone_create`, `lb_create`; `nodes_subnet_id` и `security_group_id`
  у `k8s_add_nodepool`; `disk_type` у `disk_create` и `backup_restore`, и их флаги CLI (`--subnet-id`, `--vpc-id`,
  `--disk-type` и т. п.). Было: пустое значение (например, из незаданной переменной: `--subnet-id "$SUBNET"`)
  читалось как «не задано», ресурс создавался в подсети, VPC или с типом диска по умолчанию, со `--yes` — с кодом 0.
  Стало: `USAGE`, код 1, с именем аргумента, до превью и любого запроса; значение из пробелов тоже пустое. Что
  поменять: не передавать аргумент или флаг, когда значения нет; место по умолчанию — без аргумента, как прежде.
