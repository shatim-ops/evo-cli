### Миграция

- **Пустая строка в аргументе, который называет место или вид создаваемого
  ресурса внутри проекта, — `USAGE`** (DECISIONS D100, расширение 1.9).
  Было: `--subnet-id ""` или `subnet_id: ""` (например, из пустой переменной
  в скрипте или шаблоне агента) читался как «не задано», и ресурс
  создавался в подсети, VPC или группе безопасности по умолчанию, с типом
  диска, размером, образом или версией по умолчанию; со `--yes` — без
  чтения превью и с кодом 0. Стало: `USAGE`, код 1, с именем аргумента, до
  превью и любого запроса, одинаково в CLI и MCP; значение из пробелов тоже
  считается пустым. Аргументы: `subnet_id`, `flavor`, `image`, `image_id` у
  `provision`; `flavor`, `image`, `image_id` у `deploy`; `disk_type` у
  `disk_create` и `backup_restore`; `vpc_id` у `subnet_create` и
  `dns_zone_create`; `subnet_id`, `vpc_id` у `lb_create`; `subnet_id`,
  `flavor`, `version`, `cluster_sa_id` у `k8s_create`; `nodes_subnet_id`,
  `security_group_id`, `flavor` у `k8s_add_nodepool`; `version` у
  `k8s_add_addon`; `subnet_id`, `specification_id`, `version` у `pg_create`;
  `subnet_id`, `specification_id`, `version_id` у `kafka_create` и
  `redis_create`; `cpu`, `memory` у `ca_deploy`; `kms_key_id` у
  `secret_create`; `interface_id` у `tg_create`; `next_hop_az`,
  `next_hop_interface_id` у `vpc_route_create`. Что поменять: не передавать
  аргумент или флаг, когда значения нет, или передать значение.
