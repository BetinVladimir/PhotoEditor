"""Authored real-row heavy admission boundaries; NOT RUN."""
from uuid import uuid4

from psycopg.types.json import Jsonb

from test_v32_capacity_rotation_concurrency import _install


def _scope(db):
    user = db.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)", (uuid4().hex+'@heavy.test',)).fetchone()[0]
    tenant = db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s', (user,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id", (tenant, uuid4().hex, 'b'*64)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,1) RETURNING id", (tenant, user, 'b'*64)).fetchone()[0]
    return tenant, user, artifact, snapshot


def _task(db, scope, resource='gpu', state='queued', kind='v2'):
    tenant, user, artifact, snapshot = scope
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,%s,'running',%s,%s,%s,%s,true) RETURNING id", (tenant, kind, snapshot, Jsonb({'queue_class':'batch'} if kind=='v2' else {}), 'b'*64, user)).fetchone()[0]
    return db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,lease_until) VALUES(%s,%s,'heavy:0','asset.verify','1',%s,%s,'[]',%s,%s,%s,CASE WHEN %s='running' THEN clock_timestamp()+interval '60 seconds' ELSE NULL END) RETURNING id", (tenant, run, resource, state, artifact, 'b'*64, Jsonb({'backend':'cuda' if resource=='gpu' else 'cpu'}), state)).fetchone()[0]


def _available(db, pin):
    return db.execute('SELECT photo.capacity_heavy_admission_available(%s)', (pin,)).fetchone()[0]


def test_heavy_inflight_exact_limit_and_release(db):
    pin = _install(db, 60)  # fixture policy heavy_admission_inflight=8
    scope = _scope(db)
    db.execute('SELECT pg_advisory_xact_lock(2026,32)')
    assert _available(db, pin) is True
    ids = [_task(db, scope) for _ in range(7)]
    assert _available(db, pin) is True
    last = _task(db, scope, state='running')
    assert _available(db, pin) is False
    # Cancellation request alone must not free an outstanding reservation.
    db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (last,))
    assert _available(db, pin) is False
    db.execute("UPDATE photo.tasks SET state='cancelled',lease_until=NULL WHERE id=%s", (last,))
    assert _available(db, pin) is True
    _task(db, scope)
    assert _available(db, pin) is False
    db.execute("UPDATE photo.tasks SET state='failed' WHERE id=%s", (ids[0],))
    assert _available(db, pin) is True


def test_heavy_inflight_counts_legacy_and_other_tenants_but_not_cpu_or_ready(db):
    pin = _install(db, 60)
    first, second = _scope(db), _scope(db)
    db.execute('SELECT pg_advisory_xact_lock(2026,32)')
    for state in ('blocked','ready','retry_wait','failed','cancelled','skipped'):
        _task(db, first, state=state)
    for _ in range(8):
        _task(db, first, resource='cpu')
    assert _available(db, pin) is True
    for _ in range(4):
        _task(db, first)
        _task(db, second, kind='apply')
    assert _available(db, pin) is False
