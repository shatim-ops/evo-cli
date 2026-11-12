#!/usr/bin/env python3
"""Tests of scripts/probe-requests.py on a spec written here: no network.

Usage: python3 scripts/probe-requests-test.py
"""
import importlib.util
import json
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("probe_requests", os.path.join(HERE, "probe-requests.py"))
pr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pr)

SPEC = {
    "openapi": "3.0.0",
    "paths": {
        "/api/v1/vms/{vm_id}/set-power": {"post": {
            "summary": "Питание",
            "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Power"}}}},
            "responses": {"204": {}, "422": {}}}},
        "/api/v1/vms/{vm_id}": {"put": {
            "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Update"}}}},
            "responses": {"200": {}}}},
        "/api/v1/backups": {
            # A parameter of the path level belongs to each of its requests.
            "parameters": [{"name": "project_id", "in": "query", "required": True}],
            "get": {"parameters": [{"name": "state", "in": "query"}],
                    "responses": {"200": {}}},
            "post": {"requestBody": {"content": {
                "application/yaml": {"schema": {"$ref": "#/components/schemas/Power"}},
                "application/json": {"schema": {"$ref": "#/components/schemas/Backup"}}}},
                     "responses": {"201": {}}}},
    },
    "components": {"schemas": {
        "Power": {"required": ["state"], "properties": {"state": {"type": "string", "enum": ["power_on", "power_off"]}}},
        # «not set» is the integer 1 in an anyOf next to the real type.
        "Update": {"properties": {"flavor_name": {"anyOf": [{"type": "string"}, {"type": "integer", "enum": [1]}]}}},
        "Backup": {"required": ["name", "resource"], "properties": {
            "name": {"type": "string"},
            "tag_names": {"type": "array", "items": {"type": "string"}},
            "resource": {"$ref": "#/components/schemas/Res"},
            "self": {"$ref": "#/components/schemas/Backup"}}},
        "Res": {"required": ["resource_type"], "properties": {
            "resource_type": {"allOf": [{"$ref": "#/components/schemas/Kind"}]}}},
        "Kind": {"type": "string", "enum": ["disk", "virtual_machine"]},
    }},
}


class Requests(unittest.TestCase):
    def test_body_fields_required_and_enums(self):
        r = pr.describe(SPEC, "POST", "/api/v1/vms/{vm_id}/set-power")
        self.assertEqual(r["body"], [{"name": "state", "type": "string", "required": True, "enum": ["power_on", "power_off"]}])
        self.assertEqual(r["responses"], ["204", "422"])
        self.assertEqual(r["summary"], "Питание")

    def test_any_of_names_every_type(self):
        r = pr.describe(SPEC, "PUT", "/api/v1/vms/{vm_id}")
        self.assertEqual(r["body"][0]["type"], "string | integer")

    def test_nested_ref_and_cycle(self):
        r = pr.describe(SPEC, "POST", "/api/v1/backups")
        names = [f["name"] for f in r["body"]]
        self.assertIn("resource.resource_type", names)
        self.assertIn("tag_names", names)
        kind = next(f for f in r["body"] if f["name"] == "resource.resource_type")
        self.assertTrue(kind["required"])
        self.assertEqual(kind["enum"], ["disk", "virtual_machine"])
        # A schema that refers to itself is named once, not walked for ever.
        self.assertEqual(names.count("self"), 1)
        self.assertNotIn("self.self.self.self.self.name", names)

    def test_json_body_before_other_content(self):
        r = pr.describe(SPEC, "POST", "/api/v1/backups")
        self.assertNotIn("state", [f["name"] for f in r["body"]])

    def test_deep_nesting_is_walked(self):
        schema = {"properties": {"leaf": {"type": "string"}}}
        for i in range(6):
            schema = {"properties": {f"l{i}": schema}}
        doc = {"paths": {"/x": {"post": {"requestBody": {"content": {"application/json": {"schema": schema}}},
                                         "responses": {}}}}}
        names = [f["name"] for f in pr.describe(doc, "POST", "/x")["body"]]
        self.assertIn("l5.l4.l3.l2.l1.l0.leaf", names)

    def test_query(self):
        r = pr.describe(SPEC, "GET", "/api/v1/backups")
        self.assertEqual(r["query"], ["project_id*", "state"])

    def test_absent_request_is_named_not_skipped(self):
        r = pr.describe(SPEC, "DELETE", "/api/v1/backups")
        self.assertEqual(r, {"absent": True})

    def test_render_marks_absent_and_fails(self):
        with tempfile.TemporaryDirectory() as d:
            os.makedirs(os.path.join(d, "compute"))
            with open(os.path.join(d, "compute", "s.json"), "w", encoding="utf-8") as f:
                json.dump(SPEC, f)
            plan = {"probes": [{"id": "Г-10", "title": "ВМ", "requests": [
                {"spec": "compute/s.json", "method": "POST", "path": "/api/v1/vms/{vm_id}/set-power"},
                {"spec": "compute/s.json", "method": "POST", "path": "/api/v1/tags", "note": "сервис тегов"}]}]}
            text, absent = pr.render(plan, d)
        self.assertIn("### Г-10. ВМ", text)
        self.assertIn("`state`* — string: `power_on`, `power_off`", text)
        self.assertIn("**нет в спецификации**", text)
        self.assertIn("сервис тегов", text)
        self.assertEqual(absent, ["Г-10: POST /api/v1/tags (compute/s.json)"])


if __name__ == "__main__":
    unittest.main()
