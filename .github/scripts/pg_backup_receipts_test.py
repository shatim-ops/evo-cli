"""Offline checks for per-backup readback in the standard PostgreSQL job."""
import contextlib
import io
import json
import os
import runpy
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

module = runpy.run_path(str(Path(__file__).with_name("pg-backup-receipts.py")))
outcome, record = module["outcome"], module["record"]
BACKUP = {"id": "backup-id", "name": "first"}


class BackupReceipts(unittest.TestCase):
    def test_separate_receipts_and_digest_only_in_output(self):
        import hashlib
        book = {"backups": []}
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            record(book, {"backups": None}, "parent", False)
            self.assertEqual(book["backups"], [])
            record(book, {"backups": [BACKUP]}, "parent", False)
            book.update(manual_intent=True, manual_id="second-id")
            second = {"id": "second-id", "name": module["MANUAL"]}
            record(book, {"backups": [BACKUP, second]}, "parent", True)
        record(book, {"backups": None}, "parent", False)
        self.assertEqual(book["backups"], [BACKUP, second])
        for item in book["backups"]:
            self.assertIn(hashlib.sha256(item["id"].encode()).hexdigest(), out.getvalue())
            self.assertNotIn(item["id"], out.getvalue())
        with self.assertRaises(ValueError):
            record(book, {"backups": [dict(second, id="other")]}, "parent", True)

    def test_interrupted_after_create_keeps_addressed_readback_without_inventory(self):
        script = str(Path(__file__).with_name("pg-backup-receipts.py"))
        with tempfile.TemporaryDirectory() as folder:
            env = dict(os.environ, CLUSTER_ID="parent", RUNNER_TEMP=folder)
            def call(*args):
                return subprocess.run([sys.executable, script, *args], env=env, capture_output=True, text=True, check=True)
            reply = Path(folder, "out")
            reply.write_text('{"backups": null}')
            call("record", str(reply))
            call("intend-manual")
            reply.write_text('{"status":"backing_up","operation_id":"op","backup_id":"returned-id"}')
            call("created-manual", str(reply))
            targets = call("paths")  # fresh process, no operation poll and no further inventory
            self.assertEqual(targets.stdout.splitlines(), ["1 " + module["MANUAL"]])
            self.assertFalse(targets.stderr)
            book = json.loads(Path(folder, "pg-backup-receipts.json").read_bytes())
            self.assertEqual(book["backups"], [{"id":"returned-id", "name":module["MANUAL"]}])
            self.assertEqual(book["manual_operation"], "op")

    def test_absence_requires_the_exact_backup(self):
        self.assertEqual(outcome(404, {"code": 5, "message": 'backup "first" not found'}, "parent", BACKUP), "отсутствует")
        for code, body in [(404, {"code": 5, "message": "cluster not found"}),
                           (404, {"code": 5, "message": "route not found"}),
                           (404, {"code": 5, "message": 'backup "other" not found'}),
                           (400, None), (200, dict(BACKUP, id="different"))]:
            self.assertEqual(outcome(code, body, "parent", BACKUP), "неизвестно")
        self.assertEqual(outcome(200, BACKUP, "parent", BACKUP), "осталась")
        self.assertEqual(outcome(200, {"id": "parent", "name": "first"}, "parent", {"id": "parent", "name": "first"}), "неизвестно")

    def test_cluster_id_in_create_reply_is_not_treated_as_backup_id(self):
        script = str(Path(__file__).with_name("pg-backup-receipts.py"))
        with tempfile.TemporaryDirectory() as folder:
            env = dict(os.environ, CLUSTER_ID="cluster-uuid", RUNNER_TEMP=folder)
            def call(*args, check=True):
                return subprocess.run([sys.executable, script, *args], env=env, capture_output=True, text=True, check=check)
            reply = Path(folder, "out")
            reply.write_text(json.dumps({"backups": [BACKUP]}))
            call("record", str(reply))
            call("intend-manual")
            # Как в прогоне №255: pg_backup_create вернул op.ResourceID == cluster_id в backup_id.
            reply.write_text(json.dumps({"status": "backing_up", "operation_id": "op-uuid", "backup_id": "cluster-uuid"}))
            res = call("created-manual", str(reply))
            self.assertIn("backup_id в ответе совпадает с cluster_id", res.stdout)
            self.assertNotIn("cluster-uuid", res.stdout)
            book = json.loads(Path(folder, "pg-backup-receipts.json").read_bytes())
            self.assertEqual(book["backups"], [BACKUP])
            self.assertIsNone(book["manual_id"])
            # После завершения операции pg_backups возвращает реальный ID ручной копии.
            manual = {"id": "real-manual-backup-id", "name": module["MANUAL"]}
            reply.write_text(json.dumps({"backups": [BACKUP, manual]}))
            call("record", str(reply), "--expect-manual")
            # Повторный record (шаг перед удалением) проходит и фиксирует реальный ID.
            call("record", str(reply))
            book = json.loads(Path(folder, "pg-backup-receipts.json").read_bytes())
            self.assertEqual(book["manual_id"], "real-manual-backup-id")
            self.assertEqual(book["backups"], [BACKUP, manual])
            # Подмена ID после фиксации обнаруживается с явным названием условия.
            reply.write_text(json.dumps({"backups": [BACKUP, dict(manual, id="substituted-id")]}))
            bad = call("record", str(reply), "--expect-manual", check=False)
            self.assertNotEqual(bad.returncode, 0)
            self.assertIn("подмена ID", bad.stderr)
            self.assertNotIn("substituted-id", bad.stderr)

    def test_id_substitution_and_condition_diagnostics(self):
        script = str(Path(__file__).with_name("pg-backup-receipts.py"))
        with tempfile.TemporaryDirectory() as folder:
            env = dict(os.environ, CLUSTER_ID="cluster-uuid", RUNNER_TEMP=folder)
            def call(*args):
                return subprocess.run([sys.executable, script, *args], env=env, capture_output=True, text=True)
            reply = Path(folder, "out")
            # 1) backup.id == cluster_id в списке pg_backups
            reply.write_text(json.dumps({"backups": [{"id": "cluster-uuid", "name": "auto"}]}))
            r = call("record", str(reply))
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("совпадает с cluster_id", r.stderr)
            # 2) Валидный базовый список + intend-manual
            reply.write_text(json.dumps({"backups": [BACKUP]}))
            self.assertEqual(call("record", str(reply)).returncode, 0)
            self.assertEqual(call("intend-manual").returncode, 0)
            # 3) operation_id == cluster_id в created-manual
            reply.write_text(json.dumps({"status": "backing_up", "operation_id": "cluster-uuid"}))
            r = call("created-manual", str(reply))
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("operation_id совпадает с cluster_id", r.stderr)
            # 4) backup_id == operation_id в created-manual
            reply.write_text(json.dumps({"status": "backing_up", "operation_id": "op-uuid", "backup_id": "op-uuid"}))
            r = call("created-manual", str(reply))
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("backup_id совпадает с operation_id", r.stderr)
            # 5) backup.id == operation_id в списке pg_backups
            reply.write_text(json.dumps({"status": "backing_up", "operation_id": "op-uuid"}))
            self.assertEqual(call("created-manual", str(reply)).returncode, 0)
            reply.write_text(json.dumps({"backups": [BACKUP, {"id": "op-uuid", "name": module["MANUAL"]}]}))
            r = call("record", str(reply), "--expect-manual")
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("совпадает с operation_id", r.stderr)
            # 6) cluster_id копии не совпадает с кластером прогона
            reply.write_text(json.dumps({"backups": [BACKUP, {"id": "real-id", "name": module["MANUAL"], "cluster_id": "other-cluster"}]}))
            r = call("record", str(reply), "--expect-manual")
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("подмена кластера", r.stderr)
            # 7) ручная копия отсутствует при --expect-manual
            reply.write_text(json.dumps({"backups": [BACKUP]}))
            r = call("record", str(reply), "--expect-manual")
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("ручная копия отсутствует", r.stderr)


if __name__ == "__main__":
    unittest.main()
