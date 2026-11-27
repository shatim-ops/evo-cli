"""Exercise the real Ed25519 boundary without access to a stand or cloud credentials."""
import base64
import json
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

import isolation


class IsolationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.shell("genpkey", "-algorithm", "ED25519", "-out", "private")
        self.shell("pkey", "-in", "private", "-pubout", "-outform", "DER", "-out", "public")
        self.public = (self.path / "public").read_bytes()
        now = int(time.time())
        self.ctx = dict(project="test-project", repository="fixture", workflow="f" * 40,
                        candidate=isolation.common.CANDIDATE, principal_key_sha256="e" * 64)
        self.claims = dict(self.ctx, kind="exclusive-project-writer/v1",
                           services=["artifact-registry", "compute", "container-apps", "dns", "kubernetes-rbac"],
                           guarantees=["only-subject-can-write", "no-writer-grants-until-expiry"],
                           reservation_id="test-reservation", start=now - 60, end=now + 240)
        self.now = now

    def shell(self, *args):
        subprocess.run(["openssl", *args], cwd=self.path, capture_output=True, check=True)

    def envelope(self, claims):
        body = json.dumps(claims).encode()
        (self.path / "body").write_bytes(body)
        self.shell("pkeyutl", "-sign", "-inkey", "private", "-rawin", "-in", "body", "-out", "sig")
        return {"payload": base64.b64encode(body).decode(),
                "signature": base64.b64encode((self.path / "sig").read_bytes()).decode()}

    def test_valid_grant_boundary(self):
        grant = isolation.Grant(self.envelope(self.claims), self.public, self.ctx)
        grant.require("test-project", "p90-ca-238-cli", self.now, self.now + 120)
        for project, name, start, end in (
                ("wrong", "p90-ca-238-mcp", self.now, self.now + 120),
                ("test-project", "someone-else", self.now, self.now + 120),
                ("test-project", "p90-ca-238-cli", self.now, self.now + 241),
                ("test-project", "p90-ca-238-cli", self.now - 61, self.now + 20)):
            with self.assertRaises(isolation.ContainerAppsBlocked):
                grant.require(project, name, start, end)

    def test_substitution_or_unbound_key(self):
        for changes in ({"project": "other"}, {"workflow": "2" * 40}, {"principal_key_sha256": "f" * 64},
                        {"guarantees": ["only-subject-can-write"]}, {"services": ["container-apps"]}):
            with self.assertRaises(isolation.ContainerAppsBlocked):
                isolation.Grant(self.envelope(dict(self.claims, **changes)), self.public, self.ctx)
        e = self.envelope(self.claims)
        e["signature"] = base64.b64encode(b"a" * 64).decode()
        with self.assertRaises(isolation.ContainerAppsBlocked):
            isolation.Grant(e, self.public, self.ctx)
