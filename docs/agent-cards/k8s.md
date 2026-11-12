# evo: the `k8s` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group k8s' prints it. -->

17 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `k8s_list` | `evo k8s list` | reads |  |
| `k8s_get` | `evo k8s get` | reads | `cluster_id` ← `k8s_list` |
| `k8s_options` | `evo k8s options` | reads |  |
| `k8s_plan` | `evo k8s plan` | reads |  |
| `k8s_create` | `evo k8s create` | creates: id in `cluster_id`; found with `k8s_list`; deleted with `k8s_delete` |  |
| `k8s_kubeconfig` | `evo k8s kubeconfig` | returns a kubeconfig; mints a token of the cluster | `cluster_id` ← `k8s_list` |
| `k8s_delete` | `evo k8s delete` | deletes; a repeat answers `already_deleted` | `cluster_id` ← `k8s_list` |
| `k8s_operation` | `evo k8s operation` | reads |  |
| `k8s_nodepools` | `evo k8s nodepools` | reads | `cluster_id` ← `k8s_list` |
| `k8s_nodepool_get` | `evo k8s nodepool-get` | reads | `nodepool_id` ← `k8s_nodepools` |
| `k8s_add_nodepool` | `evo k8s add-nodepool` | creates: id in `nodepool_id`; found with `k8s_nodepools` | `cluster_id` ← `k8s_list`, `security_group_id` ← `list_security_groups` |
| `k8s_addons` | `evo k8s addons` | reads |  |
| `k8s_addon_versions` | `evo k8s addon-versions` | reads |  |
| `k8s_add_addon` | `evo k8s add-addon` | creates: an addon of a cluster, known by its name; it goes with the cluster | `cluster_id` ← `k8s_list` |
| `k8s_kubectl` | `evo k8s kubectl` | changes | `cluster_id` ← `k8s_list` |
| `k8s_apply` | `evo k8s apply` | changes | `cluster_id` ← `k8s_list` |
| `k8s_helm` | `evo k8s helm` | changes | `cluster_id` ← `k8s_list` |

A change of this group that answers an `operation_id` runs on: wait for it with `k8s_operation` and its `wait_seconds`, not by polling a list.

Filters of the lists (the rule of the `evo` skill): `k8s_list` — status, name; `k8s_nodepools` — status, name; `k8s_addons` — query, category, billable.

## Notes

- A cluster reaches the internet only through a public SNAT gateway in the nodes' zone (`snat_create`): without one images and charts are not pulled, pods stay in ImagePullBackOff.
- A chart needs nodes of its size: monitoring stacks want nodes of 8 GB or more.
- An addon is ready when its pods are Running and Ready; the pods of a Job (e.g. cert-manager-startupapicheck) end Completed.
- Cloud.ru substitutes {{clusterID}}, {{clusterName}}, {{productInstanceID}}, {{nodeCIDR}} and {{serviceCIDR}} in the `config` of `k8s_add_addon`; the API installs an addon by app version and picks the addon_version itself.
