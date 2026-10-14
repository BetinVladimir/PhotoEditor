"""Nonempty pool/legacy partition fixtures; authored, NOT RUN."""
from uuid import uuid4

import pytest

from psycopg.types.json import Jsonb
from psycopg.pq import TransactionStatus


@pytest.mark.parametrize("backlog", [1, 10002])
def test_v2_snapshot_and_selected_picker_do_not_consume_legacy_run(db, backlog):
    user = db.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)", (uuid4().hex+'@queue.test',)).fetchone()[0]
    tenant = db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s', (user,)).fetchone()[0]
    artifact = db.execute(
        "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) "
        "VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id",
        (tenant, uuid4().hex, 'b'*64),
    ).fetchone()[0]
    snapshot = db.execute(
        "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
        "VALUES(%s,'input','user',%s,%s,1) RETURNING id", (tenant, user, 'b'*64),
    ).fetchone()[0]
    node, worker = 'queue-'+uuid4().hex, uuid4()
    db.execute(
        'INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,'
        'scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) '
        'VALUES(%s,true,10000000,0,10000000,10000000,10000000,clock_timestamp(),%s)', (node, 'b'*64),
    )
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'media1','media','{}')", (node,))
    db.execute(
        'INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,'
        'backends,lock_id,last_seen,capacity,db_principal,enabled,qualified_operations) '
        "VALUES(%s,%s,%s,'[\"media\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,true,'{\"asset.verify\":[\"cpu\"]}')",
        (tenant, worker, node, 'b'*64),
    )
    tasks = {}
    for kind, config in [('apply', {}), ('v2', {'queue_class':'batch'})]:
        run = db.execute(
            'INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,'
            'config_hash,created_by,graph_sealed) VALUES(%s,%s,\'running\',%s,%s,%s,%s,true) RETURNING id',
            (tenant, kind, snapshot, Jsonb(config), 'b'*64, user),
        ).fetchone()[0]
        tasks[kind] = db.execute(
            'INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,'
            'resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) '
            "VALUES(%s,%s,'queue:0','asset.verify','1','media','ready','[]',%s,%s,%s) RETURNING id",
            (tenant, run, artifact, 'b'*64, Jsonb({'backend':'cpu','resource_estimate':{'ram_bytes':2048,'vram_bytes':0,'scratch_bytes':1024,'output_bytes':0,'pixel_count':0}})),
        ).fetchone()[0]
    if backlog > 1:
        # Add real ready tasks to the final V2 run, preserving legacy isolation.
        db.execute(
            "INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,"
            "resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) "
            "SELECT %s,%s,'queue:'||n::text,'asset.verify','1','media','ready','[]',%s,%s,%s "
            "FROM generate_series(1,%s) n",
            (tenant, run, artifact, 'b'*64, Jsonb({'backend':'cpu','resource_estimate':{'ram_bytes':2048,'vram_bytes':0,'scratch_bytes':1024,'output_bytes':0,'pixel_count':0}}), backlog-1),
        )
    locked = False
    try:
        assert db.execute('SELECT pg_try_advisory_lock(2026,32)').fetchone() == (True,)
        locked = True
        db.execute('SET LOCAL ROLE photo_scheduler')
        candidates = db.execute('SELECT * FROM photo.capacity_queue_candidates(%s)', ('cpu',)).fetchall()
        assert len(candidates) == backlog
        assert len({row[0] for row in candidates}) == backlog
        assert all(row[1:3] == (tenant, 'batch') and row[4:] == (None, False, 0) for row in candidates)
        original = next(row for row in candidates if row[0] == tasks['v2'])
        baseline_nodes = db.execute('SELECT node_id FROM photo.eligible_dispatch_nodes(%s)', (tasks['v2'],)).fetchall()
        assert baseline_nodes
        assert db.execute('SELECT node_id FROM photo.capacity_affinity_dispatch_nodes(%s)', (tasks['v2'],)).fetchall() == baseline_nodes

        assert db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', ('cpu', tasks['v2'])).fetchall() == [original]
        # Last row is beyond the former 10001-row prefix in the large fixture.
        tail = candidates[-1]
        assert db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', ('cpu', tail[0])).fetchall() == [tail]
        assert db.execute('SELECT task_id FROM photo.capacity_pick_selected_task(%s,%s,%s,2)', (tail[0], tenant, 'cpu')).fetchall() == [(tail[0],)]
        assert db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', ('cpu', tasks['apply'])).fetchall() == []
        assert db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', ('gpu', tasks['v2'])).fetchall() == []

        for pool in ('gpu','signer'):
            assert db.execute('SELECT * FROM photo.capacity_queue_candidates(%s)', (pool,)).fetchall() == []
        legacy = db.execute('SELECT task_id FROM photo.pick_ready_legacy_task()').fetchall()
        assert legacy == [(tasks['apply'],)]
        selected = db.execute('SELECT task_id FROM photo.capacity_pick_selected_task(%s,%s,%s,2)', (tasks['v2'],tenant,'cpu')).fetchall()
        assert selected == [(tasks['v2'],)]
        assert db.execute('SELECT * FROM photo.capacity_pick_selected_task(%s,%s,%s,2)', (tasks['apply'],tenant,'cpu')).fetchall() == []
        assert db.execute('SELECT * FROM photo.capacity_pick_selected_task(%s,%s,%s,2)', (tasks['v2'],uuid4(),'cpu')).fetchall() == []
    finally:
        # Session locks survive rollback; fixture close is the final backstop.
        if locked and db.info.transaction_status != TransactionStatus.INERROR:
            db.execute('RESET ROLE')
            db.execute('SELECT pg_advisory_unlock(2026,32)')
