"""Replay the accepted MK8s configuration and direct receipts, including a cold cleanup."""
import copy
import os
import unittest
from unittest.mock import patch

import cluster as k


class ClusterTest(unittest.TestCase):
    def setUp(self):
        env = patch.dict(os.environ, {'CLOUDRU_PROJECT_ID':'fixture-project', 'P90_SOURCE_RUN':'19'})
        env.start()
        self.addCleanup(env.stop)
        self.records, self.writes, self.cards, self.ops = {}, [], {}, {}
        self.neighbor, self.gateway = False, False
        self.without_response, self.allocate_later = None, False
        az = {'zoneId':'zone', 'zoneName':k.ZONE,'flavorZoneState':'FLAVOR_ZONE_STATE_AVAILABLE'}
        self.catalog = {'defaultDiskType':'SSD NVMe','zones':[{'zoneId':'zone','name':k.ZONE}],
            'flavors':[{'name':purpose.lower(), 'cpu':2, 'ram':4, 'flavorId':purpose, 'purpose':'FLAVOR_PURPOSE_'+purpose,
                        'availabilityZones':[copy.deepcopy(az)]} for purpose in ('CONTROLPLANE','DATAPLANE')]}
        self.extra_page, self.nopool = False, False
        self.compute_gets = []
        self.vendor_vm = '33333333-1111-4111-8111-111111111111'
        self.vendor_disk = '44444444-1111-4111-8111-111111111111'
        def note(kind, key, **fields):
            self.records.setdefault(key, {'kind':kind}).update(copy.deepcopy(fields))
        patches = [(k.common, 'ledger', lambda: copy.deepcopy(self.records)),
                   (k.common, 'note', note), (k, 'tool', self.evo), (k, 'read', self.get),
                   (k.common, 'compute',self.compute),
                   (k.cluster_objects, 'cleanup', lambda cid:self.assertEqual(cid,'own-cluster')),
                   (k.cluster_objects, 'exercise', lambda cid:self.assertEqual(cid,'own-cluster')),
                   (k.cluster_objects, 'observe_nodes', lambda cid:[{'uid':'node-k8s-uid','provider_id':'openstack:///'+self.vendor_vm, 'ready':True}])]
        for obj, name, func in patches:
            mock=patch.object(obj,name,side_effect=func);mock.start();self.addCleanup(mock.stop)

    def compute(self, method, path):
        self.assertEqual(method, 'GET')
        self.compute_gets.append(path)
        if path.endswith(self.vendor_vm):
            return ((200, {'id':self.vendor_vm, 'disks':[{'id':self.vendor_disk}]}) if 'own-cluster' in self.cards
                    else (404, [{'message':'virtual machine not found'}]))
        self.assertTrue(path.endswith(self.vendor_disk))
        return 404, [{'message':'disk not found'}]

    def get(self, path, **query):
        if path=='productConfiguration':
            return 200, self.catalog
        if path.startswith('operations/'):
            op=path.split('/')[1]
            return 200, self.ops[op]
        if path in ('clusters','nodePools'):
            rows=[v for v in self.cards.values() if ('clusterId' in v)==(path=='nodePools')]
            if path=='nodePools':
                self.assertEqual(query.get('clusterId'), 'own-cluster')
                if self.neighbor:
                    if self.extra_page and 'pageToken' not in query:
                        return 200, {'nodePools':rows, 'nextPageToken':'next'}
                    rows.append({'id':'someone-else','clusterId':'own-cluster','name':'stranger'})
                    if self.extra_page:
                        rows=rows[-1:]
            return 200, {path:rows}
        kind, rid = path.split('/')
        if rid in self.cards:
            return 200,self.cards[rid]
        msg='cluster' if kind=='clusters' else 'node pool'
        if self.gateway:
            msg += ' route'
        return 404, {'code':5,'message':msg+' not found'}

    def evo(self, name, /, mode=None, **args):
        if name=='k8s_plan':
            return dict(args, flavor_id='CONTROLPLANE', subnet_id='sub', cluster_sa_id='sa', version='1.31')
        if mode=='--dry-run':
            resource='k8s_cluster' if name=='k8s_create' else 'k8s_nodepool'
            vals=dict(args, cpu=2,ram_gb=4,nodes_subnet_id='ns',security_group_id='sg')
            prev={'resource':resource, 'project_id':'fixture-project','zone':{'name':k.ZONE},
                  'price_classes':[{'class':'paid_unpriced','resource':resource}],
                  'params':[{'key':key,'value':val} for key,val in vals.items()]}
            return {'status':'dry_run','preview':{'action':name,'details':{'preview':prev}}}
        self.assertEqual(mode, '--yes')
        self.writes.append((name, args))
        ispool='nodepool' in name
        rid='own-pool' if ispool else 'own-cluster'
        idkey='nodepool_id' if ispool else 'cluster_id'
        if 'delete' in name:
            self.assertEqual(args[idkey],rid)
            del self.cards[rid]
            return {'operation_id':'deletion'}
        if name==self.without_response:
            raise RuntimeError('lost acknowledgement')
        op='op-'+rid
        self.ops[op]={'id':op,'resourceId':rid,'done':True}
        self.cards[rid]={'id':rid,'name':args['name'],'status':'OBJECT_STATUS_RUNNING','projectId':'fixture-project'}
        if ispool:self.cards[rid]['clusterId']='own-cluster'
        return {'operation_id':op,'status':'creating',idkey:None if self.allocate_later else rid}

    def test_all_resources_use_their_create_receipt_and_exact_readback(self):
        self.allocate_later=True
        self.cards['foreign-cluster']={'id':'foreign-cluster','name':'similar','projectId':'fixture-project'}
        k.run();k.cleanup();k.cleanup()
        self.assertEqual([n for n,_ in self.writes],['k8s_create','k8s_add_nodepool','k8s_nodepool_delete','k8s_delete'])
        self.assertEqual(set(self.cards),{'foreign-cluster'})
        self.assertEqual(self.records[k.POOL]['state'],'gone');self.assertEqual(self.records[k.CLUSTER]['state'],'gone')
        refs=self.records['k8s-observations']
        self.assertEqual(refs['opaque'],'unknown')
        self.assertEqual({(x['kind'],x['id'],x['outcome']) for x in refs['results']},
                         {('vms',self.vendor_vm,'absent'),('disks',self.vendor_disk,'absent')})

    def test_cold_cleanup_leaves_foreign_pool_and_cluster_on_page_two(self):
        k.run();self.neighbor=self.extra_page=True
        with self.assertRaisesRegex(RuntimeError,'remaining pool'): k.cleanup()
        self.assertIn('own-cluster',self.cards)
        self.assertEqual([a.get('nodepool_id') for n,a in self.writes if n=='k8s_nodepool_delete'],['own-pool'])
        self.assertNotIn('k8s_delete',[n for n,_ in self.writes])

    def test_missing_or_mismatched_receipt_never_selects_by_name(self):
        self.without_response='k8s_create'
        with self.assertRaisesRegex(RuntimeError,'lost acknowledgement'): k.run()
        self.cards['foreign']={'name':k.runname(),'id':'foreign'}
        with self.assertRaisesRegex(RuntimeError,'identity unknown'): k.cleanup()
        self.assertEqual([n for n,_ in self.writes],['k8s_create'])

    def test_repeat_after_gateway_404_cannot_repeat_delete_or_infer_absence(self):
        k.run();self.gateway=True
        with self.assertRaisesRegex(RuntimeError,'unconfirmed'):k.cleanup()
        with self.assertRaisesRegex(RuntimeError,'unconfirmed'):k.cleanup()
        self.assertEqual(self.records[k.POOL]['state'],'delete-intent')
        self.assertEqual([n for n,_ in self.writes].count('k8s_nodepool_delete'),1)
        self.assertNotIn('k8s_delete',[n for n,_ in self.writes])
        self.gateway=False;k.cleanup()
        self.assertEqual(self.records[k.CLUSTER]['state'],'gone')

    def test_catalog_availability_exact_purpose_disk_and_size_before_post(self):
        for attr,value in [('cpu',4),('ram',3),('gpu',1),('purpose','DATAPLANE'),('availabilityZones',[])]:
            with self.subTest(attr=attr), patch.dict(self.catalog['flavors'][0],{attr:value}), self.assertRaises(RuntimeError):
                k.run()
        self.catalog['flavors'][1]['availabilityZones'][0]['flavorZoneState']='FLAVOR_ZONE_STATE_SOLD_OUT'
        with self.assertRaises(RuntimeError):k.run()
        self.catalog['defaultDiskType']='premium-unapproved'
        with self.assertRaisesRegex(RuntimeError,'disk type'):k.run()
        self.assertFalse(self.writes)

    def test_unknown_network_readback_and_wrong_parent_block_mutations(self):
        k.run();self.cards['own-pool']['clusterId']='different'
        with self.assertRaisesRegex(RuntimeError,'parent changed'):k.cleanup()
        self.assertFalse(any('delete' in n for n,_ in self.writes))

    def test_unobservable_backend_associations_report_unknown_without_project_search(self):
        k.capture_backends([{'uid':'a','provider_id':'opaque'}]); k.recheck_backends()
        self.assertFalse(self.compute_gets)
        self.assertEqual(self.records['k8s-observations']['opaque'],'unknown')
