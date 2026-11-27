"""Offline candidate CLI/MCP and Kubernetes identity checks for the run-scoped objects."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import cluster_objects as k


class ClusterObjectsTest(unittest.TestCase):
    def setUp(self):
        d = tempfile.TemporaryDirectory()
        self.addCleanup(d.cleanup)
        self.records, self.maps, self.secrets = {}, {}, {}
        self.deleted = []
        self.mcp_calls = []
        self.late_replace = False
        def note(kind, key, **fields):
            self.records[key] = {**self.records.get(key, {}), "kind": kind, **fields}
        for guard in [
            patch.dict(os.environ, {"RUNNER_TEMP": d.name, "P90_SOURCE_RUN": "41", "CLOUDRU_PROJECT_ID": "project", "EVO_BIN": "candidate"}),
            patch.object(k.common, "note", side_effect=note),
            patch.object(k.common, "ledger", side_effect=lambda: copy.deepcopy(self.records)),
            patch.object(k.common, "call_evo", side_effect=self.cli),
            patch.object(k.subprocess, "run", side_effect=self.mcp),
            patch.object(k, "exercise_addon"),
            patch.object(k, "cleanup_addon"),
        ]:
            guard.start()
            self.addCleanup(guard.stop)
        self.maps["neighbor"] = {"metadata": {"uid": "other"}}

    def kubectl(self, args):
        words = args.split()
        if words[0] == "get" and words[1] == "nodes":
            out = {"kind": "NodeList", "metadata": {}, "items": [
                {"metadata": {"uid": "node-uid"}, "spec": {"providerID": "p://service-vm"},
                 "status": {"conditions": [{"type": "Ready", "status": "True"}]}}]}
        elif words[:2] == ["get", "secrets"]:
            self.assertIn("-o=jsonpath-as-json={.items[*].metadata}", words)
            self.assertIn("-n default -l owner=helm,name=", args)
            out = list(self.secrets.values())
        elif words[:2] == ["get", "configmap"]:
            self.assertIn("--ignore-not-found", words)
            if words[2] not in self.maps:
                return ""
            out = self.maps[words[2]]
        elif words[:2] == ["create", "-f"]:
            out = json.loads(Path(words[2]).read_text())
            name = out["metadata"]["name"]
            self.assertNotIn(name, self.maps)
            out["metadata"].update(uid="apply-uid", resourceVersion="71")
            self.maps[name] = out
        elif words[:2] == ["delete", "--raw"]:
            self.assertIn("/api/v1/namespaces/default/configmaps/", words[2])
            name = words[2].split("/")[-1]
            opts = json.loads(Path(words[4]).read_text())
            if self.late_replace:
                self.maps[name]["metadata"]["uid"] = "foreign-after-get"
            if self.maps[name]["metadata"]["uid"] != opts["preconditions"]["uid"]:
                raise RuntimeError("Kubernetes UID conflict")
            self.deleted.append(opts["preconditions"]["uid"])
            del self.maps[name]
            return "{}"
        else:
            self.fail("unexpected kubectl operation")
        return "Warning: simulation\n" + json.dumps(out)

    def cli(self, *argv, timeout):
        write = "--yes" in argv
        name = argv[argv.index("call") + 1]
        args = dict(x.split("=", 1) for i, x in enumerate(argv) if i and argv[i-1] == "--arg")
        self.assertEqual("cluster-id", args.pop("cluster_id"))
        if name == "k8s_kubectl":
            self.assertEqual(write, args["args"].startswith(("create ", "delete ")))
            return {"status": "ok", "output": self.kubectl(args["args"])}
        if name == "k8s_apply":
            self.assertTrue(write)
            obj = json.loads(args["manifest"])
            name = obj["metadata"]["name"]
            self.assertEqual(self.maps[name]["metadata"], obj["metadata"])
            self.maps[name] = obj
            return {"status": "applied"}
        self.assertEqual(name, "k8s_helm")
        if args["action"] == "list":
            return {"status": "ok", "output": "NAME NAMESPACE REVISION\np90-helm-41 default 1\n"}
        self.assertTrue(write)
        self.assertEqual(args["action"], "uninstall")
        self.deleted.append(self.secrets.pop("v1")["uid"])
        return {"status": "ok"}

    def mcp(self, command, *, input, **kwargs):
        self.assertEqual(command, ["candidate", "--project-id", "project", "mcp", "--toolsets", "k8s"])
        init, notify, request = map(json.loads, input.splitlines())
        self.assertEqual([init["method"], notify["method"], request["method"]],
                         ["initialize", "notifications/initialized", "tools/call"])
        name, args = request["params"]["name"], request["params"]["arguments"]
        self.assertEqual(args["cluster_id"], "cluster-id")
        self.mcp_calls.append((name, args.get("action")))
        if name == "k8s_kubectl":
            result = {"status": "ok", "output": self.kubectl(args["args"])}
        elif args["action"] == "install":
            self.assertTrue(args["confirmed"])
            manifest = json.loads((Path(args["chart"]) / "templates/cm.yaml").read_text())
            release = args["release"]
            self.assertEqual(release, manifest["metadata"]["name"])
            manifest["metadata"]["uid"] = "helm-uid"
            self.maps[release] = manifest
            self.secrets["v1"] = {"name": "sh.helm.release.v1." + release + ".v1", "uid": "release-uid",
                                  "namespace": "default", "labels": {"owner": "helm", "name": release, "version": "1"}}
            result = {"status": "deploying", "next_tool": "k8s_helm",
                      "next_args": dict(action="status", cluster_id="cluster-id", release=release, namespace="default")}
        else:
            self.assertEqual(args["action"], "status")
            result = {"status": "ok", "output": "NAME: p90-helm-41\nSTATUS: deployed\n"}
        wire = {"id": 2, "result": {"content": [{"text": json.dumps(result)}]}}
        return subprocess.CompletedProcess(command, 0, stdout=json.dumps(wire))

    def test_apply_and_helm_lifecycle_across_runner_restart(self):
        k.exercise("cluster-id")
        self.assertEqual(k.observe_nodes("cluster-id"), [{"uid": "node-uid", "provider_id": "p://service-vm", "ready": True}])
        self.assertEqual(set(self.mcp_calls), {("k8s_helm","status"),("k8s_helm","install"),("k8s_kubectl",None)})
        self.records = json.loads(json.dumps(self.records))
        k.cleanup("cluster-id")
        k.cleanup("cluster-id")
        self.assertEqual(self.deleted, ["release-uid", "helm-uid", "apply-uid"])
        self.assertEqual(set(self.maps), {"neighbor"})
        self.assertTrue(all(r["state"] == "gone" for r in self.records.values()))

    def test_foreign_helm_revision_or_new_revision_prevents_uninstall(self):
        k.exercise("cluster-id")
        initial = copy.deepcopy(self.secrets)
        for change in ("replaced", "extra"):
            with self.subTest(change=change):
                self.secrets = copy.deepcopy(initial)
                if change == "replaced":
                    self.secrets["v1"]["uid"] = "other"
                else:
                    self.secrets["v2"] = {**self.secrets["v1"], "uid": "other-revision"}
                with self.assertRaises(RuntimeError):
                    k.cleanup("cluster-id")
                self.assertEqual(self.deleted, [])

    def test_replaced_map_before_helm_uninstall_preserves_both(self):
        k.exercise("cluster-id")
        self.maps["p90-helm-41"]["metadata"]["uid"] = "foreign-map"
        with self.assertRaisesRegex(RuntimeError, "replacement ConfigMap"):
            k.cleanup("cluster-id")
        self.assertEqual(self.deleted, [])
        self.assertTrue(self.secrets)

    def test_uid_is_checked_at_delete_if_replaced_after_read(self):
        k.exercise("cluster-id")
        self.late_replace = True
        with self.assertRaisesRegex(RuntimeError, "UID conflict"):
            k.cleanup("cluster-id")
        self.assertEqual(self.deleted, ["release-uid"])
        self.assertEqual(self.maps["p90-helm-41"]["metadata"]["uid"], "foreign-after-get")
        self.assertNotEqual(self.records["k8s-object:helm"]["state"], "gone")

    def test_missing_uid_in_intent_and_foreign_cluster_refuse_cleanup(self):
        k.exercise("cluster-id")
        del self.records["k8s-object:helm"]["uid"]
        for cid in ("cluster-id", "another"):
            with self.subTest(cid=cid), self.assertRaises(RuntimeError):
                k.cleanup(cid)
        self.assertEqual(self.deleted, [])


class CertManagerTest(unittest.TestCase):
    cid = "00000000-0000-4000-8000-000000000001"
    rid = "00000000-0000-4000-8000-000000000002"
    op = "00000000-0000-4000-8000-000000000003"
    elsewhere = "00000000-0000-4000-8000-000000000004"
    project = "00000000-0000-4000-8000-000000000005"

    def setUp(self):
        self.book = {}
        self.ns = None
        self.pods, self.crs, self.crds = [], [], []
        self.owned = None
        self.other = {}
        self.writes = []
        self.billable = False
        self.addCleanup(patch.stopall)
        patch.dict(os.environ, {"CLOUDRU_PROJECT_ID":self.project}).start()
        patch.object(k.common,"ledger",side_effect=lambda: copy.deepcopy(self.book)).start()
        patch.object(k.common,"note",side_effect=self.note).start()
        patch.object(k.common,"call_evo",side_effect=self.cli).start()
        patch.object(k.common,"mk8s_get",side_effect=self.get).start()
        patch.object(k.common,"mk8s_delete_addon",side_effect=self.delete).start()
        patch.object(k,"kube",side_effect=self.kube).start()
        def once(seconds, read, msg):
            result = read()
            if not result:
                raise RuntimeError(msg)
            return result
        patch.object(k,"poll_addon",side_effect=once).start()

    def note(self,kind,key,**fields):
        self.book[key] = {**self.book.get(key,{}),"kind":kind,**fields}

    def cli(self,*args,timeout):
        if args[:2] == ("k8s","addon-versions"):
            return {"addon":"cert-manager","billable":self.billable,"latest":"v1.21.1","releases":[
                {"version":"v1.21.1","latest":True,"ready_workers":1,"namespace":""}]}
        self.assertEqual(args[:4], ("--yes","tools","call","k8s_add_addon"))
        self.assertIn("resolve_mode=NONE",args)
        self.assertEqual(self.book[k.ADDON_KEY]["state"],"intent")
        self.writes.append("post")
        self.owned = {"id":self.rid,"clusterId":self.cid,"name":"cert-manager","appVersion":"v1.21.1"}
        self.ns = {"metadata":{"name":"cert-manager","uid":"kube-namespace-id"}}
        self.pods = []
        for component in k.COMPONENTS:
            self.pods.append({"metadata":{"uid":component+"-id","name":component+"-aa-bb","namespace":k.ADDON,
                              "labels":{"app.kubernetes.io/instance":k.ADDON,"pod-template-hash":"aa"},
                              "ownerReferences":[{"name":component+"-aa","kind":"ReplicaSet","uid":component+"-rs"}]},
                              "status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}})
        self.crds = [{"metadata":{"name":"issuers.cert-manager.io","uid":"crd-uid"},
                      "spec":{"group":"cert-manager.io","names":{"plural":"issuers"}}}]
        return {"status":"installing","cluster_id":self.cid,"addon":k.ADDON,"version":"v1.21.1","operation_id":self.op}

    def get(self,path,query):
        if path == "/v3/operations/"+self.op:
            return 200, {"id":self.op,"resourceId":self.rid,"done":True}
        if path == "/v3/clusterAddons":
            self.assertEqual(query["clusterId"],self.cid)
            self.assertEqual(query["projectId"],self.project)
            if query.get("pageToken") == "end":
                return 200,{"clusterAddons":list(self.other.values())}
            return 200,{"nextPageToken":"end","clusterAddons":[self.owned] if self.owned else []}
        self.assertEqual(path,"/v3/clusterAddons/"+self.rid)
        return (200,self.owned) if self.owned else (404,{"code":5,"message":"cluster addon not found"})

    def kube(self,cid,args):
        self.assertEqual(cid,self.cid)
        if args.startswith("get namespace"):
            return json.dumps(self.ns) if self.ns else ""
        if args.startswith("get pods -n cert-manager"):
            return json.dumps({"items":self.pods})
        if args.startswith("get customresourcedefinitions"):
            return json.dumps({"items":self.crds})
        if args.startswith("get issuers.cert-manager.io -A -o=jsonpath-as-json="):
            return json.dumps(self.crs)
        self.fail("unexpected kube request")

    def delete(self,rid,project):
        self.assertEqual((rid,project),(self.rid,self.project))
        self.assertEqual(self.book[k.ADDON_KEY]["state"],"delete-intent")
        self.writes.append("delete")
        self.owned = None
        self.pods, self.crds = [], []
        return 200, {"id":self.op}

    def test_three_ready_components_recorded_and_deleted_by_operation_id(self):
        self.other[self.elsewhere] = {"id":self.elsewhere,"name":"unrelated","clusterId":self.cid}
        k.exercise_addon(self.cid)
        r = self.book[k.ADDON_KEY]
        self.assertEqual((r["state"],r["operation"],r["uid"]),("ready",self.op,self.rid))
        self.assertEqual({p["component"] for p in r["pods"]},k.COMPONENTS)
        self.book = copy.deepcopy(self.book)
        k.cleanup_addon(self.cid)
        k.cleanup_addon(self.cid)
        self.assertEqual(self.writes,["post","delete"])
        self.assertEqual(self.book[k.ADDON_KEY]["namespace_after_uninstall"],"present")
        self.assertIn(self.elsewhere,self.other)

    def test_foreign_custom_resource_or_replaced_pod_prevents_cascade(self):
        k.exercise_addon(self.cid)
        self.crs = ["somebody-elses-issuer"]
        with self.assertRaisesRegex(RuntimeError,"existing certificate"):
            k.cleanup_addon(self.cid)
        self.crs = []
        self.pods[0]["metadata"]["uid"] = "foreign-replacement"
        with self.assertRaisesRegex(RuntimeError,"replacement addon pod"):
            k.cleanup_addon(self.cid)
        self.assertEqual(self.writes,["post"])

    def test_existing_certificate_install_and_changed_tariff_no_post(self):
        self.billable = True
        with self.assertRaisesRegex(RuntimeError,"free cert-manager"):
            k.exercise_addon(self.cid)
        self.billable = False
        self.other[self.elsewhere] = {"id":self.elsewhere,"name":k.ADDON,"clusterId":self.cid}
        with self.assertRaisesRegex(RuntimeError,"previous cert-manager"):
            k.exercise_addon(self.cid)
        self.assertEqual(self.writes,[])

    def test_nonready_worker_or_unknown_delete_does_not_claim_readiness_or_absence(self):
        real_cli = self.cli
        def cli(*args,timeout):
            out = real_cli(*args,timeout=timeout)
            if "k8s_add_addon" in args:
                self.pods[-1]["status"]["conditions"][0]["status"] = "False"
            return out
        with patch.object(k.common,"call_evo",side_effect=cli), self.assertRaisesRegex(RuntimeError, "did not all reach Ready"):
            k.exercise_addon(self.cid)
        self.assertNotEqual(self.book[k.ADDON_KEY]["state"],"ready")
        with patch.object(k.common,"mk8s_delete_addon",return_value=(200,{})), self.assertRaisesRegex(RuntimeError, "still present"):
            k.cleanup_addon(self.cid)
        self.assertNotEqual(self.book[k.ADDON_KEY]["state"],"gone")


if __name__ == "__main__":
    unittest.main()
