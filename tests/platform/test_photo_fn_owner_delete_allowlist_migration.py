from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / "backend/db/migrations/00194_photo_fn_owner_delete_allowlist.sql"
LATEST = ROOT / "backend/db/migrations/00195_contact_sheet_resources.sql"


def grants(sql: str) -> set[str]:
    result=set()
    for body in re.findall(r"GRANT DELETE ON(?: TABLE)?\s+(.+?)\s+TO photo_fn_owner",sql,re.S):
        result.update(re.findall(r"photo\.([a-z0-9_]+)",body))
    return result


def test_delete_is_revoked_globally_then_regranted_only_to_current_purge_allowlist():
    base,latest=BASE.read_text(),LATEST.read_text()
    assert "REVOKE DELETE ON ALL TABLES IN SCHEMA photo FROM photo_fn_owner" in base
    allowed=grants(base)|grants(latest)|{"audit_log"}
    purge_array=latest.split("tables_to_purge constant text[]:=ARRAY[",1)[1].split("];",1)[0]
    purged=set(re.findall(r"'([a-z0-9_]+)'",purge_array))|{"audit_log"}
    assert allowed==purged
    assert "tenant_static_resources" in allowed
    assert not {"deletion_jobs","memberships","tenants"}&allowed


def test_delete_allowlist_migrations_are_forward_only():
    for migration in (BASE,LATEST):
        sql=migration.read_text();assert "-- +goose Down" in sql;assert "RAISE EXCEPTION 'Forward-only migration'" in sql
