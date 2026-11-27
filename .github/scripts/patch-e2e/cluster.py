"""One fixed-size Managed Kubernetes cluster and pool. The only writes address direct receipts."""
import os
import re
import time
from urllib.parse import urlsplit

import common
import cluster_objects

ZONE = 'ru.AZ-2'
CLUSTER, POOL = 'k8s-cluster', 'k8s-pool'
SPEC = {CLUSTER: ('clusters', 'clusters', 'k8s_create', 'k8s_delete', 'cluster_id'),
        POOL: ('nodePools', 'nodePools', 'k8s_add_nodepool', 'k8s_nodepool_delete', 'nodepool_id')}
UUID = re.compile(r'[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}')


def require(condition, why):
    if not condition:
        raise RuntimeError('Managed Kubernetes: ' + why)


def ref(s):
    return isinstance(s, str) and bool(re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,120}', s))


def project():
    p = os.environ['CLOUDRU_PROJECT_ID']
    require(ref(p), 'project unavailable')
    return p


def runname():
    run = os.environ['P90_SOURCE_RUN']
    require(bool(re.fullmatch(r'[1-9][0-9]{0,19}', run)), 'invalid run identity')
    return 'evo-p90-' + run


def read(path, **query):
    return common.mk8s_get('/v3/' + path, {'project_id': project(), **query})


def tool(name, /, mode=None, **args):
    argv = ['--project-id', project()]
    if mode:
        argv.append(mode)
    argv.extend(['tools', 'call', name, '--json'])
    for key, value in args.items():
        argv.extend(['--arg', key + '=' + (str(value).lower() if isinstance(value, bool) else str(value))])
    return common.call_evo(*argv, timeout=75)


def inventory(kind, parent=None):
    endpoint, field = SPEC[kind][:2]
    q = {'clusterId': parent} if kind == POOL else {}
    tokens, ids, rows = set(), set(), []
    for _ in range(100):
        status, page = read(endpoint, **q)
        require(status == 200 and isinstance(page, dict), 'inventory unavailable')
        batch, more = page.get(field, []), page.get('nextPageToken', '')
        require(isinstance(batch, list) and isinstance(more, str), 'incomplete inventory')
        for row in batch:
            require(isinstance(row, dict) and ref(row.get('id')) and ref(row.get('name'))
                    and row['id'] not in ids, 'incomplete inventory identity')
            if kind == POOL:
                require(row.get('clusterId', parent) == parent, 'pool listed under different cluster')
            else:
                require(row.get('projectId', project()) == project(), 'cluster listed in different project')
            rows.append(row)
            ids.add(row['id'])
        if not more:
            return rows
        require(more not in tokens, 'repeated continuation')
        tokens.add(more)
        q['pageToken'] = more
    raise RuntimeError('Managed Kubernetes: incomplete inventory')


def choose(catalog, zone, purpose):
    eligible = []
    for f in catalog['flavors']:
        if (f.get('cpu'), f.get('ram'), f.get('gpu', 0)) != (2, 4, 0):
            continue
        if f.get('purpose', '').split('_')[-1] != purpose:
            continue
        zones = [az for az in f.get('availabilityZones', []) if az.get('zoneId') == zone['zoneId']
                 or az.get('zoneName') == ZONE]
        if len(zones) != 1 or zones[0].get('flavorZoneState') != 'FLAVOR_ZONE_STATE_AVAILABLE':
            continue
        if ref(f.get('flavorId')) and ref(f.get('name')):
            # An explicit name is how the candidate resolves this exact catalog ID.
            if sum(x.get('name') == f['name'] for x in catalog['flavors']) == 1:
                eligible.append(f)
    require(bool(eligible), 'approved and available 2-vCPU/4-GiB GPU-free SKU missing')
    return sorted(eligible, key=lambda f: f['name'])[0]


def preview(kind, args, expected):
    name = SPEC[kind][2]
    out = tool(name, '--dry-run', **args)
    require(out.get('status') == 'dry_run' and out.get('preview', {}).get('action') == name, 'creation preview unavailable')
    data = out['preview']['details'].get('preview') or {}
    params = {p['key']: p['value'] for p in data.get('params', [])}
    resource = 'k8s_cluster' if kind == CLUSTER else 'k8s_nodepool'
    require(data.get('project_id') == project() and data.get('resource') == resource
            and data.get('zone', {}).get('name') == ZONE, 'project or zone changed')
    require(all(str(params.get(k)).lower() == str(v).lower() for k, v in expected.items()), 'preview size changed')
    return params


def preflight():
    require(all(c['name'] != runname() for c in inventory(CLUSTER)), 'cluster name already in use')
    status, catalog = read('productConfiguration')
    require(status == 200 and isinstance(catalog, dict) and isinstance(catalog.get('flavors'), list), 'catalog unavailable')
    zones = [z for z in catalog.get('zones', []) if z.get('name') == ZONE and ref(z.get('zoneId'))]
    require(len(zones) == 1, 'availability zone missing or ambiguous')
    disk_type = catalog.get('defaultDiskType', '')
    require(isinstance(disk_type, str) and re.sub(r'[^A-Z0-9]', '', disk_type.upper())
            in {'SSD', 'SSDDEFAULT', 'SSDNVME', 'DISKTYPESSDNVME'}, 'unapproved default disk type')
    master, worker = (choose(catalog, zones[0], purpose) for purpose in ('CONTROLPLANE', 'DATAPLANE'))
    args = dict(name=runname(), zone=ZONE, flavor=master['name'], master_count=1, kube_api_internet=True)
    plan = tool('k8s_plan', **args)
    require(plan.get('zone') == ZONE and plan.get('master_count') == 1 and plan.get('flavor_id') == master['flavorId']
            and plan.get('kube_api_internet') is True and ref(plan.get('subnet_id'))
            and ref(plan.get('cluster_sa_id')) and ref(plan.get('version')), 'resolved master differs from selected configuration')
    args.update(subnet_id=plan['subnet_id'], cluster_sa_id=plan['cluster_sa_id'], version=plan['version'])
    preview(CLUSTER, args, dict(cpu=2, ram_gb=4, flavor=master['name'], master_count=1, kube_api_internet=True))
    pool = dict(name=runname()+'-pool', zone=ZONE, flavor=worker['name'], disk_gb=20, count=1)
    return dict(cluster_args=args, pool_args=pool, worker_id=worker['flavorId'], master_id=master['flavorId'])


def poll(seconds, predicate):
    until = time.monotonic() + seconds
    while True:
        result = predicate()
        if result:
            return result
        remaining = until - time.monotonic()
        require(remaining > 0, 'deadline expired with outcome unconfirmed')
        time.sleep(min(15, remaining))


def persist(kind, **fields):
    common.note(kind, kind, **fields)


def resolve(kind, record):
    require(record.get('project') == project() and record.get('name') == (runname() if kind == CLUSTER else runname()+'-pool'),
            'receipt does not match this run or project')
    rid, op = record.get('id'), record.get('operation')
    require(ref(op), 'create response lost; resource identity unknown')
    if rid:
        require(ref(rid), 'invalid receipt ID')
        return rid
    status, body = read('operations/' + op)
    require(status == 200 and isinstance(body, dict) and body.get('id') == op, 'create operation unavailable')
    rid = body.get('resourceId')
    if not rid and not body.get('done'):
        return None
    require(ref(rid), 'create operation returned no resource identity')
    persist(kind, id=rid)
    return rid


def create(kind, args, parent=None):
    _, _, command, _, id_key = SPEC[kind]
    require(kind not in common.ledger(), 'existing intent prevents repeated creation')
    persist(kind, project=project(), name=args['name'], parent=parent, state='create-intent')
    out = tool(command, '--yes', **args)
    require(out.get('status') == 'creating' and ref(out.get('operation_id')), 'no direct create receipt; no adoption by name')
    rid = out.get(id_key)
    require(rid in (None, '') or ref(rid), 'invalid create response ID')
    persist(kind, operation=out['operation_id'], id=rid, state='created')
    return poll(180, lambda: resolve(kind, common.ledger()[kind]))


def absent(kind, rid, status, card):
    if status != 404 or not isinstance(card, dict) or card.get('code') not in (5, '5', 'NOT_FOUND'):
        return False
    msg = card.get('message')
    resource = 'cluster' if kind == CLUSTER else r'node[ _-]?pool'
    return isinstance(msg, str) and bool(re.fullmatch(resource + r"(?: (?:with id )?['\"]?" +
                         re.escape(rid) + r"['\"]?)? (?:not found|does not exist)\.?", msg.strip(), flags=re.I))


def inspect(kind, rid, parent):
    status, card = read(SPEC[kind][0] + '/' + rid)
    if absent(kind, rid, status, card):
        return None
    require(status == 200 and isinstance(card, dict) and card.get('id') == rid, 'resource GET unconfirmed')
    if kind == POOL:
        require(card.get('clusterId') == parent, 'pool parent changed')
    else:
        require(card.get('projectId', project()) == project(), 'cluster project changed')
    require(card.get('name') == common.ledger()[kind]['name'], 'resource differs from creation receipt')
    if not common.ledger()[kind].get('seen'):
        persist(kind, seen=True)
    return card


def ready(kind, rid, parent, seconds):
    def state():
        card = inspect(kind, rid, parent)
        if not card:
            return False
        st = card.get('status', '')
        require(isinstance(st, str) and not any(word in st.upper() for word in ('FAIL','ERROR','DELET')), 'resource provisioning failed')
        return card if st in ('RUNNING', 'OBJECT_STATUS_RUNNING') else False
    return poll(seconds, state)


def run():
    config = preflight()
    common.note('k8s-config', 'k8s-config', zone=ZONE, master=config['master_id'], worker=config['worker_id'],
                worker_disk_gib=20, api_public=True,
                opaque_master_disk='not selectable', project_quotas='not exposed in productConfiguration')
    cid = create(CLUSTER, config['cluster_args'])
    ready(CLUSTER, cid, None, 40*60)
    require(not inventory(POOL, cid), 'unexpected pools in new cluster')
    pool = dict(config['pool_args'], cluster_id=cid)
    params = preview(POOL, pool, dict(cpu=2, ram_gb=4, flavor=pool['flavor'], count=1, disk_gb=20, cluster_id=cid))
    require(ref(params.get('nodes_subnet_id')) and ref(params.get('security_group_id')), 'unresolved project network')
    pool.update(nodes_subnet_id=params['nodes_subnet_id'], security_group_id=params['security_group_id'])
    pid = create(POOL, pool, cid)
    ready(POOL, pid, cid, 30*60)
    nodes = cluster_objects.observe_nodes(cid)
    capture_backends(nodes)
    require(any(n.get('ready') is True for n in nodes), 'worker did not report Ready')
    cluster_objects.exercise(cid)


def capture_backends(nodes):
    # Node UIDs and providerIDs are supplementary links, never selectors for a DELETE.
    links = []
    for node in nodes:
        uid, provider = node['uid'], node.get('provider_id', '')
        link = {'uid': uid, 'provider_id': provider, 'backends': []}
        links.append(link)
        common.note('k8s-observations', 'k8s-observations', nodes=links, opaque='unknown')
        if not isinstance(provider, str) or not provider:
            continue
        vm = urlsplit(provider).path.rstrip('/').split('/')[-1]
        if not UUID.fullmatch(vm):
            continue
        link['backends'] = [('vms', vm)]
        common.note('k8s-observations', 'k8s-observations', nodes=links, opaque='unknown')
        status, card = common.compute('GET', '/api/v1/vms/' + vm)
        if status != 200 or not isinstance(card, dict) or card.get('id') != vm:
            continue
        # Attached disk/IP IDs prove a link to this VM, not independent ownership.
        # We only read them again; a surviving attachment may be shared.
        refs = {('vms', vm)}
        for item in card.get('disks') or []:
            if isinstance(item, dict):
                disk = item.get('id') or item.get('disk_id')
                if isinstance(disk, str) and UUID.fullmatch(disk):
                    refs.add(('disks', disk))
        for nic in card.get('interfaces') or []:
            if isinstance(nic, dict) and isinstance(nic.get('floating_ip'), dict):
                ip = nic['floating_ip'].get('id')
                if isinstance(ip, str) and UUID.fullmatch(ip):
                    refs.add(('floating-ips', ip))
        link['backends'] = sorted(refs)
        common.note('k8s-observations', 'k8s-observations', nodes=links, opaque='unknown')


def recheck_backends():
    observation = common.ledger().get('k8s-observations', {})
    results = []
    for node in observation.get('nodes', []):
        for resource, rid in node.get('backends', []):
            require(resource in ('vms','disks','floating-ips') and UUID.fullmatch(rid), 'invalid backend receipt')
            code, card = common.compute('GET', '/api/v1/'+resource+'/'+rid)
            if code == 200 and isinstance(card, dict) and card.get('id') == rid:
                outcome = 'present'
            else:
                names = {'vms': ('vm','virtual machine'), 'disks': ('disk',), 'floating-ips':('floating ip','floating_ip')}
                errors = card if isinstance(card, list) else [card]
                accepted = {name + ' not found' for name in names[resource]}
                accepted.update(name + ' ' + rid.lower() + ' not found' for name in names[resource])
                missing = (code == 404 and bool(errors) and all(isinstance(e, dict)
                           and str(e.get('message', '')).strip().lower() in accepted for e in errors))
                outcome = 'absent' if missing else 'unknown'
            results.append({'kind':resource, 'id':rid, 'outcome':outcome})
    common.note('k8s-observations', 'k8s-observations', results=results, opaque='unknown')
    print('Managed Kubernetes: service-internal resources without direct ID linkage — unknown.', flush=True)
    require(all(r['outcome']=='absent' for r in results), 'linked backend still present or unreadable; review only')


def delete(kind, rid, parent, seconds):
    record = common.ledger()[kind]
    require(record.get('parent') == parent, 'receipt parent differs')
    require(resolve(kind, record) == rid, 'resource ID differs')
    def removed():
        card = inspect(kind, rid, parent)
        if card is not None:
            return False
        # A 404 immediately after POST, before the service has materialized the object, is not deletion.
        if not common.ledger()[kind].get('seen'):
            status, op = read('operations/' + record['operation'])
            require(status == 200 and isinstance(op, dict) and op.get('id') == record['operation']
                    and op.get('resourceId') == rid and op.get('done') is True,
                    'not seen yet and accepted creation has not finished')
        return all(row['id'] != rid for row in inventory(kind, parent))
    if not removed():
        if record.get('state') != 'delete-intent':
            # Only issue one DELETE; recovery can prove an earlier accepted request.
            # A lost acknowledgement is not permission to redispatch it.
            require(inspect(kind, rid, parent) is not None, 'uncertain state before deletion')
            if kind == CLUSTER:
                require(not inventory(POOL, rid), 'remaining pool blocks cluster cascade')
            persist(kind, state='delete-intent')
            result = tool(SPEC[kind][3], '--yes', **{SPEC[kind][4]:rid, 'wait':False})
            if result.get('operation_id'):
                persist(kind, delete_operation=result['operation_id'])
        poll(seconds, removed)
    persist(kind, state='gone')


def cleanup():
    records = common.ledger()
    if CLUSTER not in records:
        require(POOL not in records, 'missing parent receipt')
        return
    cid = poll(120, lambda: resolve(CLUSTER, records[CLUSTER]))
    pool = records.get(POOL)
    if records[CLUSTER].get('state') != 'gone':
        if pool and pool.get('state') != 'gone':
            require(pool.get('parent') == cid, 'pool belongs to a different cluster')
            pid = poll(120, lambda: resolve(POOL, pool))
            # Even a cold runner uses existing API identities, never a project-wide name match.
            cluster_objects.cleanup(cid)
            delete(POOL, pid, cid, 30*60)
        elif not pool:
            cluster_objects.cleanup(cid)
        delete(CLUSTER, cid, None, 30*60)
    recheck_backends()
