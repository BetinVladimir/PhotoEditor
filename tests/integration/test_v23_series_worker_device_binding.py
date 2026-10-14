"""Operator-provisioned series device pins are enforced by installed SQL."""

from uuid import uuid4
from psycopg.types.json import Jsonb
from test_database import tenant


def test_series_device_binding_is_private_and_fails_closed(db):
    signature = 'photo.v23_series_worker_device_matches(uuid,uuid,jsonb)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    for role in ('photo_worker', 'photo_api', 'photo_gateway',
                 'photo_scheduler', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
    assert not db.execute(
        'SELECT photo.v23_series_worker_device_matches(%s,%s,%s::jsonb)',
        (uuid4(), uuid4(), '{"device_profile_sha":"' + 'a' * 64 +
         '","runtime_lock_sha":"' + 'b' * 64 + '"}')).fetchone()[0]


def test_series_device_binding_requires_operator_column(db):
    assert db.execute(
        "SELECT EXISTS(SELECT 1 FROM information_schema.columns "
        "WHERE table_schema='photo' AND table_name='worker_registrations' "
        "AND column_name='v23_series_device_profile_sha')"
    ).fetchone()[0]


def test_series_device_rotation_revokes_existing_profile(db):
    tenant_id, _ = tenant(db, str(uuid4()) + '@example.test')
    worker_id = uuid4()
    node = 'series-' + str(uuid4())
    runtime, device, rotated = 'b' * 64, 'c' * 64, 'd' * 64
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,"
               "vram_budget_bytes,scratch_budget_bytes) "
               "VALUES(%s,10000000,0,10000000)", (node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,"
               "resource_classes,backends,lock_id,last_seen,capacity,"
               "db_principal,node_id,enabled,qualified_operations,"
               "v23_series_device_profile_sha) VALUES(%s,%s,'[\"cpu\"]',"
               "'[\"cpu\"]',%s,now(),'{}',session_user,%s,true,"
               "'{\"series.balance\":[\"cpu\"]}',%s)",
               (tenant_id, worker_id, runtime, node, device))
    profile = Jsonb(dict(runtime_lock_sha=runtime, device_profile_sha=device))
    def matches():
        return db.execute(
            'SELECT photo.v23_series_worker_device_matches(%s,%s,%s)',
            (tenant_id, worker_id, profile)).fetchone()[0]
    assert matches()
    db.execute('UPDATE photo.worker_registrations SET '
               'v23_series_device_profile_sha=%s WHERE worker_id=%s',
               (rotated, worker_id))
    assert not matches()


def test_series_commit_guard_is_worker_only(db):
    signature = 'photo.commit_task_success(uuid,integer,bigint,uuid,text)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_worker', signature, 'EXECUTE')).fetchone()[0]
    for role in ('photo_api', 'photo_gateway', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
