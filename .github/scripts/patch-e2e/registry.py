"""A single private registry and a locally built, digest-pinned CA fixture."""

import base64
import http.client
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import tarfile
import tempfile
import time
from urllib.parse import urlencode, urlsplit

import common
import isolation
from misc import ident, require, tool


KEY = "ca-registry"
REPO = "probe"
TAG = "run"
SHA = re.compile(r"sha256:[0-9a-f]{64}")
IMAGE_SIZE = 10 * 1024 * 1024
SERVER = '''package main

import (
    "fmt"
    "net/http"
    "time"
)

func ready(w http.ResponseWriter, _ *http.Request) {
    fmt.Println("p90-ready")
    w.Header().Set("Content-Type", "text/plain")
    fmt.Fprintln(w, "p90-ready")
}

func main() {
    server := &http.Server{
        Addr: ":8080", Handler: http.HandlerFunc(ready),
        ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
    }
    if err := server.ListenAndServe(); err != nil {
        panic(err)
    }
}
'''


def note(**fields):
    common.note("artifact-registry", KEY, **fields)


def lease(rec, horizon=90):
    isolation.for_registry(rec["start"], int(time.time()) + horizon)


def preflight():
    require(all(shutil.which(x) is not None for x in ("go", "docker")), "registry build prerequisites unavailable")
    ident(os.environ["CLOUDRU_PROJECT_ID"])


def command(args, *, env, folder, stdin=None, timeout=100):
    run = subprocess.run(args, cwd=folder, env=env, input=stdin, stdout=subprocess.PIPE,
                         stderr=subprocess.DEVNULL, timeout=timeout)
    require(run.returncode == 0 and len(run.stdout) <= 4_000_000, "local image command incomplete")
    return run.stdout


def local_env():
    # Child processes never inherit the cloud key, GitHub token or signed grant.
    allowed = ("PATH", "HOME", "TMPDIR", "GOROOT", "GOCACHE", "XDG_RUNTIME_DIR", "LANG",
               "SSL_CERT_FILE", "SSL_CERT_DIR")
    return {key: os.environ[key] for key in allowed if key in os.environ}


def build(folder, tag):
    root = Path(folder)
    (root / "main.go").write_text(SERVER)
    # An implicit module download or external builder image cannot enter this fixture.
    go_env = {**local_env(), "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0",
              "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "", "GOENV": "off"}
    command(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid=",
             "-o", "server", "main.go"], env=go_env, folder=folder, timeout=180)
    binary = (root / "server").read_bytes()
    require(0 < len(binary) <= IMAGE_SIZE, "local image exceeds size bound")
    # Equivalent to FROM scratch + COPY server: one executable, no registry pull.
    with tarfile.open(root / "rootfs.tar", "w") as out:
        member = tarfile.TarInfo("server")
        member.size, member.mode = len(binary), 0o555
        out.addfile(member, io.BytesIO(binary))
    env = {**local_env(), "DOCKER_CONFIG": str(root)}
    cli = ["docker", "image", "import", "--platform", "linux/amd64",
           "--change", 'ENTRYPOINT ["/server"]', "--change", "USER 65532:65532", "--change", "EXPOSE 8080",
           str(root / "rootfs.tar"), tag]
    command(cli, env=env, folder=folder)
    data = json.loads(command(["docker", "image", "inspect", tag], env=env, folder=folder))
    require(isinstance(data, list) and len(data) == 1, "unexpected local image count")
    img = data[0]
    require(img.get("Architecture") == "amd64" and img.get("Os") == "linux" and SHA.fullmatch(img.get("Id", ""))
            and 0 < img["Size"] <= IMAGE_SIZE and img["Config"].get("Entrypoint") == ["/server"],
            "unexpected local image identity")
    return img["Id"]


def checked_host(rec):
    name = rec["name"]
    require(re.fullmatch(r"p90-[0-9a-f]{20}", name), "invalid owned registry name")
    return name + ".cr.cloud.ru"


def account_card(rec):
    rid = ident(rec["id"])
    out = tool("ar_get", id=rid)
    require(out.get("id") == rid and out.get("name") == rec["name"] and out.get("isPublic") is False
            and out.get("registryType") == "DOCKER" and out.get("tariff") == "BASIC",
            "registry identity, visibility or tariff differs")
    for field in ("projectId", "project_id"):
        if field in out:
            require(out[field] == os.environ["CLOUDRU_PROJECT_ID"], "wrong registry project")
    return out


def scoped_registries():
    rows = tool("ar_list").get("registries")
    require(isinstance(rows, list), "incomplete registry inventory")
    found = {}
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get("name"), str) and row["name"], "invalid registry inventory")
        rid = ident(row.get("id"))
        require(rid not in found, "duplicate registry in inventory")
        found[rid] = row
    return found


def anchor(rec):
    oid = ident(rec.get("operation"))
    for poll in range(20):
        op = tool("ar_operation", operation_id=oid)
        require(op.get("id") == oid and type(op.get("done")) is bool and not op.get("error"), "unknown registry operation")
        resource = op.get("resourceId") or op.get("resource_id")
        if resource and rec.get("id"):
            require(ident(resource) == ident(rec["id"]), "conflicting registry receipts")
        if op["done"]:
            rid = ident(resource or rec.get("id"))
            note(id=rid, operation_confirmed=True)
            return rid
        if poll < 19:
            time.sleep(5)
    raise RuntimeError("registry create did not settle")


def docker_login(folder, host):
    env = {**local_env(), "DOCKER_CONFIG": folder}
    command(["docker", "login", host, "--username", os.environ["CLOUDRU_KEY_ID"], "--password-stdin"],
            env=env, folder=folder, stdin=os.environ["CLOUDRU_SECRET"].encode() + b"\n", timeout=30)
    return env


def catalog_request(host, path, authorization=None):
    connection = http.client.HTTPSConnection(host, timeout=12)
    try:
        headers = {"Accept-Encoding": "identity", "Accept": "application/json"}
        if authorization:
            headers["Authorization"] = authorization
        connection.request("GET", path, headers=headers)
        resp = connection.getresponse()
        raw = resp.read(65537)
        length = resp.getheader("Content-Length")
        require(len(raw) <= 65536 and (length is None or (length.isdecimal() and int(length) == len(raw)))
                and resp.getheader("Content-Range") is None and not resp.getheader("Link")
                and resp.getheader("Content-Encoding", "identity").lower() == "identity",
                "incomplete registry control page")
        body = json.loads(raw, object_pairs_hook=common.unique_keys) if raw and resp.status == 200 else {}
        return resp.status, body, resp.getheader("WWW-Authenticate", "")
    finally:
        connection.close()


def catalog_get(host, path):
    # Registry protocols sometimes challenge for a token. Credentials can go
    # only to the already verified host. Do not follow redirects or arbitrary realms.
    code, body, challenge = catalog_request(host, path)
    plain = (os.environ["CLOUDRU_KEY_ID"] + ":" + os.environ["CLOUDRU_SECRET"]).encode()
    basic = "Basic " + base64.b64encode(plain).decode()
    if code == 200:
        # Some registries offer an anonymous, visibility-filtered catalog.
        # Require an authenticated traversal of this exact private host.
        code, body, _ = catalog_request(host, path, basic)
    elif code == 401:
        if challenge.lower().startswith("basic "):
            code, body, _ = catalog_request(host, path, basic)
        else:
            require(challenge.lower().startswith("bearer "), "unsupported registry challenge")
            tokens = dict(re.findall(r'([a-z]+)="([^"\r\n]+)"', challenge[7:]))
            realm = urlsplit(tokens.get("realm", ""))
            require(realm.scheme == "https" and realm.netloc == host and not realm.fragment and not realm.query
                    and re.fullmatch(r"/[A-Za-z0-9/_-]+", realm.path), "unreviewed registry authentication origin")
            params = {k: tokens[k] for k in ("service", "scope") if k in tokens}
            status, token, _ = catalog_request(host, realm.path + "?" + urlencode(params), basic)
            require(status == 200 and isinstance(token, dict), "registry catalog token denied")
            token = token.get("token") or token.get("access_token")
            require(isinstance(token, str) and token and "\n" not in token and "\r" not in token, "incomplete registry token")
            code, body, _ = catalog_request(host, path, "Bearer " + token)
    require(code == 200 and isinstance(body, dict), "registry inventory unavailable")
    return body


def inventory(rec, pushed):
    host = checked_host(rec)
    catalog = catalog_get(host, "/v2/_catalog?n=2").get("repositories")
    expected = [REPO] if pushed else []
    require(catalog == expected, "unexpected registry repositories")
    if pushed:
        tags = catalog_get(host, "/v2/" + REPO + "/tags/list?n=2")
        require(tags.get("name") == REPO and tags.get("tags") == [TAG], "unowned or unknown registry tags")


def remote_digest(folder, env, ref, config):
    result = json.loads(command(["docker", "manifest", "inspect", "--verbose", ref], env=env, folder=folder, timeout=30))
    require(isinstance(result, dict), "unreviewed multi-platform image")
    desc = result.get("Descriptor", {})
    platform = desc.get("platform", {})
    digest = desc.get("digest", "")
    require(SHA.fullmatch(digest) and platform.get("os") == "linux" and platform.get("architecture") == "amd64",
            "unverified manifest descriptor")
    manifest = json.loads(command(["docker", "manifest", "inspect", ref], env=env, folder=folder, timeout=30))
    require(manifest.get("config", {}).get("digest") == config and isinstance(manifest.get("layers"), list)
            and len(manifest["layers"]) == 1, "remote image is not the local single-layer fixture")
    return digest


def image():
    rec = common.ledger().get(KEY, {})
    ref = rec.get("image", "")
    require(rec.get("state") == "pushed" and not rec.get("delete_started") and SHA.fullmatch(rec.get("manifest", ""))
            and ref == checked_host(rec) + "/" + REPO + "@" + rec["manifest"], "pinned run image unavailable")
    return ref


def prepare():
    preflight()
    require(KEY not in common.ledger(), "previous registry intent exists; use recovery")
    rec = {"name": "p90-" + secrets.token_hex(10), "start": int(time.time())}
    host = checked_host(rec)
    tag = host + "/" + REPO + ":" + TAG
    with tempfile.TemporaryDirectory() as folder:
        config = build(folder, tag)
        require(not any(row["name"] == rec["name"] for row in scoped_registries().values()), "registry name already in use")
        lease(rec, 300)
        note(**rec, config=config, state="create-intent")
        out = tool("ar_create", "--yes", name=rec["name"], registry_type="DOCKER", public="false", tariff="BASIC")
        require(out.get("status") == "creating", "no direct registry create receipt")
        fields = {"operation": ident(out.get("operation_id")), "state": "creating"}
        if "registry_id" in out:
            fields["id"] = ident(out["registry_id"])
        note(**fields)
        rec = common.ledger()[KEY]
        rid = anchor(rec)
        rec = common.ledger()[KEY]
        for poll in range(12):
            status = account_card(rec).get("status")
            require(status in ("ACTIVE", "CREATING"), "unexpected owned registry state")
            if status == "ACTIVE":
                break
            if poll < 11:
                time.sleep(5)
        require(status == "ACTIVE" and scoped_registries().get(rid, {}).get("name") == rec["name"],
                "owned private registry not ready")
        inventory(rec, False)
        env = docker_login(folder, host)
        lease(rec, 200)
        note(state="push-intent")
        command(["docker", "push", tag], env=env, folder=folder, timeout=120)
        digest = remote_digest(folder, env, tag, config)
        ref = host + "/" + REPO + "@" + digest
        local = json.loads(command(["docker", "image", "inspect", tag], env=env, folder=folder))
        require(len(local) == 1 and local[0].get("Id") == config and local[0].get("RepoDigests") == [ref],
                "local RepoDigest differs from remote tag")
        require(remote_digest(folder, env, ref, config) == digest, "pinned manifest differs")
        inventory(rec, True)
        note(state="pushed", manifest=digest, image=ref)
        return image()


def registry_gone(rec):
    rid = ident(rec["id"])
    status, body = common.service("GET", "ar", "/v1/registries/" + rid,
                                  query={"projectId": os.environ["CLOUDRU_PROJECT_ID"]})
    if status == 200:
        require(isinstance(body, dict) and body.get("id") == rid and body.get("name") == rec["name"],
                "unexpected registry card on recovery")
        return False
    allowed = {"registry not found", "registry does not exist", "registry " + rid + " not found"}
    require(status == 404 and isinstance(body, dict) and set(body) <= {"code", "message", "details"}
            and body.get("details") in (None, []) and body.get("code") in (5, "5", "NOT_FOUND")
            and isinstance(body.get("message"), str)
            and body["message"].strip().lower().rstrip(".") in allowed, "registry absence unconfirmed")
    return rid not in scoped_registries()


def cleanup():
    book = common.ledger()
    rec = book.get(KEY)
    if not rec or rec.get("state") == "gone":
        return
    require(rec.get("kind") == "artifact-registry" and rec.get("operation"),
            "registry create has no receipt; do not use a name match")
    require(all(item.get("status") == "gone" and item.get("original_uid_absent") is True for item in book.values()
                if item.get("kind") == "container_app"), "an application still depends on this registry")
    if not rec.get("operation_confirmed"):
        anchor(rec)
        rec = common.ledger()[KEY]
    if registry_gone(rec):
        note(state="gone")
        return
    if not rec.get("delete_started"):
        # The OCI catalog cannot enumerate untagged digests. The independent,
        # uninterrupted writer grant must cover the whole lifetime of this ID,
        # including push and the server's cascade; inventory alone is insufficient.
        require(rec.get("state") in ("creating", "pushed"), "push disposition unknown; retain registry")
        lease(rec, 180)
        account_card(rec)
        pushed = rec["state"] == "pushed"
        inventory(rec, pushed)
        if pushed:
            pin = image()
            host = checked_host(rec)
            with tempfile.TemporaryDirectory() as folder:
                env = docker_login(folder, host)
                tag = host + "/" + REPO + ":" + TAG
                require(remote_digest(folder, env, tag, rec["config"]) == rec["manifest"]
                        and remote_digest(folder, env, pin, rec["config"]) == rec["manifest"],
                        "registry contents changed")
        lease(rec, 180)
        note(delete_started=True)
        tool("ar_delete", "--yes", id=ident(rec["id"]))
    # On an interrupted delete, address reads are the only recovery operations.
    for poll in range(18):
        if registry_gone(rec):
            note(state="gone")
            return
        if poll < 17:
            time.sleep(5)
    raise RuntimeError("registry delete incomplete; private receipt remains open")
