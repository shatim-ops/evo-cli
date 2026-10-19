# Каталог инструментов

<!-- Файл сгенерирован из реестра инструментов. Руками не править:
     go test ./internal/cli -run TestToolsDoc -update-docs
     CI сверяет файл с реестром (TestToolsDoc). -->

Инструменты evo (152) — те же, что у MCP-сервера ([mcp.md](mcp.md)) и у
`evo tools call`. Здесь — первая фраза описания; полное описание и схема
аргументов — `evo tools describe <имя>`. «Меняет»: «да» — инструмент
создаёт или меняет ресурсы и без `confirmed=true` (в CLI — `--yes`)
только показывает превью; «удаляет» — может удалить или перезаписать данные;
«нет, секрет» — ничего не меняет, но показывает секрет открытым текстом и
тоже требует `confirmed=true` (`--yes`).
Группы — для `evo mcp --toolsets` и `evo tools list --group`.

## ar

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `ar_list` | нет | — | List Artifact Registry registries in the project (id, name, type, status, public, tariff). |
| `ar_get` | нет | `id` | Get full details of one Artifact Registry registry by id (from ar_list). |
| `ar_create` | да | `name` | Create an Artifact Registry registry (async); refuses a name an existing registry has (ar_list). |
| `ar_delete` | удаляет | `id` | Delete an Artifact Registry registry by id (from ar_list) AND its artifacts (async). |
| `ar_operation` | нет | `operation_id` | Poll an Artifact Registry async operation by id (from ar_create/ar_delete). |

## audit

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `audit_events` | нет | — | Audit Logging — who did what in the project (read-only): control-plane events (create/delete/modify of VMs, DBs, k8s, S3, IAM, …) with actor, action, resource … |

## billing

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `billing_balance` | нет | — | Account balance for the billing agreement (RUB, active flag, raw agreement object). |
| `billing_consumption` | нет | `start` | Usage/consumption cost for a date range, by the agreement (CLOUDRU_AGREEMENT_ID) or, without it, by the project; scope in the answer says which. |
| `billing_finops` | нет | — | FinOps report (read-only): INFORM (cost allocation by service, anomalies vs the previous same-length period) + QUANTIFY (daily burn-rate, balance runway in … |

## ca

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `ca_list` | нет | — | List Container Apps (serverless containers) in the project (name, status). |
| `ca_get` | нет | `name` | Get a Container App by name — full details incl. |
| `ca_deploy` | да | `name`, `image` | Deploy (create+run) a Container App from a container image — serverless, scales to zero when idle. |
| `ca_delete` | удаляет | `name` | Delete a Container App by name. |
| `ca_logs` | нет | `name` | Get recent logs of a Container App: the last 100 lines by time, oldest first (app logs are the container's stdout/stderr). |
| `ca_start` | да | `name` | Start a stopped Container App (it is billed while running). |
| `ca_stop` | да | `name` | Stop a running Container App (it stops serving requests). |

## cert

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `cert_list` | нет | — | Certificate Manager — list TLS certificates in the project (read-only). |
| `cert_get` | нет | `id` | Certificate Manager — get one certificate by id (metadata, primary version, expiry, status). |
| `cert_issue_le` | да | `name`, `dns_names` | Certificate Manager — issue a FREE Let's Encrypt TLS certificate for one or more domains (async). |
| `cert_import` | да | `name`, `server_certificate`, `private_key` | Certificate Manager — import a user-managed certificate (bring-your-own). |
| `cert_renew` | да | `certificate_id` | Certificate Manager — trigger Let's Encrypt renewal for a certificate (async). |
| `cert_delete` | удаляет | `id` | Certificate Manager — permanently delete a certificate. |
| `cert_usage` | нет | `id` | Certificate Manager — list resources (load balancers/listeners) that USE a certificate. |
| `cert_versions` | нет | `certificate_id` | Certificate Manager — list a certificate's versions (id, CN, DNS names, status, expiry), all pages. |
| `cert_private_key` | нет, секрет | `certificate_id` | Certificate Manager — return the SECRET private key for a version: privateKey (private_key in the live API) is PEM text (-----BEGIN …), decoded from the API's … |
| `cert_operation` | нет | `operation_id` | Certificate Manager — poll an async operation (issue/renew) by id until done=true. |

## deploy

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `deploy` | да | — | Deploy the local docker-compose project to a Cloud.ru VM. |
| `status` | нет | — | Show the current deployment status, including VM ID, public IP, flavor, region, health state and exposed ports, from the local state. |
| `list` | нет | — | List all recorded deployments for this project (VM IP, status, flavor, creation time). |
| `logs` | нет | — | Retrieve container logs from the deployed VM via 'docker compose logs' over SSH. |
| `destroy` | удаляет | — | Permanently delete a VM and all data on it (irreversible). |
| `deploy_plan` | нет | — | STEP 1 of the deploy wizard (read-only, creates nothing). |
| `deploy_apply` | да | `plan_id` | STEP 2 of the deploy wizard. |
| `open_ports` | да | `security_group`, `ports` | Open inbound ports on a Cloud.ru security group (add ingress rules, tcp, from 0.0.0.0/0 by default). |

## disk

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `list_disk_types` | нет | — | List available Cloud.ru disk types (UUID + name, e.g. |
| `disk_list` | нет | — | List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs). |
| `disk_create` | да | `name`, `size_gb` | Create a block-storage disk. |
| `disk_attach` | да | `disk_id`, `vm` | Attach an existing disk to a VM (POST /disks/{id}/attach). |
| `disk_detach` | да | `disk_id`, `vm` | Detach a disk from a VM (POST /disks/{id}/detach). |
| `disk_resize` | да | `disk_id`, `size_gb` | Grow a disk in place via PUT /disks/{id} (up to 16384 GB). |
| `disk_delete` | удаляет | `disk_id` | Permanently delete a disk and all its data (irreversible). |

## dns

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `dns_zones` | нет | — | List Evolution DNS zones in the project (both public and private), with id, name, domain, visibility, state (and confirm_state of a public zone), valid … |
| `dns_zone_get` | нет | `id` | Get full details of one DNS zone by id (from dns_zones). |
| `dns_zone_create` | да | `domain` | Create a DNS zone. |
| `dns_zone_delete` | удаляет | `id` | Delete a DNS zone by id (from dns_zones) AND its records (async). |
| `dns_operation` | нет | `operation_id` | Poll a DNS async operation by id (from dns_zone_create/delete, dns_record_create/delete). |
| `dns_records` | нет | `zone_id` | List resource records of a DNS zone (id, name, type, values, ttl). |
| `dns_record_get` | нет | `record_id` | Get one DNS record by id (from dns_records). |
| `dns_record_create` | да | `zone_id`, `name`, `record_type`, `value` или `values` | Add a DNS record to a zone (async). |
| `dns_record_delete` | удаляет | `record_id` | Delete a DNS record by id (from dns_records) (async). |

## iam

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `iam_service_accounts` | нет | — | List IAM service accounts for a project (id, name, email, type, enabled). |

## image

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `image_list` | нет | — | Search the VM image catalogue: OS images (type public), Cloud.ru Marketplace products (marketplace) and the project's own images (private). |
| `marketplace_list` | нет | — | Read-only showcase of what Cloud.ru offers to deploy, in one list: VM images of the shared catalogue (OS images and Cloud.ru Marketplace products; the … |
| `list_images` | нет | — | List available Cloud.ru VM boot images (UUID + name). |
| `image_create` | да | `name`, `disk_id` | Create a reusable VM image FROM A BOOTABLE DISK — the final step of producing a distributable image (e.g. |
| `image_get` | нет | `image` или `image_id` | Get one VM image by name, display name or UUID (resolved like image_list: exact name, exact display_name, then a unique substring; several matches → USAGE with … |
| `image_delete` | удаляет | `image_id` | Delete a VM image. |

## k8s

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `k8s_list` | нет | — | List Managed Kubernetes (mk8s) clusters in the project (id, name, status). |
| `k8s_get` | нет | `cluster_id` | Get a Managed Kubernetes cluster by id (full details incl. |
| `k8s_options` | нет | — | List available Kubernetes versions (by release channel), node flavors and zones for mk8s. |
| `k8s_plan` | нет | — | STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master count, subnet, service account) WITHOUT creating anything. |
| `k8s_create` | да | — | STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan. |
| `k8s_kubeconfig` | нет | `cluster_id` | Get the kubeconfig for a Managed Kubernetes cluster. |
| `k8s_delete` | удаляет | `cluster_id` | Permanently delete a Managed Kubernetes cluster. |
| `k8s_nodepools` | нет | `cluster_id` | List node pools of one Managed Kubernetes cluster (name, state, desired/ready nodes). |
| `k8s_add_nodepool` | да | `cluster_id` | Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has none). |
| `k8s_addons` | нет | — | Search the Managed Kubernetes addon catalogue (Cloud.ru-packaged plugins: ingress, cert-manager, monitoring, CSI drivers, …). |
| `k8s_addon_versions` | нет | `name` | List the releases of one Managed Kubernetes addon, newest first: version (appVersion, what k8s_add_addon takes), addon_version, mode (ACTIVE, EXPERIMENTAL … |
| `k8s_add_addon` | удаляет | `cluster_id`, `name` | Install a Managed Kubernetes addon release into a cluster. |
| `k8s_kubectl` | удаляет | `cluster_id`, `args` | Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed). |
| `k8s_apply` | удаляет | `cluster_id`, `manifest` | Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -f -`. |
| `k8s_helm` | удаляет | `cluster_id`, `action` | Drive Helm on a cluster: action=install\|upgrade\|uninstall\|list\|status. |

## kafka

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `kafka_list` | нет | — | List Managed Kafka clusters (id, name, status, brokers, version). |
| `kafka_get` | нет | `cluster_id` | Get a Managed Kafka cluster by id (status, kafka_hosts, brokers, version). |
| `kafka_versions` | нет | — | List available Managed Kafka versions (id + name). |
| `kafka_specs` | нет | — | List Managed Kafka flavor specifications (id, cpu, memory_gb, min_storage_gb, min/max_brokers) for a version. |
| `kafka_create` | да | `name` | Create a Managed Kafka cluster. |
| `kafka_delete` | удаляет | `cluster_id` | Delete a Managed Kafka cluster by id AND its data. |
| `kafka_operation` | нет | `operation_id` | Poll a Managed Kafka async operation by id (from kafka_create/kafka_delete). |

## monitoring

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `metrics_history` | нет | `match` | Service Monitoring — read a metric's history over a recent window (read-only). |
| `metrics_scrape` | нет | — | Service Monitoring — current metric values in Prometheus text exposition (read-only snapshot of all/matched metrics right now). |

## nlb

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `lb_list` | нет | — | List Evolution Load Balancers (L4/network) in the project (id, name, status, vpc, external_ip). |
| `lb_get` | нет | `id` | Get full details of one load balancer by id (from lb_list): rules, listeners, addresses, AZs, status. |
| `lb_operation` | нет | `operation_id` | Poll a Load Balancer async operation by id (from lb_create/lb_delete/tg_create/tg_delete). |
| `tg_list` | нет | — | List Load Balancer target groups in the project (id, name, status, vpc). |
| `tg_get` | нет | `id` | Get details of one target group by id (from tg_list): member VMs, vpc, status. |
| `tg_create` | да | `name`, `vm_id` или `targets` | Create a target group (the set of VMs a load balancer distributes traffic to). |
| `tg_delete` | удаляет | `id` | Delete a target group by id (from tg_list). |
| `lb_create` | да | `name`, `target_group_id`, `port` | Create an L4 load balancer fronting a target group. |
| `lb_delete` | удаляет | `id` | Delete a load balancer by id (from lb_list). |

## org

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `org_projects` | нет | — | List Cloud.ru projects visible to the caller (id, name, status, org unit). |
| `org_quotas` | нет | — | List quota usages (resource, limit, current usage) — for capacity planning. |

## pg

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `pg_list` | нет | — | List Managed PostgreSQL clusters in the project (id, name, status, version, health). |
| `pg_get` | нет | `id` или `cluster_id` | Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storage, connection_string, subnets, status. |
| `pg_versions` | нет | — | List available Managed PostgreSQL versions (e.g. |
| `pg_specs` | нет | — | List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard\|business, min_storage_gb) for a version. |
| `pg_create` | да | `name` | Create a Managed PostgreSQL cluster. |
| `pg_delete` | удаляет | `id` или `cluster_id` | Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data, including all its backups (they cannot be restored). |
| `pg_operation` | нет | `operation_id` | Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create). |
| `pg_databases` | нет | `cluster_id` | List databases in a PostgreSQL cluster (name, owner, size). |
| `pg_db_create` | да | `cluster_id`, `name`, `owner` | Create a database in a PostgreSQL cluster. |
| `pg_db_delete` | удаляет | `cluster_id`, `name` | Drop a database in a PostgreSQL cluster. |
| `pg_users` | нет | `cluster_id` | List users of a PostgreSQL cluster (name, granted roles). |
| `pg_user_create` | да | `cluster_id`, `name`, `password` | Create a user in a PostgreSQL cluster. |
| `pg_user_delete` | удаляет | `cluster_id`, `name` | Delete a user in a PostgreSQL cluster. |
| `pg_backups` | нет | `cluster_id` | List backups of a PostgreSQL cluster (name, status, size, timestamps). |
| `pg_backup_create` | да | `cluster_id`, `name` | Start a manual backup of a PostgreSQL cluster (async). |

## redis

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `redis_list` | нет | — | List Managed Redis clusters (id, name, status, type, shards, replicas). |
| `redis_get` | нет | `cluster_id` | Get a Managed Redis cluster by id (status, redis_hosts, type, shards, replicas). |
| `redis_versions` | нет | — | List available Managed Redis versions (id + name). |
| `redis_specs` | нет | — | List Managed Redis flavor specifications (id, cpu, memory_gb, deployment_mode, min_storage_gb) for a version. |
| `redis_create` | да | `name` | Create a Managed Redis cluster. |
| `redis_delete` | удаляет | `cluster_id` | Delete a Managed Redis cluster by id AND its data. |
| `redis_operation` | нет | `operation_id` | Poll a Managed Redis async operation by id (from redis_create/redis_delete). |

## s3

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `s3_buckets` | нет | — | List Object Storage (S3) buckets. |
| `s3_ls` | нет | `bucket` | List objects in an S3 bucket (key, size, last_modified). |
| `s3_get` | нет | `bucket`, `key` | Download an S3 object up to 10 MiB (a larger one is refused with USAGE: use an S3 client or the console). |
| `s3_put` | удаляет | `bucket`, `key` | Upload an S3 object from inline content (content) or base64 (content_base64). |
| `s3_rm` | удаляет | `bucket`, `key` | Delete an S3 object. |
| `s3_bucket_create` | да | `bucket` | Create an Object Storage (S3) bucket. |
| `s3_bucket_delete` | удаляет | `bucket` | Delete an EMPTY S3 bucket. |

## secret

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `secret_list` | нет | — | List secrets in the project (id, name, path, description) from Cloud.ru Secret Management. |
| `secret_get` | нет | `path` | Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key. |
| `secret_value` | нет, секрет | `path` | Retrieve the DECRYPTED value of a secret by path. |
| `secret_create` | да | `path`, `value` | Store a new secret at a path (value is Base64-encoded internally; never echoed back). |
| `secret_add_version` | да | `path`, `value` | Add a new value version to an existing secret (rotation). |
| `secret_delete` | удаляет | `path` | Delete a secret by path AND all its versions. |

## snat

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `snat_list` | нет | — | List SNAT gateways in the project. |
| `snat_create` | да | `name` | Create a public SNAT gateway: outbound internet for every ROUTED subnet of its zone (an isolated subnet is not served; a VM with its own public IP goes out … |
| `snat_delete` | удаляет | `id` | Delete a SNAT gateway by id (from snat_list). |

## stack

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `stack_plan` | нет | `manifest` | Plan a multi-resource STACK from ONE declarative manifest. |
| `stack_apply` | да | `job_id` | Apply a planned stack (from stack_plan). |
| `stack_status` | нет | `job_id` | Poll a stack deploy: returns COMPACT progress {status, done[], running[], pending[], outputs{postgres,vm,tg,lb,…; lb_external_ip once the balancer has a public … |
| `stack_destroy` | удаляет | `job_id` | Tear down every resource in a stack (reverse order, idempotent). |

## vm

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `provision` | да | — | Provision a bare Cloud.ru VM. |
| `vm_get` | нет | `vm_id` или `name` | Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (public_ip), state, and every network interface. |
| `vm_list` | нет | — | List the project's VMs from the LIVE API: id, name, state, availability zone and creation time. |
| `list_zones` | нет | — | List available Cloud.ru availability zones (UUID + name). |
| `list_flavors` | нет | — | List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate each. |

## vpc

| Инструмент | Меняет | Обязательные аргументы | Описание |
|---|---|---|---|
| `fip_attach` | да | `fip`, `vm` | Bind an existing FREE floating IP to a VM's primary interface, giving it a public address. |
| `list_subnets` | нет | — | List Cloud.ru subnets (UUID, name, availability zone). |
| `list_security_groups` | нет | — | List Cloud.ru security groups (UUID, name, availability zone). |
| `list_floating_ips` | нет | — | List Cloud.ru floating IPs (address, state, whether available to attach). |
| `release_floating_ip` | удаляет | `id` или `ip_address` | Release (delete) a floating IP, freeing it from the project. |
| `vpc_list` | нет | — | List Evolution VPCs (virtual private clouds) in the project (id, name, default, type). |
| `vpc_get` | нет | `id` | Get full details of one VPC by id (from vpc_list). |
| `vpc_create` | да | `name` | Create a new VPC (virtual private cloud) (async). |
| `vpc_delete` | удаляет | `id` | Delete a VPC by id (from vpc_list) (async). |
| `vpc_operation` | нет | `operation_id` | Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete). |
| `vpc_routes` | нет | `vpc_id` | List static routes of a VPC (id, subnet, next_hop_type, status). |
| `vpc_route_create` | да | `vpc_id`, `subnet`, `next_hop_vip_id` или `next_hop_vm_id` или `next_hop_magic_router_id` | Add a static route to a VPC (async). |
| `vpc_route_delete` | удаляет | `vpc_id`, `route_id` | Delete a static route from a VPC by id (from vpc_routes) (async). |
