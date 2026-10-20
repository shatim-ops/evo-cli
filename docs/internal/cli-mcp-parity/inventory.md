# Инвентаризация CLI ↔ MCP

<!-- Сгенерировано: go run ./docs/internal/cli-mcp-parity/inventory/main.go > docs/internal/cli-mcp-parity/inventory.md. Руками не править. -->

Инструментов в реестре: 152 (изменяющих: 65). Команд дерева: 72. Команд в docs/commands.md: 108 (+3 скрытых).

## 1. Инструменты по способу вызова из CLI

| Способ | Инструментов | из них изменяющих |
|---|---|---|
| дерево | 72 | 34 |
| движок | 5 | 1 |
| tools call | 75 | 30 |

«дерево» — своя команда, построенная из CLISpec инструмента в реестре; «движок» — команда верхнего уровня со своим кодом cobra, которая делает то же через engine, а не через реестр; «tools call» — доступен из CLI только как `evo tools call <имя>`. «движок» и «tools call» вместе — временный список `tools.CLIPending` (80): тест TestParity_CommandsAndTools не даёт ему устареть, PR 3 и 5 плана его опустошают.

### По группам

| Группа | Всего | дерево | движок | tools call | RU-справка (short/long) |
|---|---|---|---|---|---|
| ar | 5 | 0 | 0 | 5 | 0/0 |
| audit | 1 | 0 | 0 | 1 | 0/0 |
| billing | 3 | 0 | 0 | 3 | 0/0 |
| ca | 7 | 0 | 0 | 7 | 0/0 |
| cert | 10 | 0 | 0 | 10 | 0/0 |
| deploy | 8 | 1 | 3 | 4 | 1/1 |
| disk | 7 | 7 | 0 | 0 | 7/7 |
| dns | 9 | 0 | 0 | 9 | 0/0 |
| iam | 1 | 0 | 0 | 1 | 0/0 |
| image | 6 | 3 | 1 | 2 | 3/3 |
| k8s | 15 | 15 | 0 | 0 | 15/15 |
| kafka | 7 | 0 | 0 | 7 | 0/0 |
| monitoring | 2 | 0 | 0 | 2 | 0/0 |
| nlb | 9 | 0 | 0 | 9 | 0/0 |
| org | 2 | 0 | 1 | 1 | 0/0 |
| pg | 15 | 15 | 0 | 0 | 15/15 |
| redis | 7 | 0 | 0 | 7 | 0/0 |
| s3 | 7 | 7 | 0 | 0 | 7/7 |
| secret | 6 | 6 | 0 | 0 | 6/6 |
| snat | 3 | 0 | 0 | 3 | 0/0 |
| stack | 4 | 0 | 0 | 4 | 0/0 |
| vm | 5 | 5 | 0 | 0 | 5/5 |
| vpc | 13 | 13 | 0 | 0 | 13/13 |

## 2. Инструмент → команда CLI

| Группа | Инструмент | Меняет | Способ | Команда CLI | RU short/long |
|---|---|---|---|---|---|
| ar | `ar_create` | да | tools call | `evo tools call ar_create` | нет/нет |
| ar | `ar_delete` | да | tools call | `evo tools call ar_delete` | нет/нет |
| ar | `ar_get` | нет | tools call | `evo tools call ar_get` | нет/нет |
| ar | `ar_list` | нет | tools call | `evo tools call ar_list` | нет/нет |
| ar | `ar_operation` | нет | tools call | `evo tools call ar_operation` | нет/нет |
| audit | `audit_events` | нет | tools call | `evo tools call audit_events` | нет/нет |
| billing | `billing_balance` | нет | tools call | `evo tools call billing_balance` | нет/нет |
| billing | `billing_consumption` | нет | tools call | `evo tools call billing_consumption` | нет/нет |
| billing | `billing_finops` | нет | tools call | `evo tools call billing_finops` | нет/нет |
| ca | `ca_delete` | да | tools call | `evo tools call ca_delete` | нет/нет |
| ca | `ca_deploy` | да | tools call | `evo tools call ca_deploy` | нет/нет |
| ca | `ca_get` | нет | tools call | `evo tools call ca_get` | нет/нет |
| ca | `ca_list` | нет | tools call | `evo tools call ca_list` | нет/нет |
| ca | `ca_logs` | нет | tools call | `evo tools call ca_logs` | нет/нет |
| ca | `ca_start` | да | tools call | `evo tools call ca_start` | нет/нет |
| ca | `ca_stop` | да | tools call | `evo tools call ca_stop` | нет/нет |
| cert | `cert_delete` | да | tools call | `evo tools call cert_delete` | нет/нет |
| cert | `cert_get` | нет | tools call | `evo tools call cert_get` | нет/нет |
| cert | `cert_import` | да | tools call | `evo tools call cert_import` | нет/нет |
| cert | `cert_issue_le` | да | tools call | `evo tools call cert_issue_le` | нет/нет |
| cert | `cert_list` | нет | tools call | `evo tools call cert_list` | нет/нет |
| cert | `cert_operation` | нет | tools call | `evo tools call cert_operation` | нет/нет |
| cert | `cert_private_key` | нет | tools call | `evo tools call cert_private_key` | нет/нет |
| cert | `cert_renew` | да | tools call | `evo tools call cert_renew` | нет/нет |
| cert | `cert_usage` | нет | tools call | `evo tools call cert_usage` | нет/нет |
| cert | `cert_versions` | нет | tools call | `evo tools call cert_versions` | нет/нет |
| deploy | `deploy` | да | движок | `evo deploy` (свой код), иначе `evo tools call deploy` | нет/нет |
| deploy | `deploy_apply` | да | tools call | `evo tools call deploy_apply` | нет/нет |
| deploy | `deploy_plan` | нет | tools call | `evo tools call deploy_plan` | нет/нет |
| deploy | `destroy` | да | дерево | `evo vm delete`, `evo destroy` (движок) | да/да |
| deploy | `list` | нет | tools call | `evo tools call list` | нет/нет |
| deploy | `logs` | нет | движок | `evo logs` (свой код), иначе `evo tools call logs` | нет/нет |
| deploy | `open_ports` | да | tools call | `evo tools call open_ports` | нет/нет |
| deploy | `status` | нет | движок | `evo status` (свой код), иначе `evo tools call status` | нет/нет |
| disk | `disk_attach` | да | дерево | `evo disk attach` | да/да |
| disk | `disk_create` | да | дерево | `evo disk create` | да/да |
| disk | `disk_delete` | да | дерево | `evo disk delete` | да/да |
| disk | `disk_detach` | да | дерево | `evo disk detach` | да/да |
| disk | `disk_list` | нет | дерево | `evo disk list` | да/да |
| disk | `disk_resize` | да | дерево | `evo disk resize` | да/да |
| disk | `list_disk_types` | нет | дерево | `evo disk types` | да/да |
| dns | `dns_operation` | нет | tools call | `evo tools call dns_operation` | нет/нет |
| dns | `dns_record_create` | да | tools call | `evo tools call dns_record_create` | нет/нет |
| dns | `dns_record_delete` | да | tools call | `evo tools call dns_record_delete` | нет/нет |
| dns | `dns_record_get` | нет | tools call | `evo tools call dns_record_get` | нет/нет |
| dns | `dns_records` | нет | tools call | `evo tools call dns_records` | нет/нет |
| dns | `dns_zone_create` | да | tools call | `evo tools call dns_zone_create` | нет/нет |
| dns | `dns_zone_delete` | да | tools call | `evo tools call dns_zone_delete` | нет/нет |
| dns | `dns_zone_get` | нет | tools call | `evo tools call dns_zone_get` | нет/нет |
| dns | `dns_zones` | нет | tools call | `evo tools call dns_zones` | нет/нет |
| iam | `iam_service_accounts` | нет | tools call | `evo tools call iam_service_accounts` | нет/нет |
| image | `image_create` | да | tools call | `evo tools call image_create` | нет/нет |
| image | `image_delete` | да | tools call | `evo tools call image_delete` | нет/нет |
| image | `image_get` | нет | дерево | `evo image get` | да/да |
| image | `image_list` | нет | дерево | `evo image list` | да/да |
| image | `list_images` | нет | движок | `evo list-images` (свой код), иначе `evo tools call list_images` | нет/нет |
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
| kafka | `kafka_create` | да | tools call | `evo tools call kafka_create` | нет/нет |
| kafka | `kafka_delete` | да | tools call | `evo tools call kafka_delete` | нет/нет |
| kafka | `kafka_get` | нет | tools call | `evo tools call kafka_get` | нет/нет |
| kafka | `kafka_list` | нет | tools call | `evo tools call kafka_list` | нет/нет |
| kafka | `kafka_operation` | нет | tools call | `evo tools call kafka_operation` | нет/нет |
| kafka | `kafka_specs` | нет | tools call | `evo tools call kafka_specs` | нет/нет |
| kafka | `kafka_versions` | нет | tools call | `evo tools call kafka_versions` | нет/нет |
| monitoring | `metrics_history` | нет | tools call | `evo tools call metrics_history` | нет/нет |
| monitoring | `metrics_scrape` | нет | tools call | `evo tools call metrics_scrape` | нет/нет |
| nlb | `lb_create` | да | tools call | `evo tools call lb_create` | нет/нет |
| nlb | `lb_delete` | да | tools call | `evo tools call lb_delete` | нет/нет |
| nlb | `lb_get` | нет | tools call | `evo tools call lb_get` | нет/нет |
| nlb | `lb_list` | нет | tools call | `evo tools call lb_list` | нет/нет |
| nlb | `lb_operation` | нет | tools call | `evo tools call lb_operation` | нет/нет |
| nlb | `tg_create` | да | tools call | `evo tools call tg_create` | нет/нет |
| nlb | `tg_delete` | да | tools call | `evo tools call tg_delete` | нет/нет |
| nlb | `tg_get` | нет | tools call | `evo tools call tg_get` | нет/нет |
| nlb | `tg_list` | нет | tools call | `evo tools call tg_list` | нет/нет |
| org | `org_projects` | нет | движок | `evo project list` (свой код), иначе `evo tools call org_projects` | нет/нет |
| org | `org_quotas` | нет | tools call | `evo tools call org_quotas` | нет/нет |
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
| redis | `redis_create` | да | tools call | `evo tools call redis_create` | нет/нет |
| redis | `redis_delete` | да | tools call | `evo tools call redis_delete` | нет/нет |
| redis | `redis_get` | нет | tools call | `evo tools call redis_get` | нет/нет |
| redis | `redis_list` | нет | tools call | `evo tools call redis_list` | нет/нет |
| redis | `redis_operation` | нет | tools call | `evo tools call redis_operation` | нет/нет |
| redis | `redis_specs` | нет | tools call | `evo tools call redis_specs` | нет/нет |
| redis | `redis_versions` | нет | tools call | `evo tools call redis_versions` | нет/нет |
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
| snat | `snat_create` | да | tools call | `evo tools call snat_create` | нет/нет |
| snat | `snat_delete` | да | tools call | `evo tools call snat_delete` | нет/нет |
| snat | `snat_list` | нет | tools call | `evo tools call snat_list` | нет/нет |
| stack | `stack_apply` | да | tools call | `evo tools call stack_apply` | нет/нет |
| stack | `stack_destroy` | да | tools call | `evo tools call stack_destroy` | нет/нет |
| stack | `stack_plan` | нет | tools call | `evo tools call stack_plan` | нет/нет |
| stack | `stack_status` | нет | tools call | `evo tools call stack_status` | нет/нет |
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
| `evo disk attach` | `disk_attach` | disk_id | — | — | confirmed | — |
| `evo disk create` | `disk_create` | name | — | — | confirmed | — |
| `evo disk delete` | `disk_delete` | disk_id | — | — | confirmed | — |
| `evo disk detach` | `disk_detach` | disk_id | — | — | confirmed | — |
| `evo disk list` | `disk_list` | — | — | — | — | — |
| `evo disk resize` | `disk_resize` | disk_id | — | — | confirmed | — |
| `evo disk types` | `list_disk_types` | — | — | — | — | — |
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
| `evo marketplace list` | `marketplace_list` | query? | — | — | — | render |
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

Расхождений нет: флаги всех 72 команд деревьев выведены из схемы.

## 5. Русская справка

| Что | Есть по-русски | Всего |
|---|---|---|
| краткое описание инструмента (toolShortRU) | 72 | 152 инструментов |
| подробная справка инструмента (toolLongRU) | 72 | 152 инструментов |
| описания флагов команд деревьев (flagShortRU) | 112 | 112 флагов |
| описания инструментов в MCP и `evo tools describe` | 0 | 152 (всегда английские) |
