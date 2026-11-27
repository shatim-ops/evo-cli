"""Shared helpers for the manually approved patch check. Never log request bodies."""
import base64
import hashlib
import hmac
import json
import os
import re
import subprocess
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


CANDIDATE = "d7645da5efea04122f984cfd8cb9a28408d08177"
SUITES = ("reads", "apps", "core", "misc", "cluster", "pg")
COMPONENT = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,99}")
RECORD_KEY = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/-]{0,155}")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


def request(method, url, headers, body=None, limit=2_000_000):
    data = None if body is None else json.dumps(body).encode()
    req = Request(url, data=data, headers=headers, method=method)
    try:
        response = build_opener(NoRedirect()).open(req, timeout=25)
    except HTTPError as error:
        response = error
    except (URLError, OSError):
        raise RuntimeError("transport failed; outcome unknown") from None
    with response:
        status = response.status
        if response.getheader("Content-Range") is not None or status == 206:
            raise RuntimeError("partial HTTP response")
        raw = response.read(limit + 1)
        length = response.getheader("Content-Length")
        if len(raw) > limit or (length is not None and int(length) != len(raw)):
            raise RuntimeError("incomplete response")
        if not raw:
            return status, None
        try:
            return status, json.loads(raw, object_pairs_hook=unique_keys)
        except (ValueError, UnicodeError):
            return status, None


def unique_keys(pairs):
    values = dict(pairs)
    if len(values) != len(pairs):
        raise RuntimeError("duplicate key in response")
    return values


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(",", ":"), allow_nan=False).encode()


class ReceiptBook:
    """One authenticated snapshot per run on a dedicated branch of a private repository.

    A new commit replaces the previous snapshot by blob SHA. A lost PUT response
    is resolved by reading back the exact signed content, never by repeating a POST.
    """
    def __init__(self):
        repo = os.environ["P90_RECEIPT_REPOSITORY"].split("/")
        self.branch = os.environ["P90_RECEIPT_BRANCH"]
        if len(repo) != 2 or not all(COMPONENT.fullmatch(v) for v in repo):
            raise RuntimeError("invalid receipt repository")
        if not COMPONENT.fullmatch(self.branch) or self.branch in {"main", "master"}:
            raise RuntimeError("dedicated receipt branch required")
        self.repo_url = "https://api.github.com/repos/" + "/".join(repo)
        self.token = os.environ["P90_RECEIPT_TOKEN"]
        self.key = bytes.fromhex(os.environ["P90_RECEIPT_HMAC_KEY"])
        if len(self.key) != 32 or not self.token:
            raise RuntimeError("invalid receipt credentials")
        run = os.environ["P90_SOURCE_RUN"]
        workflow = os.environ["GITHUB_SHA"]
        if not re.fullmatch(r"[1-9][0-9]{0,19}", run) or not re.fullmatch(r"[0-9a-f]{40}", workflow):
            raise RuntimeError("invalid workflow identity")
        self.context = {"candidate": CANDIDATE, "workflow": workflow, "run": run,
                        "repository": os.environ["GITHUB_REPOSITORY_ID"], "project": os.environ["CLOUDRU_PROJECT_ID"]}
        suite = os.environ["P90_SUITE"]
        if suite not in SUITES:
            raise RuntimeError("invalid receipt suite")
        self.context["suite"] = suite
        self.path = "/contents/patch-1.9.1/" + workflow + "/" + run + "/" + suite + ".json"

    def git(self, verb, path, body=None):
        return request(verb, self.repo_url + path,
                       {"Authorization": "Bearer " + self.token, "Accept": "application/vnd.github+json",
                        "Content-Type": "application/json", "User-Agent": "evo-release-receipts"}, body)

    def verify_repository(self):
        status, repo = self.git("GET", "")
        if status != 200 or not isinstance(repo, dict) or repo.get("private") is not True:
            raise RuntimeError("private receipt repository unconfirmed")
        if self.branch == repo.get("default_branch"):
            raise RuntimeError("receipt branch must be separate")
        if self.git("GET", "/branches/" + self.branch)[0] != 200:
            raise RuntimeError("receipt branch unavailable")

    def signed(self, records):
        payload = {"context": self.context, "records": records}
        return {"payload": payload, "mac": hmac.new(self.key, canonical(payload), hashlib.sha256).hexdigest()}

    def read(self):
        status, file = self.git("GET", self.path + "?ref=" + self.branch)
        if status == 404:
            return None, {}
        if status != 200 or not isinstance(file, dict) or file.get("encoding") != "base64":
            raise RuntimeError("receipt could not be read")
        try:
            doc = json.loads(base64.b64decode(file["content"]))
            records = doc["payload"]["records"]
            ok = (isinstance(records, dict) and doc["payload"]["context"] == self.context
                  and hmac.compare_digest(self.signed(records)["mac"], doc["mac"]))
            if not ok or not re.fullmatch(r"[0-9a-f]{40}", file["sha"]):
                raise ValueError()
            return file["sha"], records
        except (ValueError, KeyError, TypeError):
            raise RuntimeError("receipt integrity check failed") from None

    def save(self, prev_sha, records):
        encoded = canonical(self.signed(records))
        if len(encoded) > 131072:
            raise RuntimeError("receipt size limit reached")
        body = {"branch": self.branch, "content": base64.b64encode(encoded).decode(),
                "message": "Record patch verification checkpoint [skip ci]"}
        if prev_sha:
            body["sha"] = prev_sha
        try:
            status, _ = self.git("PUT", self.path, body)
        except RuntimeError:
            status = None
        _, observed = self.read()
        if observed != records or status not in (None, 200, 201):
            raise RuntimeError("durable receipt write not confirmed")

    def start(self, recovery=False):
        self.verify_repository()
        sha, records = self.read()
        if recovery:
            if not sha or "attempt" not in records:
                raise RuntimeError("original receipt missing; recovery cannot infer IDs")
        else:
            if sha:
                raise RuntimeError("a receipt for this run already exists")
            self.save(None, {"attempt": {"kind": "attempt", "status": "opened"}})


def ledger():
    sha, records = ReceiptBook().read()
    if not sha or "attempt" not in records:
        raise RuntimeError("original receipt missing")
    return records


def note(kind, key, **fields):
    if not RECORD_KEY.fullmatch(key) or not RECORD_KEY.fullmatch(kind) or "kind" in fields:
        raise RuntimeError("invalid checkpoint")
    book = ReceiptBook()
    sha, records = book.read()
    if not sha or "attempt" not in records:
        raise RuntimeError("no durable receipt")
    old = records.get(key, {"kind": kind})
    if old["kind"] != kind:
        raise RuntimeError("different resource kind at checkpoint")
    updated = {**old, **fields}
    if key in records and updated == old:
        return
    records[key] = updated
    book.save(sha, records)


def call_evo(*args, timeout=90):
    try:
        result = subprocess.run([os.environ["EVO_BIN"], *args], capture_output=True, timeout=timeout, check=False)
    except (subprocess.TimeoutExpired, OSError):
        raise RuntimeError("candidate invocation incomplete") from None
    if result.returncode:
        raise RuntimeError("candidate exit " + str(result.returncode))
    try:
        return json.loads(result.stdout)
    except ValueError:
        raise RuntimeError("candidate returned invalid JSON") from None


_bearer = None
_bearer_until = 0.0


def cloud_auth():
    global _bearer, _bearer_until
    if _bearer is None or time.monotonic() >= _bearer_until:
        status, body = request("POST", "https://iam.api.cloud.ru/api/v1/auth/token",
                               {"Content-Type": "application/json"},
                               {"keyId": os.environ["CLOUDRU_KEY_ID"], "secret": os.environ["CLOUDRU_SECRET"]}, 65536)
        if status != 200 or not isinstance(body, dict):
            raise RuntimeError("IAM exchange failed, HTTP " + str(status))
        token = body.get("access_token") or body.get("accessToken")
        if not isinstance(token, str) or not token:
            raise RuntimeError("IAM response missing token")
        expires = body.get("expires_in", body.get("expiresIn", 600))
        seconds = expires if type(expires) in (float, int) and expires > 0 else 600
        _bearer_until = time.monotonic() + min(600, max(0, seconds - 60))
        _bearer = token
    return _bearer


def service(method, host, path, query=None, body=None):
    ident = r"[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}"
    if host == "dns" and method == "PATCH":
        if (not re.fullmatch(rf"/v1/privateZones/{ident}", path) or query
                or body != {"linkedVpcIds": []}):
            raise RuntimeError("unreviewed DNS unlink")
    elif host == "mk8s" and method == "DELETE":
        if (not re.fullmatch(rf"/v3/clusterAddons/{ident}", path)
                or query != {"projectId": os.environ["CLOUDRU_PROJECT_ID"], "preserve": "false"}):
            raise RuntimeError("unreviewed add-on mutation")
    elif host == "secretmanager":
        allowed = {
            "GET": rf"/v1/secrets/{ident}(?:/versions(?:/[1-9][0-9]{{0,9}})?)?",
            "DELETE": rf"/v1/secrets/{ident}(?:/versions/[1-9][0-9]{{0,9}})?",
            "POST": rf"/v1/secrets/{ident}/versions",
        }
        if not re.fullmatch(allowed.get(method, r"(?!)"), path):
            raise RuntimeError("unreviewed Secret Manager path")
    else:
        read_paths = {
            "postgresql": r"/v1/(?:clusters|operations)(?:/[A-Za-z0-9_./%:-]+)?",
            "ar": rf"/v1/(?:registries|operations)/{ident}",
            "dns": rf"/v1/(?:(?:privateZones|privateRecordsSole)(?:/{ident})?|(?:operations|publicZones|publicRecordsSole)/{ident})",
            "certificatemanager": rf"/v1/certificate/{ident}",
            "mk8s": r"/v3/[A-Za-z0-9_./:-]+",
            "vpc": rf"/v1/vpcs(?:/{ident}(?:/routes/static)?|/operations/{ident})?",
            "nlb": rf"/v2/(?:(?:targetGroups|networkLoadBalancers)(?:/{ident})?|operations/{ident})",
        }
        if host == "compute":
            if method not in {"GET", "POST", "DELETE"} or not path.startswith("/api/v1/"):
                raise RuntimeError("unreviewed Compute path")
        elif method != "GET" or host not in read_paths or not re.fullmatch(read_paths[host], path):
            raise RuntimeError("request outside reviewed services")
    url = urlsplit(path)
    if (not path.startswith("/") or url.scheme or url.netloc or url.fragment or url.query or "//" in path
            or re.search(r"(?:^|/)\.\.(?:/|$)|[?#\\\r\n]", path)):
        raise RuntimeError("invalid relative service path")
    target = "https://" + host + ".api.cloud.ru" + path
    if query:
        target += "?" + urlencode(query)
    def send():
        return request(method, target, {"Authorization": "Bearer " + cloud_auth(), "Content-Type": "application/json"}, body)
    status, response = send()
    if method == "GET" and status == 401:
        global _bearer
        _bearer = None
        return send()
    return status, response


def pg_get(path):
    if not path.startswith("/v1/"):
        raise RuntimeError("unexpected PostgreSQL path")
    return service("GET", "postgresql", path)


def compute(method, path, query=None, body=None):
    if not path.startswith("/api/v1/"):
        raise RuntimeError("unexpected Compute path")
    return service(method, "compute", path, query, body)


def mk8s_get(path, query=None):
    return service("GET", "mk8s", path, query)

def mk8s_delete_addon(uid, project):
    if project != os.environ["CLOUDRU_PROJECT_ID"]:
        raise RuntimeError("wrong add-on project")
    return service("DELETE", "mk8s", "/v3/clusterAddons/" + uid, {"projectId": project, "preserve": "false"})
