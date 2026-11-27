"""Private-DNS proofs use synthetic API state; no external calls."""
import os
import subprocess
import unittest
from unittest.mock import patch

import private_dns as d


def uid(i):
    return f"aaaaaaaa-aaaa-4aaa-8aaa-{i:012}"


class PrivateDNS(unittest.TestCase):
    def setUp(self):
        self.zid, self.rid, self.vpc, self.pid = map(uid, range(1, 5))
        self.records = {}
        self.cards = {}
        self.ops = {}
        self.writes = []
        self.window_ok = True
        self.n = 10
        p = patch.dict(os.environ, CLOUDRU_PROJECT_ID=self.pid, P90_SOURCE_RUN="17", P90_NET_BIN=__file__)
        p.start(); self.addCleanup(p.stop)
        for host, fn, value in ((d.common,"ledger",lambda: self.records), (d.common,"note",self.note),
                                (d.common,"service",self.api), (d.isolation,"dns_window",self.window),
                                (d,"fence",self.fence), (d,"tool",self.tool),
                                (d.time,"sleep",lambda _: self.fail("unexpected poll"))):
            p = patch.object(host, fn, value); p.start(); self.addCleanup(p.stop)
        self.note("network-vpc","network.vpc",project=self.pid, id=self.vpc, returned_directly=True)

    def note(self, kind, key, **fields):
        self.records.setdefault(key,dict(kind=kind)).update(fields)

    def window(self, start, end):
        if not self.window_ok:
            raise RuntimeError("no original signed grant")
        self.assertLess(start,end)

    def newop(self, rid):
        self.n += 1
        op = uid(self.n)
        self.ops[op] = dict(id=op,done=True,resourceId=rid)
        return op

    def fence(self, key, tool, args, method, path, body=None):
        d.window()
        self.writes.append((method,path))
        rid = self.zid if key == d.ZONE else self.rid
        if method == "POST":
            self.assertTrue(d.row(key)["intent"])
            if key == d.ZONE:
                self.assertEqual(args["vpc_id"],self.vpc)
                self.assertEqual(body["linkedVpcIds"],[self.vpc])
            else:
                self.assertEqual(body["type"],"a")
            self.cards[rid] = dict(body,id=rid)
            self.note("guard","network."+key,operation=self.newop(rid),returned_directly=True,create_dispatched=True)
        else:
            self.assertTrue(d.row(key)["delete_intent"])
            del self.cards[rid]
            self.note("guard","network."+key,delete_operation=self.newop(rid),delete_dispatched=True)

    def tool(self, name, args, *, negative_case=False, dry=False):
        if negative_case:
            self.assertIn(name,["dns_zone_delete","dns_zone_create"])
            return {"reason":"USAGE","error":"VPC " + self.vpc + (" linked" if name.endswith("delete") else " has no DNS server")}
        if dry:
            return {"status":"dry_run","preview":{"action":"dns_zone_create","details":{"preview":{
                "project_id":self.pid,"params":[{"key":"vpc_id","value":self.vpc},{"key":"visibility","value":"private"}]}}}}
        return {"id":args.get("id",args.get("record_id"))}

    def api(self, method, host, path, query=None, body=None):
        self.assertEqual(host,"dns")
        if path.startswith("/v1/operations/"):
            return 200,self.ops[path.split("/")[-1]]
        key = d.ZONE if "Zones" in path else d.RECORD
        if path == d.COLLECTION[key]:
            self.assertEqual(query,{"project_id":self.pid} if key==d.ZONE else {"private_zone_id":self.zid})
            return 200, {("zones" if key==d.ZONE else "records"):
                         [c for c in self.cards.values() if ("domain" in c)==(key==d.ZONE)]}
        rid = path.split("/")[-1]
        if method == "PATCH":
            self.assertEqual(path,d.COLLECTION[d.ZONE]+"/"+self.zid)
            self.assertEqual(body,{"linkedVpcIds":[]})
            self.assertEqual(self.cards[self.zid]["linkedVpcIds"],[self.vpc])
            self.writes.append((method,path))
            self.cards[self.zid]["linkedVpcIds"] = []
            return 202,{"id":self.newop(self.zid)}
        if rid in self.cards:
            return 200,self.cards[rid]
        return 404,{"code":5,"message":("Zone" if key==d.ZONE else "Record")+" not found"}

    def test_product_d65_d66_and_exact_unlink(self):
        d.negative(self.vpc)
        d.run(); d.cleanup()
        self.assertEqual(self.writes,[("POST",d.COLLECTION[d.ZONE]),("POST",d.COLLECTION[d.RECORD]),
            ("DELETE",d.COLLECTION[d.RECORD]+"/"+self.rid),("PATCH",d.COLLECTION[d.ZONE]+"/"+self.zid),
            ("DELETE",d.COLLECTION[d.ZONE]+"/"+self.zid)])
        self.assertTrue(d.row("dns-negative")["refused"] and d.row(d.ZONE)["d66_refused"])
        self.assertTrue(d.row(d.ZONE)["gone"] and d.row(d.RECORD)["gone"])

    def test_failed_or_expired_grant_stops_before_create_and_unlink(self):
        self.window_ok = False
        with self.assertRaisesRegex(RuntimeError,"grant"):
            d.run()
        self.assertFalse(self.writes)
        self.window_ok = True
        d.create(d.ZONE,self.vpc)
        self.window_ok = False
        with self.assertRaisesRegex(RuntimeError,"grant"):
            d.cleanup()
        self.assertEqual(len(self.writes),1)

    def test_foreign_link_or_foreign_record_blocks_even_under_grant(self):
        d.create(d.ZONE,self.vpc)
        self.cards[self.zid]["linkedVpcIds"].append(uid(67))
        with self.assertRaisesRegex(RuntimeError,"ambiguous"):
            d.cleanup()
        self.cards[self.zid]["linkedVpcIds"]=[self.vpc]
        self.cards[uid(8)]=dict(id=uid(8),type="A",name="other",privateZoneId=self.zid)
        with self.assertRaisesRegex(RuntimeError,"unexpected record"):
            d.cleanup()
        self.assertEqual(self.writes,[("POST",d.COLLECTION[d.ZONE])])

    def test_lost_create_and_failed_operation_cannot_adopt_same_name(self):
        d.create(d.ZONE,self.vpc)
        r=d.row(d.ZONE); r.pop("id"); r.pop("operation")
        with self.assertRaisesRegex(RuntimeError,"identity"):
            d.cleanup()
        r["operation"]=self.newop(self.zid); self.ops[r["operation"]]["error"]={"code":7}
        with self.assertRaisesRegex(RuntimeError,"failed"):
            d.cleanup()
        self.assertEqual(len(self.writes),1)

    def test_gateway_404_and_lost_patch_cannot_complete(self):
        d.create(d.ZONE,self.vpc)
        d.mark(d.ZONE,unlink_intent=True)
        with self.assertRaisesRegex(RuntimeError,"still present"):
            d.cleanup()
        with patch.object(d.common,"service",return_value=(404,{"code":5,"message":"zone route not found"})):
            with self.assertRaisesRegex(RuntimeError,"not established"):
                d.card(d.ZONE,self.zid,allow_gone=True)
        self.assertEqual(len(self.writes),1)

    def test_negative_case_refuses_foreign_vpc(self):
        with self.assertRaisesRegex(RuntimeError,"own VPC"):
            d.negative(uid(42))
        self.assertFalse(self.writes)


class CandidateEnvelope(unittest.TestCase):
    def test_real_cli_error_envelope_has_numeric_code_and_named_reason(self):
        result = subprocess.CompletedProcess([],1,b"",b'trace\n{"error":{"code":1,"reason":"USAGE","message":"no DNS server"}}\n')
        with patch.dict(os.environ,EVO_BIN="/unused",CLOUDRU_PROJECT_ID=uid(4)), patch.object(d.subprocess,"run",return_value=result):
            self.assertEqual(d.tool("dns_zone_create",{},negative_case=True),{"reason":"USAGE","error":"no DNS server"})
