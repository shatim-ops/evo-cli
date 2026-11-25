// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

// inheritedErrorText only shrinks. Closes by 1.10 (release plan P1, lines
// 426/556 at c22064f4, #2761). File, function, kind and exact text identify each
// occurrence on main before P1. A new occurrence of the same text also fails.
var inheritedErrorText = map[string]int{
	"internal/artifact/client.go\tapiError\tcloud status\tartifact-registry API %d: %s": 1,

	"internal/audit/client.go\tapiError\tcloud status\taudit API %d: %s":                                                                                                 1,
	"internal/audit/client.go\tapiError\tcloud status\taudit API rate limit (audit API %d)":                                                                              1,
	"internal/audit/client.go\tapiError\tcloud status\taudit logging is not enabled for the project (audit API %d)":                                                      1,
	"internal/audit/client.go\tapiError\tcloud status\tno access to audit logs: the key's role lacks it, or audit logging is not enabled for the project (audit API %d)": 1,
	"internal/audit/client.go\tapiError\tcloud status\tаудит не включён для проекта (audit API %d)":                                                                      1,
	"internal/audit/client.go\tapiError\tcloud status\tнет доступа к аудит-логам: у роли ключа нет прав на них или аудит не включён для проекта (audit API %d)":          1,
	"internal/audit/client.go\tapiError\tcloud status\tпревышена частота запросов к аудиту (audit API %d)":                                                               1,

	"internal/billing/client.go\tClient.consumptionError\tcloud status\tno access to the consumption of agreement %s: the key's role lacks it (billing API %d)": 1,
	"internal/billing/client.go\tClient.consumptionError\tcloud status\tno access to the consumption of project %s: the key's role lacks it (billing API %d)":   1,
	"internal/billing/client.go\tClient.consumptionError\tcloud status\tнет доступа к потреблению договора %s: у роли ключа нет прав (billing API %d)":          1,
	"internal/billing/client.go\tClient.consumptionError\tcloud status\tнет доступа к потреблению проекта %s: у роли ключа нет прав (billing API %d)":           1,
	"internal/billing/client.go\tapiError\tcloud status\tbilling API %d: %s":                                                                                    1,

	"internal/certmanager/client.go\tapiError\tcloud status\tcertmanager API %d: %s": 1,

	"internal/cli/configure.go\tconfigureZones\tmessage\tno tool list_zones": 1,

	"internal/cli/engine_commands.go\tcallEngineTool\tmessage\tno tool %s": 1,

	"internal/cli/mcp.go\tcheckAllowedProject\tmessage\tproject %s is not allowed on this server": 1,

	"internal/compose/parse.go\tparseFloat\tmessage\tnot a finite number":                                    1,
	"internal/compose/parse.go\tparseFloat\tmessage\tnot a number":                                           1,
	"internal/compose/parse.go\tparseLongPort\tmessage\tport %v: target is required":                         1,
	"internal/compose/parse.go\tparseMemory\tmessage\tnot a byte value (use e.g. 512M or 1G)":                2,
	"internal/compose/parse.go\tparseMemory\tmessage\tout of range":                                          1,
	"internal/compose/parse.go\tparsePortNumber\tmessage\t%q is not a port number (1-65535)":                 1,
	"internal/compose/parse.go\tparsePortRange\tmessage\trange %q is reversed":                               1,
	"internal/compose/parse.go\tparsePortString\tmessage\tport %q: %v":                                       2,
	"internal/compose/parse.go\tparsePortString\tmessage\tport %q: bad IPv6 address":                         1,
	"internal/compose/parse.go\tparsePortString\tmessage\tport %q: expected [IP:][HOST:]CONTAINER":           1,
	"internal/compose/parse.go\tparsePortString\tmessage\tport %q: host and container ranges differ in size": 1,
	"internal/compose/parse.go\tparsePortString\tmessage\tport %q: unknown protocol %q":                      1,
	"internal/compose/parse.go\tparsePorts\tmessage\tports must be a list":                                   1,
	"internal/compose/parse.go\tparsePorts\tmessage\tunsupported port entry %v":                              1,
	"internal/compose/parse.go\tparseResources\tmessage\tcpus %v is not a number of CPUs":                    1,
	"internal/compose/parse.go\tparseResources\tmessage\tmemory %v: %v":                                      1,

	"internal/config/config.go\tsetFieldAny\tmessage\tunknown field %q":   1,
	"internal/config/config.go\ttoInt\tmessage\tcannot convert %T to int": 1,

	"internal/config/profile.go\t\tmessage\tlocked": 1,

	"internal/confirm/confirm.go\t\tmessage\tconfirmation required": 1,

	"internal/containerapp/client.go\tapiError\tcloud status\tcontainer-apps API %d: %s": 1,

	"internal/dbaas/client.go\tapiError\tcloud status\tdbaas API %d: %s": 1,

	"internal/deployplan/apply.go\tApplyWithTimeout\tmessage\tVM %s was created, but the create call failed; it is billed — call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), whether to delete it instead (destroy) — ask the person: %w":                               1,
	"internal/deployplan/apply.go\tApplyWithTimeout\tmessage\ta VM named %q already exists in another availability zone (%s); nothing was created — choose another name in deploy_plan":                                                                                                                            1,
	"internal/deployplan/apply.go\tfinishApply\tmessage\tVM %s was created, but deploy_apply was interrupted; it is still running — call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), check it with vm_get; whether to delete it instead (destroy) — ask the person: %w": 1,

	"internal/deployplan/resolve.go\tResolve\thint\t') in the Cloud.ru console, or plan in another zone.":                                           1,
	"internal/deployplan/resolve.go\tResolve\thint\tCreate a security group whose name contains the zone name (e.g. 'web_":                          1,
	"internal/deployplan/resolve.go\tResolve\tmessage\tcould not resolve availability zone %q to a UUID":                                            1,
	"internal/deployplan/resolve.go\tResolve\tmessage\tno security group for zone %s in the project":                                                1,
	"internal/deployplan/resolve.go\tResolve\tmessage\tno subnet found for project/zone %s — create a network+subnet in the Cloud.ru console first": 1,
	"internal/deployplan/resolve.go\tResolve\tmessage\tproject_id is required":                                                                      1,
	"internal/deployplan/resolve.go\tpickImage\tmessage\tno images available":                                                                       1,

	"internal/deployplan/store.go\tLoad\tmessage\tplan %q expired at %s; run deploy_plan again":                                                                                           1,
	"internal/deployplan/store.go\tLockApply\tmessage\tanother deploy_apply for VM %q is running; wait for it to finish, then call deploy_apply again (the VM is adopted, not recreated)": 1,

	"internal/dns/client.go\tAPIError.Error\tcloud status\tdns API %d: %s": 1,

	"internal/engine/deploy.go\tEngine.Deploy\tmessage\texit status %d":        1,
	"internal/engine/deploy.go\tEngine.prepareDocker\tmessage\texit status %d": 1,

	"internal/engine/deploy_preview.go\t\tmessage\tno compose file to size the VM by": 1,

	"internal/engine/destroy.go\t\tmessage\tVM deletion not confirmed": 1,

	"internal/engine/preview.go\t\tmessage\tdry run": 1,

	"internal/engine/quota.go\t\tmessage\tquotas: the key reads no quotas (403)": 1,

	"internal/engine/stage.go\t\tmessage\tthe stage still runs":                                 1,
	"internal/engine/stage.go\tInProgress.Error\tError()\tdeploy is still in progress: %s (%s)": 1,

	"internal/floatingip/floatingip.go\tAskError.Error\tError()\t, got no answer and is not listed: a person decides whether to reserve another": 1,
	"internal/floatingip/floatingip.go\tAskError.Error\tError()\t, requested ":                                                                   1,
	"internal/floatingip/floatingip.go\tAskError.Error\tError()\tfloating IP ":                                                                   1,
	"internal/floatingip/floatingip.go\tLostError.Error\tError()\t: the create got no answer: ":                                                  1,
	"internal/floatingip/floatingip.go\tLostError.Error\tError()\tfloating IP ":                                                                  1,
	"internal/floatingip/floatingip.go\tReserve\tmessage\tthe create answered no floating IP id":                                                 1,

	"internal/history/history.go\tAppend\tmessage\t%s is not a directory of evo":     1,
	"internal/history/history.go\tAppend\tmessage\toutcome %q is not of the journal": 1,

	"internal/iamadmin/client.go\tStatusError.Error\tcloud status\tiam API %d: %s": 1,
	"internal/iamadmin/client.go\tapiError\tcloud status\tiam API %d: %s":          4,

	"internal/iamadmin/login.go\tClient.BulkEvaluate\tmessage\tiam: bulk-evaluate answered %d results to %d checks": 1,
	"internal/iamadmin/login.go\tClient.RoleID\tmessage\tiam: no product role %s":                                   1,

	"internal/journal/journal.go\t\tmessage\tthe journal is empty": 1,

	"internal/kubetools/flags.go\tCheckConnectionFlags\tmessage\tkubectl flag %s is not allowed: evo runs kubectl with KUBERC=off, so that a preferences file (aliases, flag defaults) cannot change its commands":                                  1,
	"internal/kubetools/flags.go\tCheckConnectionFlags\tmessage\tkubectl flag %s is not allowed: it replaces the cluster or credentials of the kubeconfig evo writes (the cluster's token would go elsewhere); the cluster is chosen by cluster_id": 1,

	"internal/kubetools/kubetools.go\trun\tmessage\t%s not found on the host (install it in ~/.local/bin)": 1,

	"internal/login/idtoken.go\tConfig.jwk\tmessage\tjwk":     1,
	"internal/login/idtoken.go\tConfig.jwk\tmessage\tjwks":    1,
	"internal/login/idtoken.go\tConfig.jwk\tmessage\tno key ": 1,

	"internal/loginworld/world.go\tWorld.Browser\tmessage\tauthorize: %d": 1,

	"internal/mk8s/addons.go\tClient.CreateClusterAddon\tmessage\t%s; the install may have started — check the addons of the cluster in the Cloud.ru console before retrying": 1,
	"internal/mk8s/addons.go\tClient.CreateClusterAddon\tmessage\tmk8s operation %s failed: %s":                                                                               1,
	"internal/mk8s/addons.go\tflexInt.UnmarshalJSON\tmessage\treadyWorkers: %q is not an integer":                                                                             1,

	"internal/mk8s/client.go\tapiError\tcloud status\tmk8s API %d: %s":      1,
	"internal/mk8s/client.go\tapiError\tcloud status\tmk8s API %d: %s (%v)": 1,

	"internal/mk8s/kubeconfig.go\tEmbedToken\tmessage\tkubeconfig has no users to rewrite": 1,
	"internal/mk8s/kubeconfig.go\tfetchOIDCTokenOnce\tmessage\tOIDC token HTTP %d: %s":     1,

	"internal/mk8s/nodepool.go\tClient.ListNodePools\tmessage\tmk8s: node pools need a cluster ID": 1,

	"internal/monitoring/client.go\tapiError\tcloud status\tmonitoring API %d: %s": 1,

	"internal/nlb/client.go\tapiError\tcloud status\tnlb API %d: %s": 1,

	"internal/pager/pager.go\t\tmessage\tthe list is not complete": 1,

	"internal/postgresql/client.go\tapiError\tcloud status\tpostgresql API %d: %s": 1,

	"internal/receipt/receipt.go\t\tmessage\tno receipt": 1,

	"internal/s3/client.go\tTooLargeError.Error\tError()\tobject is %d bytes, over the %d-byte limit": 1,
	"internal/s3/client.go\tTooLargeError.Error\tError()\tobject is over the %d-byte limit":           1,

	"internal/secret/client.go\tapiError\tcloud status\tsecret API %d: %s": 1,

	"internal/state/lock.go\t\tmessage\theld by another process": 1,

	"internal/tools/namecheck.go\tnoID\tmessage\t id":                  1,
	"internal/tools/namecheck.go\tnoID\tmessage\tthe API answered no ": 1,

	"internal/tools/project_bound.go\t\tmessage\tproject not allowed on this server": 1,

	"internal/tools/registry.go\t\tmessage\tnot resolved by the call":                                           1,
	"internal/tools/registry.go\tParseToolsets\tmessage\tunknown toolset (item %d of the list); known: all, %s": 1,

	"internal/tools/tools.go\tRegistry.planDestroyTargeted\tmessage\tVM %s is not in this project's VM list, and asking for it by ID failed (%w); nothing was deleted and its local record is kept — call destroy again": 1,
	"internal/tools/tools.go\tRegistry.planDestroyTargeted\tmessage\tengine unavailable": 1,
	"internal/tools/tools.go\tRegistry.planProvision\tmessage\tengine unavailable":       1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tVM %s created but did not become active within %s; it is still being created and is billed, and it is recorded in the state — check it with vm_get; whether to delete it (destroy vm_id %s) — ask the person; to wait longer, set CLOUDRU_TIMEOUT (seconds).%s": 1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tVM %s entered state %s. It is recorded in the state; whether to delete it (destroy vm_id %s) — ask the person.%s%s":                                                                                                                                             1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tVM %s is active at %s and billed.%s": 1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tVM %s was created, but provision was interrupted (%w); it is still running and recorded in the state — check it with vm_get; whether to delete it (destroy vm_id %s), ask the person.%s": 1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tVM %s was created, but the create call failed (%v); it is billed and recorded in the state — check it with vm_get; whether to delete it (destroy vm_id %s), ask the person.%s":           1,
	"internal/tools/tools.go\tRegistry.provisionVM\tmessage\tno subnets in project — cannot attach a network interface":                                                                                                                               1,
	"internal/tools/tools.go\tprovisionNoPublicIP\thint\tAttach a free address with fip_attach (vm %s; addresses from list_floating_ips); whether to delete the VM instead (destroy vm_id=%s) — ask the person.":                                      1,
	"internal/tools/tools.go\tprovisionNoPublicIP\tmessage\tVM %s is active, but its public IP was not attached (%v)":                                                                                                                                 1,

	"internal/tools/tools_admin.go\torgHTTPError\tmessage\tOrganization API: ": 1,

	"internal/tools/tools_deploy.go\tRegistry.applyDeployPlan\thint\tAsk the user to start it in the Cloud.ru console, then call deploy_apply again with the same plan_id %s; whether to delete it instead (destroy vm_id %s) — ask the person. Retrying before it runs answers the same.": 1,
	"internal/tools/tools_deploy.go\tRegistry.applyDeployPlan\tmessage\tVM %s is %s: %s": 1,
	"internal/tools/tools_deploy.go\tRegistry.applyDeployPlan\tmessage\tVM %s was created, but the state file could not be saved (%w); it is running and billed — call deploy_apply again with the same plan_id once the state is writable, whether to delete it instead (destroy vm_id %s, or the Cloud.ru console) — ask the person": 1,
	"internal/tools/tools_deploy.go\tRegistry.localEngine\tmessage\tengine unavailable": 1,

	"internal/tools/tools_disk.go\tRegistry.planDiskCreate\tmessage\tdisk %s %s was created (it is billed), but not attached to VM %s: %v": 1,
	"internal/tools/tools_disk.go\tRegistry.planDiskCreate\tmessage\tthe create response has no disk id":                                   1,
	"internal/tools/tools_disk.go\tdiskAtLimit.Error\tError()\tthe disk is %s at the time limit of the call":                               1,
	"internal/tools/tools_disk.go\tdiskFailed.Error\tError()\tthe disk is in state ":                                                       1,
	"internal/tools/tools_disk.go\twaitDiskAvailable\tmessage\tthe disk did not become available within %s (state %s)":                     1,

	"internal/tools/tools_marketplace.go\tRegistry.handleMarketplaceList\tmessage\tengine unavailable": 1,

	"internal/tools/tools_pg.go\tRegistry.planPgCreate\tmessage\tinstances %d: specification %s (%s) takes 1-%d nodes": 1,

	"internal/tools/tools_stack.go\tRegistry.callTool\tmessage\t%s: unparseable result: %s": 1,
	"internal/tools/tools_stack.go\tRegistry.callTool\tmessage\tunknown tool %q":            1,
	"internal/tools/tools_stack.go\tRegistry.takenByName\tmessage\t%s answered without %q":  1,

	"internal/tools/tools_vm_diagnose.go\t\terror\tproblem": 5,

	"internal/tools/userkeys.go\tdescribeUserKeys\terror\t; not sent to the VM":                             1,
	"internal/tools/userkeys.go\tdescribeUserKeys\terror\tnot an authorized_keys entry; not sent to the VM": 1,

	"internal/vmstage/vmstage.go\tcheckName\tmessage\tstage input of %s: no quotes or line breaks":              1,
	"internal/vmstage/vmstage.go\tcheckName\tmessage\tstage name %q: lower-case letters, digits and dashes":     1,
	"internal/vmstage/vmstage.go\tparse\tmessage\tthe stage's status on the VM is not read: %q":                 1,
	"internal/vmstage/vmstage.go\tsshRunner.Start\tmessage\tstart the stage %s on the VM: unexpected answer %q": 1,

	"internal/vpc/client.go\tapiError\tcloud status\tvpc API %d: %s": 2,
}
