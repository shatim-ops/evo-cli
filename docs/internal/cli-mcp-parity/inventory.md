# Инвентаризация CLI ↔ MCP

<!-- Сгенерировано: go run ./docs/internal/cli-mcp-parity/inventory/main.go > docs/internal/cli-mcp-parity/inventory.md. Руками не править. -->

Инструментов в реестре: 152 (изменяющих: 65). Команд дерева: 144. Команд в docs/commands.md: 195 (+3 скрытых).

## 1. Инструменты по способу вызова из CLI

| Способ | Инструментов | из них изменяющих |
|---|---|---|
| дерево | 144 | 63 |
| движок | 5 | 1 |
| tools call | 3 | 1 |

«дерево» — своя команда, построенная из CLISpec инструмента в реестре; «движок» — команда верхнего уровня со своим кодом cobra, которая делает то же через engine, а не через реестр; «tools call» — доступен из CLI только как `evo tools call <имя>`. «движок» и «tools call» вместе — временный список `tools.CLIPending` (8): тест TestParity_CommandsAndTools не даёт ему устареть, PR 3 и 5 плана его опустошают.

### По группам

| Группа | Всего | дерево | движок | tools call | RU-справка (short/long) |
|---|---|---|---|---|---|
| ar | 5 | 5 | 0 | 0 | 5/5 |
| audit | 1 | 1 | 0 | 0 | 1/1 |
| billing | 3 | 3 | 0 | 0 | 3/3 |
| ca | 7 | 7 | 0 | 0 | 7/7 |
| cert | 10 | 10 | 0 | 0 | 10/10 |
| deploy | 8 | 2 | 3 | 3 | 8/8 |
| disk | 7 | 7 | 0 | 0 | 7/7 |
| dns | 9 | 9 | 0 | 0 | 9/9 |
| iam | 1 | 1 | 0 | 0 | 1/1 |
| image | 6 | 5 | 1 | 0 | 6/6 |
| k8s | 15 | 15 | 0 | 0 | 15/15 |
| kafka | 7 | 7 | 0 | 0 | 7/7 |
| monitoring | 2 | 2 | 0 | 0 | 2/2 |
| nlb | 9 | 9 | 0 | 0 | 9/9 |
| org | 2 | 1 | 1 | 0 | 2/2 |
| pg | 15 | 15 | 0 | 0 | 15/15 |
| redis | 7 | 7 | 0 | 0 | 7/7 |
| s3 | 7 | 7 | 0 | 0 | 7/7 |
| secret | 6 | 6 | 0 | 0 | 6/6 |
| snat | 3 | 3 | 0 | 0 | 3/3 |
| stack | 4 | 4 | 0 | 0 | 4/4 |
| vm | 5 | 5 | 0 | 0 | 5/5 |
| vpc | 13 | 13 | 0 | 0 | 13/13 |

## 2. Инструмент → команда CLI

| Группа | Инструмент | Меняет | Способ | Команда CLI | RU short/long |
|---|---|---|---|---|---|
| ar | `ar_create` | да | дерево | `evo ar create` | да/да |
| ar | `ar_delete` | да | дерево | `evo ar delete` | да/да |
| ar | `ar_get` | нет | дерево | `evo ar get` | да/да |
| ar | `ar_list` | нет | дерево | `evo ar list` | да/да |
| ar | `ar_operation` | нет | дерево | `evo ar operation` | да/да |
| audit | `audit_events` | нет | дерево | `evo audit events` | да/да |
| billing | `billing_balance` | нет | дерево | `evo billing balance` | да/да |
| billing | `billing_consumption` | нет | дерево | `evo billing consumption` | да/да |
| billing | `billing_finops` | нет | дерево | `evo billing finops` | да/да |
| ca | `ca_delete` | да | дерево | `evo ca delete` | да/да |
| ca | `ca_deploy` | да | дерево | `evo ca deploy` | да/да |
| ca | `ca_get` | нет | дерево | `evo ca get` | да/да |
| ca | `ca_list` | нет | дерево | `evo ca list` | да/да |
| ca | `ca_logs` | нет | дерево | `evo ca logs` | да/да |
| ca | `ca_start` | да | дерево | `evo ca start` | да/да |
| ca | `ca_stop` | да | дерево | `evo ca stop` | да/да |
| cert | `cert_delete` | да | дерево | `evo cert delete` | да/да |
| cert | `cert_get` | нет | дерево | `evo cert get` | да/да |
| cert | `cert_import` | да | дерево | `evo cert import` | да/да |
| cert | `cert_issue_le` | да | дерево | `evo cert issue-le` | да/да |
| cert | `cert_list` | нет | дерево | `evo cert list` | да/да |
| cert | `cert_operation` | нет | дерево | `evo cert operation` | да/да |
| cert | `cert_private_key` | нет | дерево | `evo cert private-key` | да/да |
| cert | `cert_renew` | да | дерево | `evo cert renew` | да/да |
| cert | `cert_usage` | нет | дерево | `evo cert usage` | да/да |
| cert | `cert_versions` | нет | дерево | `evo cert versions` | да/да |
| deploy | `deploy` | да | движок | `evo deploy` (свой код), иначе `evo tools call deploy` | да/да |
| deploy | `deploy_apply` | да | tools call | `evo tools call deploy_apply` | да/да |
| deploy | `deploy_plan` | нет | tools call | `evo tools call deploy_plan` | да/да |
| deploy | `destroy` | да | дерево | `evo vm delete`, `evo destroy` (движок) | да/да |
| deploy | `list` | нет | tools call | `evo tools call list` | да/да |
| deploy | `logs` | нет | движок | `evo logs` (свой код), иначе `evo tools call logs` | да/да |
| deploy | `open_ports` | да | дерево | `evo vpc open-ports` | да/да |
| deploy | `status` | нет | движок | `evo status` (свой код), иначе `evo tools call status` | да/да |
| disk | `disk_attach` | да | дерево | `evo disk attach` | да/да |
| disk | `disk_create` | да | дерево | `evo disk create` | да/да |
| disk | `disk_delete` | да | дерево | `evo disk delete` | да/да |
| disk | `disk_detach` | да | дерево | `evo disk detach` | да/да |
| disk | `disk_list` | нет | дерево | `evo disk list` | да/да |
| disk | `disk_resize` | да | дерево | `evo disk resize` | да/да |
| disk | `list_disk_types` | нет | дерево | `evo disk types` | да/да |
| dns | `dns_operation` | нет | дерево | `evo dns operation` | да/да |
| dns | `dns_record_create` | да | дерево | `evo dns record-create` | да/да |
| dns | `dns_record_delete` | да | дерево | `evo dns record-delete` | да/да |
| dns | `dns_record_get` | нет | дерево | `evo dns record-get` | да/да |
| dns | `dns_records` | нет | дерево | `evo dns records` | да/да |
| dns | `dns_zone_create` | да | дерево | `evo dns zone-create` | да/да |
| dns | `dns_zone_delete` | да | дерево | `evo dns zone-delete` | да/да |
| dns | `dns_zone_get` | нет | дерево | `evo dns zone-get` | да/да |
| dns | `dns_zones` | нет | дерево | `evo dns zones` | да/да |
| iam | `iam_service_accounts` | нет | дерево | `evo iam service-accounts` | да/да |
| image | `image_create` | да | дерево | `evo image create` | да/да |
| image | `image_delete` | да | дерево | `evo image delete` | да/да |
| image | `image_get` | нет | дерево | `evo image get` | да/да |
| image | `image_list` | нет | дерево | `evo image list` | да/да |
| image | `list_images` | нет | движок | `evo list-images` (свой код), иначе `evo tools call list_images` | да/да |
| image | `marketplace_list` | нет | дерево | `evo marketplace list` | да/да |
| k8s | `k8s_add_addon` | да | дерево | `evo k8s add-addon` | да/да |
| k8s | `k8s_add_nodepool` | да | дерево | `evo k8s add-nodepool` | да/да |
| k8s | `k8s_addon_versions` | нет | дерево | `evo k8s addon-versions` | да/да |
| k8s | `k8s_addons` | нет | дерево | `evo k8s addons` | да/да |
| k8s | `k8s_apply` | да | дерево | `evo k8s apply` | да/да |
| k8s | `k8s_create` | да | дерево | `evo k8s create` | да/да |
| k8s | `k8s_delete` | да | дерево | `evo k8s delete` | да/да |
| k8s | `k8s_get` | нет | дерево | `evo k8s get` | да/да |
| k8s | `k8s_helm` | да | дерево | `evo k8s helm` | да/да |
| k8s | `k8s_kubeconfig` | нет | дерево | `evo k8s kubeconfig` | да/да |
| k8s | `k8s_kubectl` | да | дерево | `evo k8s kubectl` | да/да |
| k8s | `k8s_list` | нет | дерево | `evo k8s list` | да/да |
| k8s | `k8s_nodepools` | нет | дерево | `evo k8s nodepools` | да/да |
| k8s | `k8s_options` | нет | дерево | `evo k8s options` | да/да |
| k8s | `k8s_plan` | нет | дерево | `evo k8s plan` | да/да |
| kafka | `kafka_create` | да | дерево | `evo kafka create` | да/да |
| kafka | `kafka_delete` | да | дерево | `evo kafka delete` | да/да |
| kafka | `kafka_get` | нет | дерево | `evo kafka get` | да/да |
| kafka | `kafka_list` | нет | дерево | `evo kafka list` | да/да |
| kafka | `kafka_operation` | нет | дерево | `evo kafka operation` | да/да |
| kafka | `kafka_specs` | нет | дерево | `evo kafka specs` | да/да |
| kafka | `kafka_versions` | нет | дерево | `evo kafka versions` | да/да |
| monitoring | `metrics_history` | нет | дерево | `evo metrics history` | да/да |
| monitoring | `metrics_scrape` | нет | дерево | `evo metrics scrape` | да/да |
| nlb | `lb_create` | да | дерево | `evo lb create` | да/да |
| nlb | `lb_delete` | да | дерево | `evo lb delete` | да/да |
| nlb | `lb_get` | нет | дерево | `evo lb get` | да/да |
| nlb | `lb_list` | нет | дерево | `evo lb list` | да/да |
| nlb | `lb_operation` | нет | дерево | `evo lb operation` | да/да |
| nlb | `tg_create` | да | дерево | `evo tg create` | да/да |
| nlb | `tg_delete` | да | дерево | `evo tg delete` | да/да |
| nlb | `tg_get` | нет | дерево | `evo tg get` | да/да |
| nlb | `tg_list` | нет | дерево | `evo tg list` | да/да |
| org | `org_projects` | нет | движок | `evo project list` (свой код), иначе `evo tools call org_projects` | да/да |
| org | `org_quotas` | нет | дерево | `evo org quotas` | да/да |
| pg | `pg_backup_create` | да | дерево | `evo pg backup-create` | да/да |
| pg | `pg_backups` | нет | дерево | `evo pg backups` | да/да |
| pg | `pg_create` | да | дерево | `evo pg create` | да/да |
| pg | `pg_databases` | нет | дерево | `evo pg databases` | да/да |
| pg | `pg_db_create` | да | дерево | `evo pg db-create` | да/да |
| pg | `pg_db_delete` | да | дерево | `evo pg db-delete` | да/да |
| pg | `pg_delete` | да | дерево | `evo pg delete` | да/да |
| pg | `pg_get` | нет | дерево | `evo pg get` | да/да |
| pg | `pg_list` | нет | дерево | `evo pg list` | да/да |
| pg | `pg_operation` | нет | дерево | `evo pg operation` | да/да |
| pg | `pg_specs` | нет | дерево | `evo pg specs` | да/да |
| pg | `pg_user_create` | да | дерево | `evo pg user-create` | да/да |
| pg | `pg_user_delete` | да | дерево | `evo pg user-delete` | да/да |
| pg | `pg_users` | нет | дерево | `evo pg users` | да/да |
| pg | `pg_versions` | нет | дерево | `evo pg versions` | да/да |
| redis | `redis_create` | да | дерево | `evo redis create` | да/да |
| redis | `redis_delete` | да | дерево | `evo redis delete` | да/да |
| redis | `redis_get` | нет | дерево | `evo redis get` | да/да |
| redis | `redis_list` | нет | дерево | `evo redis list` | да/да |
| redis | `redis_operation` | нет | дерево | `evo redis operation` | да/да |
| redis | `redis_specs` | нет | дерево | `evo redis specs` | да/да |
| redis | `redis_versions` | нет | дерево | `evo redis versions` | да/да |
| s3 | `s3_bucket_create` | да | дерево | `evo s3 bucket-create` | да/да |
| s3 | `s3_bucket_delete` | да | дерево | `evo s3 bucket-delete` | да/да |
| s3 | `s3_buckets` | нет | дерево | `evo s3 buckets` | да/да |
| s3 | `s3_get` | нет | дерево | `evo s3 get` | да/да |
| s3 | `s3_ls` | нет | дерево | `evo s3 ls` | да/да |
| s3 | `s3_put` | да | дерево | `evo s3 put` | да/да |
| s3 | `s3_rm` | да | дерево | `evo s3 rm` | да/да |
| secret | `secret_add_version` | да | дерево | `evo secret add-version` | да/да |
| secret | `secret_create` | да | дерево | `evo secret create` | да/да |
| secret | `secret_delete` | да | дерево | `evo secret delete` | да/да |
| secret | `secret_get` | нет | дерево | `evo secret get` | да/да |
| secret | `secret_list` | нет | дерево | `evo secret list` | да/да |
| secret | `secret_value` | нет | дерево | `evo secret value` | да/да |
| snat | `snat_create` | да | дерево | `evo snat create` | да/да |
| snat | `snat_delete` | да | дерево | `evo snat delete` | да/да |
| snat | `snat_list` | нет | дерево | `evo snat list` | да/да |
| stack | `stack_apply` | да | дерево | `evo stack apply` | да/да |
| stack | `stack_destroy` | да | дерево | `evo stack destroy` | да/да |
| stack | `stack_plan` | нет | дерево | `evo stack plan` | да/да |
| stack | `stack_status` | нет | дерево | `evo stack status` | да/да |
| vm | `list_flavors` | нет | дерево | `evo vm flavors` | да/да |
| vm | `list_zones` | нет | дерево | `evo vm zones`, `evo list-zones` (движок) | да/да |
| vm | `provision` | да | дерево | `evo vm create` | да/да |
| vm | `vm_get` | нет | дерево | `evo vm get` | да/да |
| vm | `vm_list` | нет | дерево | `evo vm list` | да/да |
| vpc | `fip_attach` | да | дерево | `evo vpc fip-attach` | да/да |
| vpc | `list_floating_ips` | нет | дерево | `evo vpc fips` | да/да |
| vpc | `list_security_groups` | нет | дерево | `evo vpc security-groups` | да/да |
| vpc | `list_subnets` | нет | дерево | `evo vpc subnets` | да/да |
| vpc | `release_floating_ip` | да | дерево | `evo vpc fip-release` | да/да |
| vpc | `vpc_create` | да | дерево | `evo vpc create` | да/да |
| vpc | `vpc_delete` | да | дерево | `evo vpc delete` | да/да |
| vpc | `vpc_get` | нет | дерево | `evo vpc get` | да/да |
| vpc | `vpc_list` | нет | дерево | `evo vpc list` | да/да |
| vpc | `vpc_operation` | нет | дерево | `evo vpc operation` | да/да |
| vpc | `vpc_route_create` | да | дерево | `evo vpc route-create` | да/да |
| vpc | `vpc_route_delete` | да | дерево | `evo vpc route-delete` | да/да |
| vpc | `vpc_routes` | нет | дерево | `evo vpc routes` | да/да |

## 3. Команда CLI → инструмент

Команды деревьев (из `CLISpec`): позиционные аргументы, переименования и поля схемы без флага.

| Команда | Инструмент | Позиционные | Rename (аргумент→флаг) | Без флага (Omit) | Через корень | Pairs/Files/Raw/Render |
|---|---|---|---|---|---|---|
| `evo ar create` | `ar_create` | name | — | — | confirmed | — |
| `evo ar delete` | `ar_delete` | id | — | — | confirmed | — |
| `evo ar get` | `ar_get` | id | — | — | — | — |
| `evo ar list` | `ar_list` | — | — | — | — | — |
| `evo ar operation` | `ar_operation` | operation_id | — | — | — | — |
| `evo audit events` | `audit_events` | — | — | — | — | — |
| `evo billing balance` | `billing_balance` | — | — | — | — | — |
| `evo billing consumption` | `billing_consumption` | — | project_id→--project-id | — | — | — |
| `evo billing finops` | `billing_finops` | — | project_id→--project-id | — | — | — |
| `evo ca delete` | `ca_delete` | name | — | — | confirmed | — |
| `evo ca deploy` | `ca_deploy` | name | — | — | confirmed | pairs:env |
| `evo ca get` | `ca_get` | name | — | — | — | — |
| `evo ca list` | `ca_list` | — | — | — | — | — |
| `evo ca logs` | `ca_logs` | name | — | — | — | render |
| `evo ca start` | `ca_start` | name | — | — | confirmed | — |
| `evo ca stop` | `ca_stop` | name | — | — | confirmed | — |
| `evo cert delete` | `cert_delete` | id | — | — | confirmed | — |
| `evo cert get` | `cert_get` | id | — | — | — | — |
| `evo cert import` | `cert_import` | name | — | — | confirmed | — |
| `evo cert issue-le` | `cert_issue_le` | name | — | — | confirmed | — |
| `evo cert list` | `cert_list` | — | — | — | — | — |
| `evo cert operation` | `cert_operation` | operation_id | — | — | — | — |
| `evo cert private-key` | `cert_private_key` | certificate_id | — | — | confirmed | — |
| `evo cert renew` | `cert_renew` | certificate_id | — | — | confirmed | — |
| `evo cert usage` | `cert_usage` | id | — | — | — | — |
| `evo cert versions` | `cert_versions` | certificate_id | — | — | — | — |
| `evo disk attach` | `disk_attach` | disk_id | — | — | confirmed | — |
| `evo disk create` | `disk_create` | name | — | — | confirmed | — |
| `evo disk delete` | `disk_delete` | disk_id | — | — | confirmed | — |
| `evo disk detach` | `disk_detach` | disk_id | — | — | confirmed | — |
| `evo disk list` | `disk_list` | — | — | — | — | — |
| `evo disk resize` | `disk_resize` | disk_id | — | — | confirmed | — |
| `evo disk types` | `list_disk_types` | — | — | — | — | — |
| `evo dns operation` | `dns_operation` | operation_id | — | — | — | — |
| `evo dns record-create` | `dns_record_create` | zone_id name | — | — | confirmed | — |
| `evo dns record-delete` | `dns_record_delete` | record_id | — | — | confirmed | — |
| `evo dns record-get` | `dns_record_get` | record_id | — | — | — | — |
| `evo dns records` | `dns_records` | zone_id | — | — | — | — |
| `evo dns zone-create` | `dns_zone_create` | domain | — | — | confirmed | — |
| `evo dns zone-delete` | `dns_zone_delete` | id | — | — | confirmed | — |
| `evo dns zone-get` | `dns_zone_get` | id | — | — | — | — |
| `evo dns zones` | `dns_zones` | — | — | — | — | — |
| `evo iam service-accounts` | `iam_service_accounts` | — | project_id→--project-id | — | — | — |
| `evo image create` | `image_create` | name | — | — | confirmed | — |
| `evo image delete` | `image_delete` | image_id | — | — | confirmed | — |
| `evo image get` | `image_get` | image_id\|image | — | — | — | render |
| `evo image list` | `image_list` | query? | — | — | — | render |
| `evo k8s add-addon` | `k8s_add_addon` | cluster_id name | — | — | confirmed | file:config |
| `evo k8s add-nodepool` | `k8s_add_nodepool` | cluster_id | — | — | confirmed | — |
| `evo k8s addon-versions` | `k8s_addon_versions` | name | — | — | — | render |
| `evo k8s addons` | `k8s_addons` | query? | — | — | — | render |
| `evo k8s apply` | `k8s_apply` | cluster_id | — | — | confirmed | raw |
| `evo k8s create` | `k8s_create` | — | — | — | confirmed | — |
| `evo k8s delete` | `k8s_delete` | cluster_id | — | — | confirmed | — |
| `evo k8s get` | `k8s_get` | cluster_id | — | — | — | — |
| `evo k8s helm` | `k8s_helm` | cluster_id action | — | — | confirmed | raw |
| `evo k8s kubeconfig` | `k8s_kubeconfig` | cluster_id | — | — | — | — |
| `evo k8s kubectl` | `k8s_kubectl` | cluster_id args... | — | — | confirmed | raw |
| `evo k8s list` | `k8s_list` | — | — | — | — | — |
| `evo k8s nodepools` | `k8s_nodepools` | cluster_id | — | — | — | — |
| `evo k8s options` | `k8s_options` | — | — | — | — | — |
| `evo k8s plan` | `k8s_plan` | — | — | — | — | — |
| `evo kafka create` | `kafka_create` | name | — | — | confirmed | — |
| `evo kafka delete` | `kafka_delete` | cluster_id | — | — | confirmed | — |
| `evo kafka get` | `kafka_get` | cluster_id | — | — | — | — |
| `evo kafka list` | `kafka_list` | — | — | — | — | — |
| `evo kafka operation` | `kafka_operation` | operation_id | — | — | — | — |
| `evo kafka specs` | `kafka_specs` | — | — | — | — | — |
| `evo kafka versions` | `kafka_versions` | — | — | — | — | — |
| `evo lb create` | `lb_create` | name | — | — | confirmed | — |
| `evo lb delete` | `lb_delete` | id | — | — | confirmed | — |
| `evo lb get` | `lb_get` | id | — | — | — | — |
| `evo lb list` | `lb_list` | — | — | — | — | — |
| `evo lb operation` | `lb_operation` | operation_id | — | — | — | — |
| `evo marketplace list` | `marketplace_list` | query? | — | — | — | render |
| `evo metrics history` | `metrics_history` | match | — | — | — | — |
| `evo metrics scrape` | `metrics_scrape` | match? | — | — | — | render |
| `evo org quotas` | `org_quotas` | — | project_id→--project-id | — | — | — |
| `evo pg backup-create` | `pg_backup_create` | cluster_id name | — | — | confirmed | — |
| `evo pg backups` | `pg_backups` | cluster_id | — | — | — | — |
| `evo pg create` | `pg_create` | name | — | — | confirmed | — |
| `evo pg databases` | `pg_databases` | cluster_id | — | — | — | — |
| `evo pg db-create` | `pg_db_create` | cluster_id name | — | — | confirmed | — |
| `evo pg db-delete` | `pg_db_delete` | cluster_id name | — | — | confirmed | — |
| `evo pg delete` | `pg_delete` | cluster_id | — | id | confirmed | — |
| `evo pg get` | `pg_get` | cluster_id | — | id | — | — |
| `evo pg list` | `pg_list` | — | — | — | — | — |
| `evo pg operation` | `pg_operation` | operation_id | — | — | — | — |
| `evo pg specs` | `pg_specs` | — | — | — | — | — |
| `evo pg user-create` | `pg_user_create` | cluster_id name | — | — | confirmed | — |
| `evo pg user-delete` | `pg_user_delete` | cluster_id name | — | — | confirmed | — |
| `evo pg users` | `pg_users` | cluster_id | — | — | — | — |
| `evo pg versions` | `pg_versions` | — | — | — | — | — |
| `evo redis create` | `redis_create` | name | — | — | confirmed | — |
| `evo redis delete` | `redis_delete` | cluster_id | — | — | confirmed | — |
| `evo redis get` | `redis_get` | cluster_id | — | — | — | — |
| `evo redis list` | `redis_list` | — | — | — | — | — |
| `evo redis operation` | `redis_operation` | operation_id | — | — | — | — |
| `evo redis specs` | `redis_specs` | — | — | — | — | — |
| `evo redis versions` | `redis_versions` | — | — | — | — | — |
| `evo s3 bucket-create` | `s3_bucket_create` | bucket | region→--s3-region | — | confirmed | — |
| `evo s3 bucket-delete` | `s3_bucket_delete` | bucket | — | — | confirmed | — |
| `evo s3 buckets` | `s3_buckets` | — | — | — | — | — |
| `evo s3 get` | `s3_get` | bucket key | — | — | — | — |
| `evo s3 ls` | `s3_ls` | bucket | — | — | — | — |
| `evo s3 put` | `s3_put` | bucket key | — | — | confirmed | — |
| `evo s3 rm` | `s3_rm` | bucket key | — | — | confirmed | — |
| `evo secret add-version` | `secret_add_version` | path | — | — | confirmed | — |
| `evo secret create` | `secret_create` | path | — | — | confirmed | — |
| `evo secret delete` | `secret_delete` | path | — | — | confirmed | — |
| `evo secret get` | `secret_get` | path | — | — | — | — |
| `evo secret list` | `secret_list` | — | — | — | — | — |
| `evo secret value` | `secret_value` | path | — | — | confirmed | — |
| `evo snat create` | `snat_create` | name | — | — | confirmed | — |
| `evo snat delete` | `snat_delete` | id | — | — | confirmed | — |
| `evo snat list` | `snat_list` | — | — | — | — | — |
| `evo stack apply` | `stack_apply` | job_id | — | — | confirmed | — |
| `evo stack destroy` | `stack_destroy` | job_id | — | — | confirmed | — |
| `evo stack plan` | `stack_plan` | — | — | — | — | file:manifest |
| `evo stack status` | `stack_status` | job_id | — | — | — | — |
| `evo tg create` | `tg_create` | name | — | — | confirmed | — |
| `evo tg delete` | `tg_delete` | id | — | — | confirmed | — |
| `evo tg get` | `tg_get` | id | — | — | — | — |
| `evo tg list` | `tg_list` | — | — | — | — | — |
| `evo vm create` | `provision` | name | image_metadata→--meta, ports→--port | — | confirmed, project_id, region | pairs:image_metadata |
| `evo vm delete` | `destroy` | vm_id\|name | — | — | confirmed | — |
| `evo vm flavors` | `list_flavors` | — | — | — | — | — |
| `evo vm get` | `vm_get` | vm_id\|name | — | — | — | — |
| `evo vm list` | `vm_list` | — | — | — | — | — |
| `evo vm zones` | `list_zones` | — | — | — | — | — |
| `evo vpc create` | `vpc_create` | name | — | — | confirmed | — |
| `evo vpc delete` | `vpc_delete` | id | — | — | confirmed | — |
| `evo vpc fip-attach` | `fip_attach` | fip | — | — | confirmed | — |
| `evo vpc fip-release` | `release_floating_ip` | id? | — | — | confirmed | — |
| `evo vpc fips` | `list_floating_ips` | — | — | — | — | — |
| `evo vpc get` | `vpc_get` | id | — | — | — | — |
| `evo vpc list` | `vpc_list` | — | — | — | — | — |
| `evo vpc open-ports` | `open_ports` | security_group | ports→--port | — | confirmed | — |
| `evo vpc operation` | `vpc_operation` | operation_id | — | — | — | — |
| `evo vpc route-create` | `vpc_route_create` | vpc_id | — | — | confirmed | — |
| `evo vpc route-delete` | `vpc_route_delete` | vpc_id route_id | — | — | confirmed | — |
| `evo vpc routes` | `vpc_routes` | vpc_id | — | — | — | — |
| `evo vpc security-groups` | `list_security_groups` | — | — | — | — | — |
| `evo vpc subnets` | `list_subnets` | — | — | — | — | — |

Команды вне деревьев (из docs/commands.md и скрытые):

| Команда | Вид | Инструмент по смыслу | Почему |
|---|---|---|---|
| `evo` | group | — | корень |
| `evo completion` | local | — | скрипт автодополнения оболочки; к облаку не обращается |
| `evo completion bash` | local | — | то же |
| `evo completion fish` | local | — | то же |
| `evo completion powershell` | local | — | то же |
| `evo completion zsh` | local | — | то же |
| `evo configure` | local | — | пишет ключ в профиль на диске сервера/машины; MCP-клиент ключей не передаёт по контракту (docs/mcp.md) |
| `evo configure delete` | local | — | удаляет профиль с диска |
| `evo configure list` | local | — | читает профили с диска |
| `evo crew` | hidden | — | титры |
| `evo deploy` | engine | `deploy` | свой код cobra → engine.Deploy; инструмент deploy — тот же engine.Deploy с NonInteractive+Yes |
| `evo destroy` | engine | `destroy` | свой код → engine.Destroy (запись state); инструмент destroy без аргументов — тот же путь; с vm_id|name — команда дерева vm delete |
| `evo doctor` | local | — | диагностика машины: файлы настроек, права, PATH; часть (связь с API, ключ) могла бы быть инструментом |
| `evo init` | local | — | пишет .evo.yaml в текущий каталог после интерактивного выбора; инструмента нет |
| `evo list-images` | hidden | `list_images` | устаревшая, скрытая; свой код engine.ListImages (общий каталог), инструмент list_images добавляет образы проекта |
| `evo list-zones` | engine | `list_zones` | свой код → Cloud.ListAvailabilityZones; дубль evo vm zones с другим форматом --json |
| `evo logs` | engine | `logs` | свой код → engine.Logs; у инструмента нет follow |
| `evo mcp` | meta | — | сам сервер MCP |
| `evo mcp healthcheck` | hidden | — | HEALTHCHECK Docker-образа |
| `evo project` | group | — | группа |
| `evo project list` | local | `org_projects` | свой код listProjects (configure.go) → Organization API; по смыслу = org_projects, формат ответа другой |
| `evo project use` | local | — | пишет проект в профиль на диске |
| `evo status` | engine | `status` | свой код → engine.Status; инструмент status читает state и CheckCloud сам, без withNames |
| `evo tools` | meta | — | группа |
| `evo tools call` | meta | — | универсальный вызов любого инструмента |
| `evo tools describe` | meta | — | описание инструмента (англ.) |
| `evo tools list` | meta | — | список инструментов |
| `evo verify` | local | — | TCP/HTTP-проверки портов развёрнутого приложения с машины оператора; инструмента нет |
| `evo version` | local | — | версия бинаря; MCP отдаёт её в initialize.serverInfo |
| `evo whoami` | local | — | профиль, маскированный Key ID, проект, срок токена; инструмента нет |

## 4. Флаги команд деревьев против схемы инструмента

Ожидаемый набор флагов выводится из схемы по правилам trees.go (snake_case → kebab-case, минус позиционные, Omit и rootArgs, плюс Rename) и сравнивается с docs/commands.md. Пусто — расхождений нет.

Расхождений нет: флаги всех 144 команд деревьев выведены из схемы.

## 5. Русская справка

| Что | Есть по-русски | Всего |
|---|---|---|
| краткое описание инструмента (ShortRU) | 152 | 152 инструментов |
| подробное описание инструмента (DescriptionRU) | 152 | 152 инструментов |
| описания аргументов (ArgDescriptionsRU) | 444 | 444 аргументов |
| из них флаги команд деревьев | 222 | 222 флагов |
| описания инструментов в MCP | 0 | 152 (всегда английские) |
