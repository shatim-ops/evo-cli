#!/usr/bin/env python3
"""Tests of the field ratchet and of the state ratchet of
scripts/api-contract-check.py, offline.

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
                options:
                  type: object
                  properties:
                    fast: {type: boolean}
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
        # No states: the fake spec has none (StateRatchet tests them).
        argv = ["api-contract-check.py", "--specs", os.path.join(self.dir, "specs"), "--contract", cp,
                "--root", self.dir, "--fields", fp, "--states", os.path.join(self.dir, "no-states.json"), *extra]
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
        req[1]["accepted"] = {"field-new:encrypted": "evo creates unencrypted things (declared, #496)",
                              "field-new:options": "evo leaves the options to the platform's defaults"}
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
        req["impact"].update({"options.fast": "работа", "options.nope": "работа"})
        _, out, _ = self.run_check()
        self.assertIn("impact-unknown:encrypt", out)
        self.assertNotIn("impact-unknown:size", out)
        # A nested field is looked up through the schema, as body-unknown.
        self.assertNotIn("impact-unknown:options.fast", out)
        self.assertIn("impact-unknown:options.nope", out)

    def test_update(self):
        rc, out, written = self.run_check("--update-fields")
        self.assertEqual(rc, 0, out)
        self.assertIn("network.subnet", written["requests"]["svc GET /v1/things/{id}"])
        self.assertIn("encrypted", written["requests"]["svc POST /v1/things"])
        self.assertIn("_comment", written)
        self.baseline = written
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)


STATES_SPEC = """
openapi: 3.0.0
paths: {}
components:
  schemas:
    ThingState:
      type: string
      enum: [creating, ready, deleting]
    acme.v1.Thing:
      type: object
      properties:
        status:
          allOf:
            - $ref: "#/components/schemas/ThingState"
        phases:
          type: array
          items: {type: string, enum: [one, two]}
        name: {type: string}
"""


class StateRatchet(unittest.TestCase):
    """The states of scripts/api-contract-states.json against the specs
    (release plan 1.9, «мир несёт каждое состояние из спецификации»): a
    state the spec gains fails the check until the file has it, and the test
    of the worlds then asks for its row."""

    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.makedirs(os.path.join(self.dir, "specs", "svc"))
        with open(os.path.join(self.dir, "specs", "svc", "api.yaml"), "w") as f:
            f.write(STATES_SPEC)
        self.states = {"_comment": "kept", "states": {
            "svc:ThingState": ["creating", "ready", "deleting"],
            "svc:acme.v1.Thing.status": ["creating", "ready", "deleting"],
            "svc:acme.v1.Thing.phases": ["one", "two"],
        }}

    def tearDown(self):
        shutil.rmtree(self.dir)

    def run_check(self, *extra):
        cp = os.path.join(self.dir, "contract.json")
        fp = os.path.join(self.dir, "fields.json")
        sp = os.path.join(self.dir, "states.json")
        with open(cp, "w") as f:
            json.dump({"services": [{"name": "svc", "spec": "svc/api.yaml", "requests": []}]}, f)
        with open(fp, "w") as f:
            json.dump({"requests": {}}, f)
        with open(sp, "w") as f:
            json.dump(self.states, f)
        argv = ["api-contract-check.py", "--specs", os.path.join(self.dir, "specs"), "--contract", cp,
                "--root", self.dir, "--fields", fp, "--states", sp, *extra]
        out, err = io.StringIO(), io.StringIO()
        old = sys.argv
        sys.argv = argv
        try:
            with redirect_stdout(out), redirect_stderr(err):
                rc = check.main()
        finally:
            sys.argv = old
        with open(sp) as f:
            return rc, out.getvalue(), json.load(f)

    def test_as_the_spec(self):
        # A schema, a field behind allOf and $ref, an array of an enum.
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)
        self.assertNotIn("state-", out)

    def test_new_state(self):
        self.states["states"]["svc:ThingState"].remove("deleting")
        self.states["states"]["svc:acme.v1.Thing.phases"].remove("two")
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 1, out)
        self.assertIn("state-new:deleting", out)
        self.assertIn("state-new:two", out)
        self.assertEqual(out.count("state-new:"), 2, out)

    def test_gone_state_is_no_failure(self):
        self.states["states"]["svc:ThingState"].append("paused")
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)
        self.assertIn("state-gone:paused", out)

    def test_key_that_names_nothing(self):
        # A schema renamed, a field that is no enum, a service not in the
        # contract: the key holds nothing, and that is an error, not a pass.
        for key, want in (("svc:ThingStatus", "state-enum-missing"), ("svc:acme.v1.Thing.name", "state-enum-missing"),
                          ("other:ThingState", "state-spec-missing")):
            self.states["states"] = {key: ["creating"]}
            rc, out, _ = self.run_check()
            self.assertEqual(rc, 1, key + out)
            self.assertIn(want, out)

    def test_code_only_reads_no_states(self):
        self.states["states"]["svc:ThingState"].remove("deleting")
        rc, out, _ = self.run_check("--code-only")
        self.assertEqual(rc, 0, out)
        self.assertNotIn("state-new", out)

    def test_update(self):
        self.states["states"]["svc:ThingState"] = ["creating", "paused"]
        self.states["states"]["svc:ThingStatus"] = ["kept as it is"]
        rc, out, written = self.run_check("--update-states")
        # The key that names nothing stays an error and stays as it was.
        self.assertEqual(rc, 1, out)
        self.assertEqual(written["states"]["svc:ThingState"], ["creating", "ready", "deleting"])
        self.assertEqual(written["states"]["svc:ThingStatus"], ["kept as it is"])
        self.assertEqual(written["_comment"], "kept")
        del written["states"]["svc:ThingStatus"]
        self.states = written
        rc, out, _ = self.run_check()
        self.assertEqual(rc, 0, out)


if __name__ == "__main__":
    unittest.main()
