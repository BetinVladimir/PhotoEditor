"""Versioned params metadata and quota protocol fenced by assembly lease."""
from uuid import uuid4
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_params_reservation_completion_and_lease_fence(db):
    tid, assembly, _, _, _, _, scope = fixture(db, snapshot_authority=True)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (assembly,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s", (assembly,))
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s", (tid,))
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tid,))
    def call(name, *args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            return db.execute(f"SELECT * FROM photo.{name}({','.join(['%s'] * len(args))})", args).fetchall()
        finally:
            db.execute('RESET ROLE')
    expansion = call('open_v27_semantic_assembly', tid, run)[0][0]
    lease = call('claim_v27_semantic_assembly')[0][1]
    def reserve(token=lease, digest='a'*64):
        return call('reserve_v27_semantic_assembly_document', tid, run, expansion, token, 'task_params', digest, 2048, 'fixture')[0]
    baseline = db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tid,)).fetchone()
    assert reserve(uuid4())[0] == 'forbidden'
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0') WHERE id=%s", (tid,))
        assert reserve()[0] == 'quota'
    created = reserve()
    assert created[0] == 'created' and not created[3]
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tid,)).fetchone() == (baseline[0], baseline[1]+2048)
    assert reserve(digest='b'*64)[0] == 'conflict'
    resumed = reserve()
    assert resumed[0] == 'resumed' and resumed[1] == created[1] and resumed[2] != created[2]
    def complete(doc_token=resumed[2], claim=lease, version='pinned-v1'):
        return call('complete_v27_semantic_assembly_document', tid, run, expansion, claim, 'task_params', created[1], doc_token, 'a'*64, 2048, 'fixture', version)[0][0]
    assert not complete(created[2])
    assert not complete(claim=uuid4())
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert not complete()
    assert complete()
    assert complete()
    assert not complete(version='different-v2')
    assert reserve()[0] == 'replayed'
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (created[1],))
        assert reserve()[0] == 'stale'
        assert not complete()
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tid,)).fetchone() == (baseline[0]+2048, baseline[1])
    assert db.execute('SELECT object_version,output_role FROM photo.artifacts WHERE id=%s', (created[1],)).fetchone() == ('pinned-v1', 'task_params')
    assert call('release_v27_semantic_assembly', expansion, lease) == [(True,)]
    assert not complete()
    for signature in ('reserve_v27_semantic_assembly_document(uuid,uuid,uuid,uuid,text,text,bigint,text)', 'complete_v27_semantic_assembly_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)'):
        for role in ('photo_api', 'photo_worker', 'public'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, 'photo.'+signature)).fetchone()[0]
