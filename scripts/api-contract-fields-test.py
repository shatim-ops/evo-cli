#!/usr/bin/env python3
"""Tests of the field ratchet of scripts/api-contract-check.py, offline.

A fake spec of one service and a fake contract: a field the spec gains after
the baseline fails the check unless evo reads or sends it, it is of an
ignored class or the request accepts it; a new object is one finding, not
one per field under it; --update-fields writes the baseline (release plan
1.6, the ratchet of the specs in job contract).

Usage: python3 scripts/api-contract-fields-test.py
"""
import importlib.util
import io
import json
import os
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout, redirect_stderr

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("api_contract_check", os.path.join(HERE, "api-contract-check.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

SPEC = """
openapi: 3.0.0
paths:
  /v1/things/{id}:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: string}
                  name: {type: string}
                  size: {type: integer}
                  created_at: {type: string}
                  displayName: {type: string}
                  legacy: {type: string, deprecated: true}
                  zone:
                    type: object
                    properties:
                      id: {type: string}
                      name: {type: string}
                  network:
                    type: object
                    properties:
                      subnet: {type: string}
                      vpc: {type: string}
                  tags:
                    type: array
                    items: {type: string}
  /v1/things:
    post:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name: {type: string}
                size: {type: integer}
                state: {type: string, readOnly: true}
                encrypted: {type: boolean}
      responses:
        "200": {description: ok}
"""


class FieldRatchet(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.makedirs(os.path.join(self.dir, "specs", "svc"))
        with open(os.path.join(self.dir, "specs", "svc", "api.yaml"), "w") as f:
            f.write(SPEC)
        self.contract = {"services": [{"name": "svc", "spec": "svc/api.yaml", "requests": [
            {"method": "GET", "path": "/v1/things/{id}", "reads": ["id", "name"]},
            {"method": "POST", "path": "/v1/things", "body": ["name"]},
        ]}]}
        self.baseline = {"requests": {
            "svc GET /v1/things/{id}": ["id", "name"],
            "svc POST /v1/things": ["name"],
        }}

    def tearDown(self):
        shutil.rmtree(self.dir)

    def run_check(self, *extra):
        cp = os.path.join(self.dir, "contract.json")
        fp = os.path.join(self.dir, "fields.json")
        with open(cp, "w") as f:
            json.dump(self.contract, f)
        with open(fp, "w") as f:
            json.dump(self.baseline, f)
        argv = ["api-contract-check.py", "--specs", os.path.join(self.dir, "specs"), "--contract", cp,
                "--root", self.dir, "--fields", fp, *extra]
        out, err = io.StringIO(), io.StringIO()
        old = sys.argv
        sys.argv = argv
        try:
            with redirect_stdout(out), redirect_stderr(err):
                rc = check.main()
        finally:
            sys.argv = old
        with open(fp) as f:
            return rc, out.getvalue(), json.load(f)

    def test_new_fields(self):
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 1, out)
        # A new field of the answer evo does not read, and a new object once.
        self.assertIn("field-new:size", out)
        self.assertIn("field-new:network", out)
        self.assertNotIn("field-new:network.subnet", out)
        self.assertIn("field-new:tags[]", out)
        # Ignored classes: *_at, display*, deprecated, a mini reference.
        for ignored in ("created_at", "displayName", "legacy", "zone.id", "zone.name"):
            self.assertNotIn("field-new:" + ignored, out)
        # The body: a new field evo does not send; readOnly is ignored.
        self.assertIn("field-new:encrypted", out)
        self.assertNotIn("field-new:state", out)
        # zone itself is an object of a mini reference: new, reported.
        self.assertRegex(out, r"field-new:zone\b(?!\.)")

    def test_read_or_accepted(self):
        req = self.contract["services"][0]["requests"]
        req[0]["reads"] += ["size", "network.subnet", "tags[]", "zone.id"]
        req[0]["accepted"] = {"field-new:network.vpc": "the VPC is named by the subnet"}
        req[1]["body"] += ["size"]
        req[1]["accepted"] = {"field-new:encrypted": "evo creates unencrypted things (declared, #496)"}
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)
        self.assertNotIn("[error", out)

    def test_gone_and_missing(self):
        self.baseline["requests"]["svc GET /v1/things/{id}"] += ["old_field"]
        del self.baseline["requests"]["svc POST /v1/things"]
        _, out, _ = self.run_check()
        self.assertIn("field-gone:old_field", out)
        self.assertIn("field-baseline-missing", out)

    def test_impact_unknown(self):
        # A field of impact the body of the spec does not have: an error.
        req = self.contract["services"][0]["requests"][1]
        req["impact"] = {"size": "цена", "encrypt": "безопасность; пропуск: план"}
        _, out, _ = self.run_check()
        self.assertIn("impact-unknown:encrypt", out)
        self.assertNotIn("impact-unknown:size", out)

    def test_update(self):
        rc, out, written = self.run_check("--update-fields")
        self.assertEqual(rc, 0, out)
        self.assertIn("network.subnet", written["requests"]["svc GET /v1/things/{id}"])
        self.assertIn("encrypted", written["requests"]["svc POST /v1/things"])
        self.assertIn("_comment", written)
        self.baseline = written
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)


if __name__ == "__main__":
    unittest.main()
