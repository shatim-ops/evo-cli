"""Validate a stand controller's signed, non-revocable exclusive-writer grant.

The trust anchor belongs to the system enforcing the grant, independently of
the Actions principal. A review, an IAM snapshot or a statement signed by
the runner is not a grant. This verifier does not change permissions.
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time

import common


class ContainerAppsBlocked(RuntimeError):
    pass


def blocked():
    return ContainerAppsBlocked("Проверяемое окно исключительной записи отсутствует. Создание заблокировано.")


class Grant:
    def __init__(self, envelope, public_der, context):
        # RFC 8410 SubjectPublicKeyInfo prefix for Ed25519, then the 32-byte public key.
        if len(public_der) != 44 or public_der[:12].hex() != "302a300506032b6570032100":
            raise blocked()
        payload = base64.b64decode(envelope["payload"], validate=True)
        signature = base64.b64decode(envelope["signature"], validate=True)
        if len(payload) > 8192 or len(signature) != 64:
            raise blocked()
        with tempfile.TemporaryDirectory() as temp:
            d = Path(temp)
            for name, data in (("issuer.der", public_der), ("grant", payload), ("signature", signature)):
                (d / name).write_bytes(data)
            verified = subprocess.run(["openssl", "pkeyutl", "-verify", "-pubin", "-keyform", "DER",
                                       "-inkey", str(d / "issuer.der"), "-rawin", "-in", str(d / "grant"),
                                       "-sigfile", str(d / "signature")], capture_output=True, timeout=5)
        if verified.returncode:
            raise blocked()
        claims = json.loads(payload, object_pairs_hook=common.unique_keys)
        if (not isinstance(claims, dict) or any(claims.get(k) != v for k, v in context.items())
                or claims.get("kind") != "exclusive-project-writer/v1"
                or claims.get("services") != ["artifact-registry", "compute", "container-apps", "dns", "kubernetes-rbac"]
                or claims.get("guarantees") != ["only-subject-can-write", "no-writer-grants-until-expiry"]
                or not isinstance(claims.get("reservation_id"), str)
                or not re.fullmatch(r"[A-Za-z0-9._-]{1,128}", claims["reservation_id"])
                or type(claims.get("start")) is not int or type(claims.get("end")) is not int
                or claims["end"] <= claims["start"]):
            raise blocked()
        self.claims = claims
        self.fingerprint = hashlib.sha256(payload).hexdigest()

    def interval(self, project, start, end):
        now = int(time.time())
        if (project != self.claims["project"]
                or start > end or not self.claims["start"] <= start <= now < end <= self.claims["end"]):
            raise blocked()

    def require(self, project, name, start, end):
        if not re.fullmatch(r"p90-ca-[1-9][0-9]{0,19}-(?:mcp|cli)", name):
            raise blocked()
        self.interval(project, start, end)


def for_registry(start, end):
    grant = original()
    grant.interval(os.environ["CLOUDRU_PROJECT_ID"], start, end)


def dns_window(start, end):
    original().interval(os.environ["CLOUDRU_PROJECT_ID"], start, end)


def original():
    grant = acquire()
    saved = common.ledger().get("ca-isolation", {})
    if saved.get("grant_sha256") != grant.fingerprint:
        raise blocked()
    return grant


def acquire():
    try:
        credential = os.environ["CLOUDRU_KEY_ID"]
        context = {"project": os.environ["CLOUDRU_PROJECT_ID"], "workflow": os.environ["GITHUB_SHA"],
                   "candidate": common.CANDIDATE, "repository": os.environ["GITHUB_REPOSITORY_ID"],
                   "principal_key_sha256": hashlib.sha256(credential.encode()).hexdigest()}
        return Grant(json.loads(os.environ["P90_EXCLUSIVE_GRANT"], object_pairs_hook=common.unique_keys),
                     base64.b64decode(os.environ["P90_ISOLATION_AUTHORITY"], validate=True), context)
    except (KeyError, TypeError, ValueError, OSError, RuntimeError, subprocess.SubprocessError):
        raise blocked() from None


def verify_dispatch_grant():
    grant = acquire()
    now = int(time.time())
    grant.interval(os.environ["CLOUDRU_PROJECT_ID"], now, now + 60)
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="ascii") as out:
        print("grant=" + grant.fingerprint, file=out)


if __name__ == "__main__":
    try:
        verify_dispatch_grant()
    except ContainerAppsBlocked:
        raise SystemExit("The stand has not proved exclusive writes for the reviewed operations.") from None
