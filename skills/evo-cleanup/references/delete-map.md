# What lists a resource and what deletes it

In the order of deletion. Every delete takes its preview first (the call
without `confirmed`, `--dry-run` in the CLI), then the person's yes, then
the call with `confirmed: true` / `--yes`. Verify with the list (the
resource is gone) or with its `*_get` (`NOT_FOUND`).

| # | Resource | List | Delete (MCP / CLI) | Id from the list |
|---|---|---|---|---|
| 1 | deployment of a directory | `status`, `list` | `destroy` / `evo destroy` | run in that directory |
| 2 | DNS record | `dns_records` | `dns_record_delete` / `evo dns record-delete` | `record_id` |
| 3 | DNS zone | `dns_zones` | `dns_zone_delete` / `evo dns zone-delete` | `id` |
| 4 | load balancer | `lb_list` | `lb_delete` / `evo lb delete` | `id` |
| 5 | target group | `tg_list` | `tg_delete` / `evo tg delete` | `id` |
| 6 | Kubernetes cluster | `k8s_list` | `k8s_delete` / `evo k8s delete` | `cluster_id` |
| 7 | VM | `vm_list` | `destroy` with `vm_id` / `evo vm delete` | `vm_id` |
| 8 | disk not attached | `disk_list` | `disk_delete` / `evo disk delete` | `disk_id` |
| 9 | free floating IP | `list_floating_ips` | `release_floating_ip` / `evo vpc fip-release` | `id` |
| 10 | SNAT gateway | `snat_list` | `snat_delete` / `evo snat delete` | `id` |
| 11 | PostgreSQL cluster | `pg_list` | `pg_delete` / `evo pg delete` | `cluster_id` |
| 12 | Kafka cluster | `kafka_list` | `kafka_delete` / `evo kafka delete` | `cluster_id` |
| 13 | Redis cluster | `redis_list` | `redis_delete` / `evo redis delete` | `cluster_id` |
| 14 | static route | `vpc_routes` | `vpc_route_delete` / `evo vpc route-delete` | `vpc_id`, `route_id` |
| 15 | subnet | `list_subnets` | `subnet_delete` / `evo vpc subnet-delete` | `subnet_id` |
| 16 | VPC | `vpc_list` | `vpc_delete` / `evo vpc delete` | `id` |
| 17 | S3 object | `s3_ls` | `s3_rm` / `evo s3 rm` | `bucket`, `key` |
| 18 | S3 bucket, empty | `s3_buckets` | `s3_bucket_delete` / `evo s3 bucket-delete` | `bucket` |
| 19 | container app | `ca_list` | `ca_delete` / `evo ca delete` | `name` |
| 20 | secret | `secret_list` | `secret_delete` / `evo secret delete` | `path` |
| 21 | certificate | `cert_list` | `cert_delete` / `evo cert delete` | `id` |
| 22 | artifact registry | `ar_list` | `ar_delete` / `evo ar delete` | `id` |
| 23 | own image | `image_list` with `type: private` | `image_delete` / `evo image delete` | `image_id` |

Notes:

- PostgreSQL databases and users go with their cluster; delete them alone
  (`pg_db_delete`, `pg_user_delete`) only when the cluster stays.
- A bucket with objects is refused (`USAGE`) before the question: delete
  its objects first.
- A subnet with VMs, balancers or clusters in it is refused before the
  question (`can_delete` false): delete them first.
- A floating IP in use by a VM or a SNAT gateway is refused: it goes with
  the VM (`release_ip`) or the gateway.
- A cluster in FAILED may not be deletable through the API (DB-21,
  DB-17): do not repeat the delete; the way is Cloud.ru support or the
  console.
- Port rules opened with `open_ports` are not resources of a list here:
  close them with `close_ports` if the person knows the port, protocol and
  source.
