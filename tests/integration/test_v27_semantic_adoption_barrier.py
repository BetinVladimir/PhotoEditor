"""SQL metadata acceptance atomically opens assembly; no GPU content proof."""
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_transfer_acceptance_opens_barrier_atomically(db):
    tid, assembly, _, producer, result, candidate, _ = fixture(db, snapshot_authority=True)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (producer,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s", (assembly,))
    db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)", (tid, producer, result))
    inventory = Jsonb([dict(artifact_id=str(candidate), sha256='9'*64, purpose='a'*64, ordinal=0)])
    def adopt(fence=1):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            return db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,%s,%s,%s,%s)', (tid, producer, fence, result, inventory, 'a'*64)).fetchone()[0]
        finally:
            db.execute('RESET ROLE')
    def barriers():
        return db.execute("SELECT id FROM photo.graph_expansions WHERE tenant_id=%s AND run_id=%s AND kind='v2.semantic.assemble/1'", (tid, run)).fetchall()
    assert not adopt(fence=2)
    assert barriers() == []
    with db.transaction(force_rollback=True):
        assert adopt()
        assert len(barriers()) == 1
    assert barriers() == []
    assert adopt()
    first = barriers()
    assert len(first) == 1
    assert not adopt()
    assert barriers() == first
    for role in ('photo_scheduler', 'photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.adopt_v2_plan_pre_semantic_barrier(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')", (role,)).fetchone()[0]
