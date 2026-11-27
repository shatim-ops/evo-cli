"""Bind a manual job to its reviewed SHA and reject duplicate paid runs."""
import json
import os
import re
import subprocess
from urllib.parse import urlencode

from common import CANDIDATE

BRANCH = "claude/live-runs-191-p90-d7645da5"
REPO = "shatim-ops/evo-cli"
WORKFLOW = ".github/workflows/cloud-e2e.yml"
PREVIOUS_LAUNCHER = "32f9a4279dab8184679f42e3abbfc7c5de8adf7d"


def api(path):
    return json.loads(subprocess.check_output(["gh", "api", "repos/" + REPO + "/" + path]))


def validate_source(workflow):
    for commit, expected_parent in ((workflow, PREVIOUS_LAUNCHER), (PREVIOUS_LAUNCHER, CANDIDATE)):
        parents = subprocess.check_output(["git", "show", "-s", "--format=%P", commit], text=True).strip()
        if parents != expected_parent:
            raise ValueError("unexpected launcher ancestry")
    paths = subprocess.check_output(["git", "diff", "--name-only", "--no-renames", CANDIDATE, workflow], text=True).splitlines()
    if WORKFLOW not in paths or any(
        p != WORKFLOW and not p.startswith(".github/scripts/patch-e2e/") for p in paths
    ):
        raise ValueError("unexpected changes outside the launcher's scope")


def validate():
    e = os.environ
    workflow = e["GITHUB_SHA"]
    run_id = e["GITHUB_RUN_ID"]
    if (e["GITHUB_EVENT_NAME"] != "workflow_dispatch" or e["GITHUB_REPOSITORY"] != REPO
            or e["GITHUB_REF"] != "refs/heads/" + BRANCH or e["GITHUB_RUN_ATTEMPT"] != "1"
            or workflow != e["REVIEWED"] or workflow != e["GITHUB_WORKFLOW_SHA"]
            or not re.fullmatch(r"[0-9a-f]{40}", workflow)):
        raise ValueError("unreviewed ref or repeat attempt")
    validate_source(workflow)
    mode, source = e["P90_MODE"], e["ORIGINAL_RUN"]
    if mode == "recover":
        if not re.fullmatch(r"[1-9][0-9]{0,19}", source) or source == run_id:
            raise ValueError("supply the original finished full run")
        old = api("actions/runs/" + source)
        if not (old.get("status") == "completed" and old.get("event") == "workflow_dispatch"
                and old.get("run_attempt") == 1 and old.get("head_sha") == workflow
                and old.get("head_branch") == BRANCH and old.get("path") == WORKFLOW
                and old.get("display_title", "").startswith("P90 full ")):
            raise ValueError("recovery source differs")
        return source
    if mode != "full" or source:
        raise ValueError("invalid mode or original run")
    pull = api("pulls/2725")
    if pull.get("state") != "open" or pull.get("head", {}).get("sha") != CANDIDATE:
        raise ValueError("the PR is no longer the reviewed candidate")
    query = urlencode({"branch": BRANCH, "event": "workflow_dispatch", "per_page": 100})
    runs = api("actions/workflows/cloud-e2e.yml/runs?" + query)
    if runs["total_count"] != len(runs["workflow_runs"]):
        raise ValueError("incomplete run history")
    full_runs = [str(r["id"]) for r in runs["workflow_runs"] if r["display_title"].startswith("P90 full ")]
    if full_runs != [run_id]:
        raise ValueError("duplicate or unconfirmed full dispatch")
    return run_id


if __name__ == "__main__":
    source = validate()
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="ascii") as out:
        print("source_run=" + source, file=out)
