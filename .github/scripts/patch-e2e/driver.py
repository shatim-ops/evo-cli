"""Entry points for the pinned patch workflow, including cold-runner recovery."""
import json
import os
from pathlib import Path
import sys

import common


def selected_scenario():
    import apps
    import core
    import pg
    import reads
    import misc
    import cluster
    # Disk and network exercise the second live VM while both receipts still exist.
    modules = {"reads": reads, "apps": apps, "core": core, "misc": misc, "cluster": cluster, "pg": pg}
    name = os.environ.get("P90_SUITE")
    if name not in modules:
        raise RuntimeError("missing or unauthorized suite")
    return name, modules[name]


def public(line):
    print(line, flush=True)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as out:
            print(line, file=out)


def mark(name, outcome):
    common.note("phase", "phase:" + name, state=outcome)


def begin():
    common.ReceiptBook().start(recovery=os.environ["P90_MODE"] == "recover")


def preflight():
    name, module = selected_scenario()
    if name == "core":
        import network
        network.preflight()
    if name == "cluster":
        import cluster_objects
        cluster_objects.preflight_addon()
    if name != "reads":
        module.preflight()
    mark("preflight", "passed")


def cleanup_one(name, module):
    mark(name + "-cleanup", "started")
    module.cleanup()
    if name == "apps":
        import registry
        registry.cleanup()
    mark(name + "-cleanup", "checked")


def full():
    if common.ledger().get("phase:preflight", {}).get("state") != "passed":
        raise RuntimeError("full preflight evidence missing")
    name, module = selected_scenario()
    mark(name, "started")
    module.run()
    mark(name, "exercised")
    if name != "reads":
        cleanup_one(name, module)
    public(name + ": checks completed")


def cleanup():
    name, module = selected_scenario()
    book = common.ledger()
    if name == "reads" or "phase:" + name not in book:
        public(name + ": no mutating scenario was entered")
        return
    if book.get("phase:" + name + "-cleanup", {}).get("state") != "checked":
        cleanup_one(name, module)
    public(name + ": cleanup checked against its signed resource receipts")


def main(argv):
    if argv == ["checkpoint"]:
        data = json.load(sys.stdin)
        common.note(data["kind"], data["key"], **data["fields"])
    elif argv == ["ledger"]:
        json.dump(common.ledger(), sys.stdout)
    else:
        action = {"begin": begin, "preflight": preflight, "full": full, "cleanup": cleanup}.get(argv[0] if len(argv) == 1 else "")
        if action is None:
            raise RuntimeError("unknown workflow mode")
        private = Path(os.environ["RUNNER_TEMP"]) / "p90-work"
        private.mkdir(mode=0o700, exist_ok=True)
        os.chdir(private)
        action()


if __name__ == "__main__":
    os.umask(0o077)
    try:
        main(sys.argv[1:])
    except Exception as error:
        # Exception text from libraries or services may contain private paths, IDs or credentials.
        # Only fixed local categories and an HTTP code are safe for public summaries.
        import re
        text = str(error)
        iam = re.fullmatch(r"IAM exchange failed, HTTP ([1-5][0-9]{2})", text)
        known = {"IAM response missing token", "private receipt repository unconfirmed",
                 "receipt branch unavailable", "incomplete response", "duplicate key in response",
                 "partial HTTP response", "durable receipt write not confirmed"}
        reason = "IAM HTTP " + iam[1] if iam else (text if text in known else "consult the private per-ID receipt")
        public("P90 " + sys.argv[1] + " incomplete: " + reason)
        sys.exit(1)
