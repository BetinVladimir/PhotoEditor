"""Durable compilation lease; publication is a separate protocol."""
from uuid import uuid4
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_barrier_claim_recovery_and_fencing(db):
    tid, assembly, _, _, _, _, scope = fixture(db, snapshot_authority=True)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (assembly,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s", (assembly,))
    def call(name, *args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            placeholders = ','.join(['%s'] * len(args))
            return db.execute(f'SELECT * FROM photo.{name}({placeholders})', args).fetchall()
        finally:
            db.execute('RESET ROLE')
    expansion = call('open_v27_semantic_assembly', tid, run)[0][0]
    assert expansion is not None
    assert call('open_v27_semantic_assembly', tid, run) == [(expansion,)]
    assert call('open_v27_semantic_assembly', uuid4(), run) == [(None,)]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert call('claim_v27_semantic_assembly') == []
    first = call('claim_v27_semantic_assembly')[0]
    assert first[0] == expansion and first[3:5] == (tid, run)
    assert call('v27_semantic_assembly_lease_live', tid, run, expansion, first[1]) == [(True,)]
    assert call('v27_semantic_assembly_lease_live', uuid4(), run, expansion, first[1]) == [(False,)]
    assert call('v27_semantic_assembly_lease_live', tid, run, expansion, uuid4()) == [(False,)]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (run,))
        assert call('v27_semantic_assembly_lease_live', tid, run, expansion, first[1]) == [(False,)]
    assert call('claim_v27_semantic_assembly') == []
    assert call('renew_v27_semantic_assembly', expansion, uuid4()) == [(False,)]
    assert call('renew_v27_semantic_assembly', expansion, first[1]) == [(True,)]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert call('renew_v27_semantic_assembly', expansion, first[1]) == [(False,)]
    db.execute("UPDATE photo.graph_expansions SET expansion_lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (expansion,))
    second = call('claim_v27_semantic_assembly')[0]
    assert second[1] != first[1]
    assert call('v27_semantic_assembly_lease_live', tid, run, expansion, first[1]) == [(False,)]
    assert call('release_v27_semantic_assembly', expansion, first[1]) == [(False,)]
    assert call('renew_v27_semantic_assembly', expansion, first[1]) == [(False,)]
    assert call('release_v27_semantic_assembly', expansion, second[1]) == [(True,)]
    assert call('v27_semantic_assembly_lease_live', tid, run, expansion, second[1]) == [(False,)]
    assert call('claim_v27_semantic_assembly') == []  # retry backoff
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s', (run,)).fetchone() == (True,)
    for signature in ('open_v27_semantic_assembly(uuid,uuid)', 'claim_v27_semantic_assembly()', 'renew_v27_semantic_assembly(uuid,uuid)', 'release_v27_semantic_assembly(uuid,uuid)', 'v27_semantic_assembly_lease_live(uuid,uuid,uuid,uuid)'):
        for role in ('photo_api', 'photo_worker', 'public'):
            assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, 'photo.' + signature)).fetchone()[0]
