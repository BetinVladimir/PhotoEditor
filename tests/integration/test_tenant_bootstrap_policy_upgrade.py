"""Actual auth-role registration and lossless policy upgrade, isolated DB only."""
from pathlib import Path
import os
import subprocess
from uuid import uuid4

import pytest
import psycopg
from psycopg.types.json import Jsonb
from psycopg.conninfo import make_conninfo

ROOT = Path(__file__).resolve().parents[2]


def bootstrap(db):
    email = f"{uuid4()}@test.example"
    with db.transaction():
        db.execute("SET LOCAL ROLE photo_auth")
        actor, epoch = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)", (email,)).fetchone()
        db.execute("RESET ROLE")
    return db.execute("SELECT id,quotas,retention,revision FROM photo.tenants WHERE owner_user_id=%s", (actor,)).fetchone()


@pytest.mark.migration_head(2574)
def test_bootstrap_upgrade_preserves_custom_policies_and_auth_function_identity(db):
    legacy = bootstrap(db)
    modern = bootstrap(db)
    mixed = bootstrap(db)
    legacy_quotas = {**legacy[1], "active_gpu_tasks": 7, "active_runs": 9, "storage_bytes": 23456789}
    legacy_retention = {"deleted_days": 91, "artifact_days": 83, "custom_retention_note": "preserve"}
    modern_quotas = {"storage_bytes": 34567890, "source_files": 1234, "max_active_gpu_tasks": 3}
    modern_retention = {"processing_days": 19, "exports_days": 42, "temporary_hours": 48}
    db.execute("UPDATE photo.tenants SET quotas=%s,retention=%s WHERE id=%s", (Jsonb(legacy_quotas), Jsonb(legacy_retention), legacy[0]))
    db.execute("UPDATE photo.tenants SET quotas=%s,retention=%s WHERE id=%s", (Jsonb(modern_quotas), Jsonb(modern_retention), modern[0]))
    db.execute("UPDATE photo.tenants SET quotas=quotas||'{\"max_active_gpu_tasks\":3}',retention=retention||'{\"processing_days\":19}' WHERE id=%s", (mixed[0],))
    before = db.execute("SELECT oid,proowner,proacl,proconfig FROM pg_proc WHERE oid='photo.auth_bootstrap_user(text,text,boolean)'::regprocedure").fetchone()
    db.execute((ROOT / "backend/db/migrations/02575_tenant_bootstrap_policy_compatibility.sql").read_text().split("-- +goose Down")[0])
    assert db.execute("SELECT oid,proowner,proacl,proconfig FROM pg_proc WHERE oid='photo.auth_bootstrap_user(text,text,boolean)'::regprocedure").fetchone() == before
    quotas, retention, revision = db.execute("SELECT quotas,retention,revision FROM photo.tenants WHERE id=%s", (legacy[0],)).fetchone()
    assert quotas == {**legacy_quotas, "max_active_gpu_tasks": 1}
    assert retention == {"processing_days": 7, "exports_days": 30, "temporary_hours": 24, **legacy_retention}
    assert revision == legacy[3] + 1
    assert db.execute("SELECT quotas,retention,revision FROM photo.tenants WHERE id=%s", (modern[0],)).fetchone() == (modern_quotas, modern_retention, modern[3])
    mixed_quota, mixed_retention = db.execute("SELECT quotas,retention FROM photo.tenants WHERE id=%s", (mixed[0],)).fetchone()
    assert mixed_quota == {**mixed[1], "max_active_gpu_tasks": 3}
    assert mixed_retention == {**mixed[2], "processing_days": 19, "exports_days": 30, "temporary_hours": 24}
    fresh = bootstrap(db)
    assert fresh[1] == {"storage_bytes": 10737418240, "source_files": 10000, "max_active_gpu_tasks": 1}
    assert fresh[2] == {"processing_days": 7, "exports_days": 30, "temporary_hours": 24}
    assert db.execute("SELECT has_function_privilege('photo_auth','photo.auth_bootstrap_user(text,text,boolean)','EXECUTE'),has_function_privilege('photo_api','photo.auth_bootstrap_user(text,text,boolean)','EXECUTE')").fetchone() == (True, False)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute("SET LOCAL ROLE photo_api")
            db.execute("SELECT * FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",))
    assert db.execute("SELECT count(*) FROM photo.tenants").fetchone()[0] == 4


def test_full_tenant_package_with_actual_database(database):
    environment = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
                       GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local")
    result = subprocess.run(["go", "test", "-json", "-p", "2", "-count=1", "-timeout=90s", "./internal/tenant"],
                            cwd=ROOT / "backend", env=environment,
                            text=True, capture_output=True, timeout=110)
    (ROOT / "docs/implementation-v2/audit/mac-deep-testing-2026-10-17/browser-current/tenant-policy-go.jsonl").write_text(result.stdout)
    assert result.returncode == 0, result.stdout + result.stderr
