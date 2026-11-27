"""Offline PostgreSQL checks. No cloud clients or credentials are used."""
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import pg


class PGTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        ctx = patch.dict(os.environ, {'P90_SOURCE_RUN': '9001', 'CLOUDRU_PROJECT_ID': 'test-project', 'RUNNER_TEMP': tmp.name})
        ctx.start()
        self.addCleanup(ctx.stop)
        self.log = []
        self.state = {}
        self.parent = False
        self.users = {'dbadmin'}
        self.databases = {'defaultdb'}
        self.copies = {'auto-one': 'first'}
        self.backup_http = 400
        self.after_cluster = (404, {'code': 5, 'message': 'cluster not found'})
        self.rate = 2.4650
        self.preview_cpu = 1
        self.public_ip = False
        self.lose = False
        self.omit_cluster_id = False
        for name, value in [('call_evo', self.cli), ('pg_get', self.get), ('ledger', lambda: {k: dict(v) for k, v in self.state.items()}), ('note', self.note)]:
            ctx = patch.object(pg.common, name, value)
            ctx.start()
            self.addCleanup(ctx.stop)

    def note(self, kind, key, **fields):
        record = self.state.setdefault(key, {'kind': kind})
        self.assertEqual(record['kind'], kind)
        record.update(fields)

    def cli(self, *args, timeout):
        name = args[args.index('call') + 1]
        values = dict(v.split('=', 1) for k, v in zip(args, args[1:]) if k == '--arg')
        self.log.append((name, '--yes' in args, values.copy()))
        if name == 'pg_list':
            result = [{'id': 'neighbor', 'name': 'unrelated', 'status': 'READY'}]
            if self.parent:
                result += [{'id': 'ours', 'name': pg.marker(), 'status': 'READY'}]
            return {'clusters': result}
        if name == 'list_zones':
            return {'zones': [{'id':'az','name':pg.ZONE,'enabled':True}]}
        if name == 'list_subnets':
            return {'subnets':[{'id':'sn','name':'existing','availability_zone_id':'az'}]}
        if name == 'pg_specs':
            return {'specifications':[{'id':'spec', 'deployment_mode':'STANDARD','cpu':1,'memory_gb':2,'min_storage_gb':10}]}
        if name == 'pg_create':
            if '--dry-run' in args:
                will = dict(values, instances=1, storage_gb=10, cpu=self.preview_cpu, memory_gb=2, deployment_mode='STANDARD', zone=pg.ZONE, backup_enabled=True)
                price = {'currency':'RUB','total_hourly_rub':self.rate,'source':'tariff version 260918'}
                preview = {'resource':'pg_cluster','project_id':'test-project','zone':{'name':pg.ZONE}, 'params':[{'key':'access','value':'evo sets up none'}], 'public_ip':self.public_ip, 'cost':price}
                return {'status':'dry_run','preview':{'action':name,'details':{'will_create':will,'preview':preview}}}
            self.assertIn('--yes', args)
            self.parent = True
            if self.lose:
                raise RuntimeError('simulated lost response')
            if self.omit_cluster_id:
                return {'status':'creating','operation_id':'creation'}
            return {'status':'creating','operation_id':'creation','cluster_id':'ours'}
        if name == 'pg_operation':
            op = values['operation_id']
            return {'id':op,'done':True,'resource_id': {'creation':'ours','backup-op':'manual-one'}[op]}
        if name == 'pg_delete':
            self.assertEqual(values['cluster_id'], 'ours')
            self.parent = False
            return {'status':'deleting','operation_id':'delete-op'}
        if '--args-file' in args:
            secret = Path(args[args.index('--args-file')+1]);self.assertEqual(secret.stat().st_mode & 0o777, 0o600)
            values = json.loads(secret.read_text())
            self.assertTrue(values['password']); self.assertFalse(any(values['password'] in arg for arg in args))
        self.assertEqual(values['cluster_id'], 'ours')
        if name in ('pg_users', 'pg_databases'):
            field = name[3:]
            return {field:[{'name':n} for n in getattr(self,field)]}
        if name == 'pg_backups':
            if not self.parent:
                raise RuntimeError('parent unavailable')
            return {'backups':[{'id':bid,'name':n,'cluster_id':'ours'} for bid,n in self.copies.items()]}
        if name == 'pg_user_create':
            self.users.add(values['name']); return {'status':'created','user':{'name':values['name']}}
        if name == 'pg_db_create':
            self.databases.add(values['name']); return {'status':'created','database':{'name':values['name']}}
        if name == 'pg_backup_create':
            self.copies['manual-one']=values['name']; return {'status':'backing_up','operation_id':'backup-op','backup_id':'manual-one'}
        if name == 'pg_db_delete':
            self.databases.remove(values['name']); return {'status':'deleted'}
        if name == 'pg_user_delete':
            self.users.remove(values['name']); return {'status':'deleted'}
        self.fail('unexpected CLI call')

    def get(self, path):
        self.log.append(('raw',False,{'path':path}))
        if path == '/v1/clusters/ours':
            if self.parent:
                return 200, {'id':'ours','name':pg.marker(),'project_id':'test-project','status':'READY'}
            return self.after_cluster
        self.assertIn(path, ('/v1/clusters/ours/backups/first','/v1/clusters/ours/backups/p90-backup'))
        backup = 'first' if path.endswith('/first') else 'p90-backup'
        if self.backup_http == 200:
            bid = 'auto-one' if backup == 'first' else 'manual-one'
            return 200, {'id':bid,'name':backup}
        return self.backup_http, {'code':5,'message':'parent cluster not found'}

    def test_ordinary_run_and_cleanup_remember_every_backup_separately(self):
        self.omit_cluster_id = True  # association recovered exclusively from this operation
        pg.run()
        self.assertEqual(self.state['pg-user']['state'], 'absent')
        self.assertEqual(self.state['pg-db']['state'], 'absent')
        with self.assertRaisesRegex(RuntimeError,'backup cleanup unconfirmed'):
            pg.cleanup()
        copies = {v['id']:v for v in self.state.values() if v['kind']=='pg-backup'}
        self.assertEqual(set(copies), {'auto-one','manual-one'})
        self.assertEqual({v['state'] for v in copies.values()}, {'unknown'})
        self.assertEqual({v['readback_http'] for v in copies.values()}, {400})
        self.assertEqual(self.state[pg.CLUSTER]['state'], 'absent')
        self.backup_http = 404
        with self.assertRaisesRegex(RuntimeError,'backup cleanup unconfirmed'):
            pg.cleanup()
        self.assertEqual([v['readback_http'] for v in copies.values()], [404,404])
        self.assertEqual([name for name, yes, _ in self.log if yes], ['pg_create','pg_user_create','pg_db_create','pg_backup_create','pg_db_delete','pg_user_delete','pg_delete'])
        self.assertNotIn('password', json.dumps(self.state))

    def test_exact_retained_copy_and_cluster_gateway_are_distinguished(self):
        pg.run(); self.backup_http = 200
        self.after_cluster = (404, {'message':'upstream route not found'})
        with self.assertRaisesRegex(RuntimeError, 'ownership or readback unconfirmed'):
            pg.cleanup()
        self.assertNotEqual(self.state[pg.CLUSTER]['state'], 'absent')
        self.after_cluster = (404, {'code':5,'message':'cluster not found'})
        with self.assertRaisesRegex(RuntimeError, 'backup cleanup unconfirmed'):
            pg.cleanup()
        self.assertEqual({v['state'] for v in self.state.values() if v['kind']=='pg-backup'}, {'retained'})

    def test_exact_backup_error_names_each_copy_after_parent_deletion(self):
        pg.run()
        def exact_get(path):
            for name in self.copies.values():
                if path == '/v1/clusters/ours/backups/' + name:
                    return 404, {'code':5, 'message':f'backup "{name}" not found'}
            return self.get(path)
        with patch.object(pg.common, 'pg_get', side_effect=exact_get):
            pg.cleanup()
        self.assertEqual({v['state'] for v in self.state.values() if v['kind']=='pg-backup'}, {'absent'})
        copy = {'id':'own-copy','name':'test-copy'}
        info = {'@type':'type.googleapis.com/google.rpc.ResourceInfo', 'resourceType':'postgresql/Backup',
                'resourceName':'clusters/ours/backups/test-copy'}
        self.assertTrue(pg.exact_backup_missing(404, {'code':5,'message':'backup not found','details':[info]},'ours',copy))
        for code, message, details in ((404, 'backup not found', []), (400, 'backup "test-copy" not found', []),
                (404, 'backup "foreign" not found', []), (404, 'cluster not found', [info]),
                (404, 'backup "test-copy" not found', [dict(info, resourceName='clusters/foreign/backups/test-copy')])):
            self.assertFalse(pg.exact_backup_missing(code, {'code':5,'message':message,'details':details},'ours',copy))

    def test_lost_create_cannot_adopt_same_name_or_repeat(self):
        self.lose = True
        with self.assertRaisesRegex(RuntimeError,'lost response'): pg.run()
        with self.assertRaisesRegex(RuntimeError,'no receipt'): pg.cleanup()
        with self.assertRaisesRegex(RuntimeError,'already attempted'): pg.run()
        self.assertEqual([n for n,y,_ in self.log if y], ['pg_create'])

    def test_unrelated_dependants_stop_before_cluster_delete(self):
        pg.run(); self.copies['neighbor-copy']='someone-else'
        with self.assertRaisesRegex(RuntimeError,'unrelated backup'): pg.cleanup()
        del self.copies['neighbor-copy'];self.users.add('unrelated-user')
        with self.assertRaisesRegex(RuntimeError,'unrelated child'): pg.cleanup()
        self.assertNotIn('pg_delete',[n for n,_,_ in self.log])

    def test_concrete_size_and_public_ip_stop_before_create(self):
        for prop,bad,original in [('preview_cpu',2,1),('public_ip',True,False)]:
            with self.subTest(prop=prop,bad=bad):
                setattr(self,prop,bad)
                with self.assertRaises(RuntimeError): pg.run()
                setattr(self,prop,original)
        self.assertFalse(any(y for _,y,_ in self.log));self.assertFalse(self.state)
        self.rate = None
        self.assertEqual(pg.preflight()['storage_gb'],10)

    def test_incomplete_lists_do_not_become_absence(self):
        for missing in ({'clusters':[],'next_page_token':'more'}, {'clusters':None}, {}, {'clusters':[None]}):
            with self.subTest(missing=missing), self.assertRaises(RuntimeError): pg.rows(missing,'clusters')
        with patch.object(pg, 'tool',return_value={'backups':None}), patch.object(pg.common,'pg_get',return_value=(200, {'backups':[], 'next_page_token':'later'})):
            with self.assertRaisesRegex(RuntimeError,'unconfirmed'): pg.backups('ours','manual')
        with patch.object(pg, 'tool',return_value={'backups':None}), patch.object(pg.common,'pg_get',return_value=(200, {})):
            self.assertEqual(pg.backups('ours','manual'), {})
