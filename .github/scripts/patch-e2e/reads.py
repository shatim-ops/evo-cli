"""Project-scoped CLI reads for the 1.9.1 patch; keep raw responses out of evidence."""

import os
import re


# These are project inventory reads only. Do not follow IDs returned here to
# another API. dns zones reads the public and private zone collections itself.
INVENTORY = (
    (("vm", "list"), "vms"),
    (("disk", "list"), "disks"),
    (("backup", "list"), "backups"),
    (("image", "list", "--type", "private"), "images"),
    (("vpc", "list"), "vpcs"),
    (("vpc", "subnets"), "subnets"),
    (("vpc", "security-groups"), "security_groups"),
    (("vpc", "fips"), "floating_ips"),
    (("snat", "list"), "nat_gateways"),
    (("ar", "list"), "registries"),
    (("cert", "list"), "certificates"),
    (("dns", "zones"), "zones"),
    (("k8s", "list"), "clusters"),
    (("lb", "list"), "load_balancers"),
    (("tg", "list"), "target_groups"),
    (("secret", "list"), "secrets"),  # Metadata only, never fetch the value.
)


def require(ok):
    if not ok:
        raise ValueError("Unexpected project or read response")


def row_count(out, field):
    items = out.get(field)
    # Cert Manager permits an omitted repeated field on the wire. The CLI
    # serializes its nil slice as null and explicitly reports count zero.
    if field == "certificates" and field in out and items is None and out.get("count") == 0:
        items = []
    require(isinstance(items, list) and all(isinstance(item, dict) for item in items))
    if "count" in out:
        require(type(out["count"]) is int and len(items) == out["count"])
    return len(items)


def run():
    import common

    project = os.environ.get("CLOUDRU_PROJECT_ID", "").strip()
    run_id = os.environ.get("GITHUB_RUN_ID", "")
    attempt = os.environ.get("GITHUB_RUN_ATTEMPT", "")
    require(bool(project) and bool(re.fullmatch(r"[0-9]+", run_id)) and bool(re.fullmatch(r"[0-9]+", attempt)))
    metric = f"evo_e2e_p90_absent_{run_id}_{attempt}"

    def read(*args):
        out = common.call_evo(*args, "--json", timeout=50)
        require(isinstance(out, dict))
        return out

    def record(key, evidence, **fields):
        common.note("read", key, evidence=evidence, **fields)

    # Use a named series; the other services in the project are outside this run.
    out = read("metrics", "history", metric, "--since", "1m", "--project-id", project)
    require(out.get("project") == project and out.get("match") == metric)
    require(row_count(out, "series") == 0)
    record("metrics-history", "live-empty-selector", rows=0)

    for args, field in INVENTORY:
        out = read(*args, "--project-id", project)
        if field == "certificates":
            require(out.get("project") == project)
        record("-".join(args[:2]), "live-list", rows=row_count(out, field))

    # Exercise the real, pre-consent name/shape checks. No caller-controlled
    # flag here can enable Execute. Use only synthetic non-secret input.
    name = f"p90-{run_id}-{attempt}"
    previews = {
        "ar_create": {"name": name},
        "vpc_create": {"name": name},
        "secret_create": {"path": name, "value": "synthetic-placeholder"},
        "k8s_create": {"name": name, "zone": "ru.AZ-2", "kube_api_internet": "false"},
    }
    for tool, params in previews.items():
        argv = ["--dry-run", "--project-id", project, "tools", "call", tool]
        for key, value in params.items():
            argv.extend(("--arg", f"{key}={value}"))
        out = read(*argv)
        require(out.get("status") == "dry_run" and isinstance(out.get("preview"), dict)
                and out["preview"].get("action") == tool)
        record(tool, "live-lookup-dry-run")

    out = read("iam", "service-accounts", "--project-id", project)
    record("iam-service-accounts", "live-project-list", rows=row_count(out, "service_accounts"))

    # These aggregate commands cannot exclude services in the request. Do not
    # fetch and filter afterwards: even the raw responses would be out of scope.
    for key in ("audit-events", "billing-consumption", "org-quotas"):
        record(key, "not-exercised-no-upstream-service-filter")

    # --check would visit live entries of any historical tool; other service
    # exclusions cannot be enforced there. Check the CLI's local journal only.
    out = read("history", "--project-id", project)
    lines = row_count(out, "lines")
    live = row_count(out, "live")
    require("checked" not in out)
    record("history", "local-journal", lines=lines, live_rows=live)
