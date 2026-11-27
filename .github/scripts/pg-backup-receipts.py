"""Keep backup IDs and names local to this run. Public output contains only ordinals."""
import hashlib
import json
import os
from pathlib import Path
import sys
from urllib.parse import quote


MANUAL = "evo-e2e-backup"


class ReceiptError(ValueError):
    """Safe, ID-free description of the violated receipt invariant."""


def valid_token(value):
    return (isinstance(value, str) and bool(value) and len(value) <= 256
            and all(ord(c) >= 32 and ord(c) != 127 for c in value))


def identity(item):
    return (isinstance(item, dict) and item.get("name") not in (".", "..")
            and all(valid_token(item.get(k)) for k in ("id", "name")))


def record(book, result, cid, require_manual):
    if not isinstance(result, dict) or "backups" not in result:
        raise ReceiptError("неполный список копий: отсутствует поле backups")
    backups = result["backups"]
    # pg_backups serializes the candidate's nil Go slice as null on a
    # successful empty read. This only records observed identities; it cannot
    # establish any earlier backup's disappearance.
    if backups is None:
        backups = []
    if not isinstance(backups, list):
        raise ReceiptError("неполный список копий: поле backups не массив")
    receipts = book["backups"]
    known = {r["id"]: r["name"] for r in receipts}
    names = {r["name"]: r["id"] for r in receipts}
    manual_op = book.get("manual_operation")
    seen = set()
    for item in backups:
        if not identity(item):
            raise ReceiptError("некорректные поля id/name резервной копии")
        if item.get("cluster_id", cid) != cid:
            raise ReceiptError("подмена кластера: cluster_id копии не совпадает с кластером прогона")
        rid, name = item["id"], item["name"]
        if rid == cid:
            raise ReceiptError("подмена ID: id резервной копии совпадает с cluster_id")
        if manual_op and rid == manual_op:
            raise ReceiptError("подмена ID: id резервной копии совпадает с operation_id")
        if rid in seen:
            raise ReceiptError("нарушено условие: дублирующийся backup.id в списке pg_backups")
        if rid in known and known[rid] != name:
            raise ReceiptError("подмена имени: для известного backup.id изменилось имя копии")
        if name in names and names[name] != rid:
            raise ReceiptError("подмена ID: для известного имени копии пришёл другой backup.id")
        if name == MANUAL:
            if not book.get("manual_intent"):
                raise ReceiptError("нарушено условие: имя ручной копии уже занято до intend-manual")
            if book.get("manual_id") not in (None, rid):
                raise ReceiptError("подмена ID: id ручной копии не совпадает с сохранённым manual_id")
            if book.get("manual_id") is None:
                book["manual_id"] = rid
        if rid not in known:
            receipts.append({"id": rid, "name": name})
            known[rid], names[name] = name, rid
            digest = hashlib.sha256(rid.encode()).hexdigest()
            print(f"PostgreSQL backup №{len(receipts)}, ID SHA-256 {digest}")
        seen.add(rid)
    if require_manual and names.get(MANUAL) not in seen:
        raise ReceiptError("нарушено условие: ручная копия отсутствует в списке pg_backups")


def outcome(code, body, cid, backup):
    if not isinstance(body, dict) or not identity(backup) or backup["id"] == cid:
        return "неизвестно"
    if code == 200 and (body.get("id"), body.get("name"), body.get("cluster_id", cid)) == (backup["id"], backup["name"], cid):
        return "осталась"
    if code != 404 or body.get("code") not in (5, "5", "NOT_FOUND"):
        return "неизвестно"
    message = body.get("message")
    if not isinstance(message, str):
        return "неизвестно"
    named = any(message == f'backup "{item}" not found' for item in (backup["name"], backup["id"]))
    details = body.get("details", [])
    if not isinstance(details, list) or not all(isinstance(d, dict) for d in details):
        return "неизвестно"
    resources = [d for d in details if d.get("@type") == "type.googleapis.com/google.rpc.ResourceInfo"]
    if resources:
        path = "clusters/" + cid + "/backups/" + backup["name"]
        exact = all(d.get("resourceName") == path and d.get("resourceType") in
                    ("Backup", "backup", "postgresql/Backup", "postgresql.api.cloud.ru/Backup") for d in resources)
        named = exact and (named or message.lower() in ("backup not found", "postgresql backup not found"))
    return "отсутствует" if named else "неизвестно"


def main():
    cmd = sys.argv[1]
    path = Path(os.environ["RUNNER_TEMP"]) / "pg-backup-receipts.json"
    cid = os.environ["CLUSTER_ID"]
    book = json.loads(path.read_bytes()) if path.exists() else {"cluster": cid, "backups": []}
    if book["cluster"] != cid:
        raise ReceiptError("подмена кластера: cluster в квитанции не совпадает с CLUSTER_ID")
    os.umask(0o077)
    if cmd in ("record", "intend-manual", "created-manual"):
        if cmd == "record":
            record(book, json.loads(Path(sys.argv[2]).read_bytes()), cid, "--expect-manual" in sys.argv)
        elif cmd == "created-manual":
            reply = json.loads(Path(sys.argv[2]).read_bytes())
            if not isinstance(reply, dict):
                raise ReceiptError("ответ pg_backup_create не JSON-объект")
            if not book.get("manual_intent"):
                raise ReceiptError("нарушено условие: created-manual вызван без intend-manual")
            if reply.get("status") != "backing_up":
                raise ReceiptError("нарушено условие: статус ответа pg_backup_create не backing_up")
            op_id = reply.get("operation_id")
            if not valid_token(op_id):
                raise ReceiptError("нарушено условие: отсутствует корректный operation_id")
            if op_id == cid:
                raise ReceiptError("подмена ID: operation_id совпадает с cluster_id")
            book["manual_operation"] = op_id
            bid = reply.get("backup_id")
            if bid is None or bid == "":
                book["manual_id"] = None
            elif bid == op_id:
                raise ReceiptError("подмена ID: backup_id совпадает с operation_id")
            elif bid == cid:
                # В Cloud.ru PostgreSQL операция создания копии отдаёт в resource_id
                # ID кластера, а не ID копии; не записываем cluster_id как backup.id.
                book["manual_id"] = None
                book["manual_create_id_is_cluster"] = True
                print("pg_backup_create: backup_id в ответе совпадает с cluster_id (resource_id операции); ID ручной копии будет установлен по списку pg_backups")
            else:
                if not valid_token(bid):
                    raise ReceiptError("некорректное значение backup_id в ответе pg_backup_create")
                book["manual_id"] = bid
                # The POST identifies the target before the operation completes.
                # Keep it for readback even if the runner never sees another list.
                row = {"id": bid, "name": MANUAL}
                record(book, {"backups": [row]}, cid, False)
        else:
            if not path.exists():
                raise ReceiptError("нарушено условие: отсутствует базовый список копий до intend-manual")
            if any(r["name"] == MANUAL for r in book["backups"]):
                raise ReceiptError("нарушено условие: имя ручной копии уже есть в базовом списке")
            book["manual_intent"] = True
        tmp = path.with_suffix(".tmp")
        tmp.write_text(json.dumps(book))
        os.replace(tmp, path)
    elif cmd == "paths":
        if not path.exists():
            print("::warning::DB-22: состав резервных копий неизвестен", file=sys.stderr)
        for i, row in enumerate(book["backups"], 1):
            print(i, quote(row["name"], safe=""))
        if book.get("manual_intent") and all(r["name"] != MANUAL for r in book["backups"]):
            print("::warning::DB-22: судьба ручной копии неизвестна, её ID не подтверждён", file=sys.stderr)
    elif cmd == "result":
        n, status, filename, inventory = sys.argv[2:]
        b = book["backups"][int(n) - 1]
        try:
            body = json.loads(Path(filename).read_bytes())
        except (ValueError, OSError):
            body = None
        verdict = outcome(int(status), body, cid, b)
        try:
            listed = json.loads(Path(inventory).read_bytes())["backups"]
        except (ValueError, TypeError, KeyError, OSError):
            listed = None
        if isinstance(listed, list) and any(isinstance(r, dict) and r.get("id") == b["id"] and
                r.get("name") == b["name"] and r.get("cluster_id", cid) == cid for r in listed):
            verdict = "ответы списка и карточки расходятся, исход неизвестен" if verdict == "отсутствует" else "осталась"
        kind = "ручная" if b["name"] == MANUAL else "из списка кластера"
        print(f"DB-22: копия №{n} ({kind}), HTTP {status}, {verdict}")
    else:
        raise ReceiptError("неизвестная команда квитанции")


if __name__ == "__main__":
    try:
        main()
    except ReceiptError as exc:
        sys.exit(f"некорректная или неполная квитанция PostgreSQL: {exc}")
    except (OSError, ValueError, TypeError, KeyError, AttributeError, IndexError) as exc:
        sys.exit(f"некорректная или неполная квитанция PostgreSQL: ошибка чтения/разбора ({type(exc).__name__})")
