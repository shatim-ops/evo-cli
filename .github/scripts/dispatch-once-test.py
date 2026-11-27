#!/usr/bin/env python3
"""Offline checks for the one-dispatch guard."""
import importlib.util
from pathlib import Path
import unittest
from urllib.parse import parse_qs, urlsplit

spec = importlib.util.spec_from_file_location("dispatch_once", Path(__file__).with_name("dispatch-once.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)
SHA = "a" * 40
REF = "refs/heads/claude/live-runs-a1-fixture"
BRANCH = REF.removeprefix("refs/heads/")
ENV = dict(GITHUB_REPOSITORY=guard.REPO, GITHUB_EVENT_NAME="workflow_dispatch", GITHUB_REF=REF,
           GITHUB_RUN_ID="400", GITHUB_RUN_NUMBER="10", GITHUB_RUN_ATTEMPT="1", GITHUB_SHA=SHA,
           GITHUB_WORKFLOW_SHA=SHA, GITHUB_WORKFLOW_REF=f"{guard.REPO}/.github/workflows/cloud-e2e.yml@{REF}")


def row(n):
    return dict(id=n + 390, run_number=n, path=".github/workflows/cloud-e2e.yml", head_branch=BRANCH,
                head_repository={"full_name": guard.REPO}, event="workflow_dispatch", head_sha=SHA)


class DispatchTest(unittest.TestCase):
    def verify(self, pages, **override):
        observed = []
        def fetch(url):
            parsed = urlsplit(url)
            self.assertEqual(parsed.hostname, "api.github.com")
            self.assertEqual(parsed.path, "/repos/shatim-ops/evo-cli/actions/workflows/cloud-e2e.yml/runs")
            q = parse_qs(parsed.query)
            self.assertEqual((q["head_sha"], q["branch"], q["event"]), ([SHA], [BRANCH], ["workflow_dispatch"]))
            page = int(q["page"][0])
            self.assertEqual(page, len(observed) + 1)
            observed.append(page)
            n, rows = pages[page - 1]
            return {"total_count": n, "workflow_runs": rows}
        guard.verify(ENV | override, fetch)
        self.assertEqual(len(observed), len(pages))

    def test_first_and_later_queued(self):
        self.verify([(1, [row(10)])])
        rows = [row(n) for n in range(110, 9, -1)]
        self.verify([(101, rows[:100]), (101, rows[100:])])

    def test_second_dispatch_or_earlier_on_later_page(self):
        rows = [row(n) for n in range(109, 8, -1)]
        for pages in [[(2, [row(10), row(9)])], [(101, rows[:100]), (101, rows[100:])]]:
            with self.assertRaises(guard.Stop):
                self.verify(pages)

    def test_incomplete_or_ambiguous(self):
        rows = [row(n) for n in range(110, 10, -1)]
        for pages in [[(0, [])], [(1, [row(11)])], [(1001, [])], [(101, rows), (102, [row(10)])],
                      [(101, rows), (101, [])], [(101, rows), (101, [rows[0]])],
                      [(2, [row(10), row(10)])], [(1, [row(10) | {"head_sha": "b" * 40}])],
                      [(1, [row(10) | {"path": ".github/workflows/other.yml"}])]]:
            with self.assertRaises(guard.Stop):
                self.verify(pages)

    def test_saturated_search_limit_refused(self):
        rows = [row(n) for n in range(1009, 9, -1)]
        with self.assertRaises(guard.Stop):
            self.verify([(1000, rows[i:i+100]) for i in range(0, 1000, 100)])

    def test_mismatched_context(self):
        for override in [dict(GITHUB_WORKFLOW_SHA="b"*40), dict(GITHUB_RUN_NUMBER="11"),
                         dict(GITHUB_RUN_ATTEMPT="2"), dict(GITHUB_REF="refs/heads/main")]:
            with self.assertRaises(guard.Stop):
                self.verify([(1, [row(10)])], **override)


if __name__ == "__main__":
    unittest.main()
