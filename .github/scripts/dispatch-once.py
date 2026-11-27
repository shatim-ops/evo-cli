#!/usr/bin/env python3
"""Allow the earliest dispatch of this workflow, on this ref and process SHA."""
import json
import os
import re
import sys
from urllib import error, parse, request

REPO = "shatim-ops/evo-cli"
PER_PAGE = 100


class Stop(ValueError):
    pass


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def verify(env, fetch):
    sha = env["GITHUB_SHA"]
    ref = env["GITHUB_REF"]
    branch = ref.removeprefix("refs/heads/")
    wf = re.fullmatch(
        re.escape(REPO) + r"/\.github/workflows/([A-Za-z0-9_-]+\.ya?ml)@" + re.escape(ref),
        env["GITHUB_WORKFLOW_REF"],
    )
    if (env["GITHUB_REPOSITORY"] != REPO or env["GITHUB_EVENT_NAME"] != "workflow_dispatch"
            or env["GITHUB_RUN_ATTEMPT"] != "1" or not wf
            or not re.fullmatch(r"refs/heads/claude/live-runs-[A-Za-z0-9._-]+", ref)
            or not re.fullmatch("[0-9a-f]{40}", sha) or env["GITHUB_WORKFLOW_SHA"] != sha):
        raise Stop()
    run_id = int(env["GITHUB_RUN_ID"])
    run_number = int(env["GITHUB_RUN_NUMBER"])
    if min(run_id, run_number) < 1:
        raise Stop()

    total = None
    seen = {}
    numbers = set()
    for page in range(1, 11):
        query = parse.urlencode(dict(branch=branch, head_sha=sha, event="workflow_dispatch",
                                     per_page=PER_PAGE, page=page))
        url = f"https://api.github.com/repos/{REPO}/actions/workflows/{wf[1]}/runs?{query}"
        result = fetch(url)
        count = result["total_count"]
        rows = result["workflow_runs"]
        # GitHub search returns at most 1,000 matches. At the boundary a
        # capped count cannot establish that the earliest dispatch is visible.
        if (type(count) is not int or not 1 <= count < 1000 or not isinstance(rows, list)
                or (total is not None and total != count)
                or len(rows) != min(PER_PAGE, count - len(seen))):
            raise Stop()
        total = count
        for row in rows:
            number, rid = row["run_number"], row["id"]
            if (type(number) is not int or type(rid) is not int or min(number, rid) < 1
                    or number in numbers or rid in seen or row["event"] != "workflow_dispatch"
                    or row["head_branch"] != branch or row["head_sha"] != sha
                    or row["head_repository"]["full_name"] != REPO
                    or row["path"].split("@", 1)[0] != ".github/workflows/" + wf[1]):
                raise Stop()
            seen[rid] = number
            numbers.add(number)
        if len(seen) == count:
            # A later queued dispatch does not cancel the first run. Any
            # earlier dispatch, even cancelled or failed, consumes the slot.
            if seen.get(run_id) != run_number or min(numbers) != run_number:
                raise Stop()
            return
    raise Stop()


def main():
    token = os.environ["GH_TOKEN"]
    if not token:
        raise Stop()
    client = request.build_opener(NoRedirect())
    def fetch(url):
        headers = {"Accept": "application/vnd.github+json", "Authorization": "Bearer " + token,
                   "X-GitHub-Api-Version": "2022-11-28", "User-Agent": "evo-release-dispatch-check"}
        with client.open(request.Request(url, headers=headers), timeout=12) as response:
            if response.status != 200:
                raise Stop()
            data = response.read(8 * 1024 * 1024 + 1)
            if len(data) > 8 * 1024 * 1024:
                raise Stop()
            return json.loads(data)
    verify(os.environ, fetch)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, TypeError, KeyError, OSError, error.HTTPError):
        sys.exit("Первый запуск на этой ветке и SHA задания не подтверждён.")
    print("Подтверждён первый ручной запуск на этой ветке и SHA задания.")
