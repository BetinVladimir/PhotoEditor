"""Read-only exact version locations for a complete fenced assembly claim."""
from uuid import uuid4,UUID
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_source_inventory_is_fenced_and_scoped(db):
    tid, assembly, _, producer, _, candidate, scope = fixture(db, snapshot_authority=True)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (assembly,)).fetchone()[0]
    params = db.execute('SELECT params_artifact_id FROM photo.tasks WHERE id=%s', (producer,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s", (assembly,))
    def call(name, *args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute(f"SELECT * FROM photo.{name}({','.join(['%s']*len(args))})", args).fetchall()
        finally:db.execute('RESET ROLE')
    expansion = call('open_v27_semantic_assembly', tid, run)[0][0]
    lease = call('claim_v27_semantic_assembly')[0][1]
    def read(tenant=tid, token=lease):
        return call('v27_semantic_assembly_source_inventory', tenant, run, expansion, token)
    rows = read()
    assert {(row[0],row[1],row[3]) for row in rows} == {
        (producer,params,'Params'), (producer,candidate,'TransferCandidate'),
        (producer,UUID(scope['qualification']['calibration']['artifact_id']),'Calibration'),
        (None,scope['root'],'InputSnapshot'), (None,scope['source'],'WorkingImage')}
    assert all(row[6] == ('pinned-v1' if row[3]=='Calibration' else 'v1') and row[8] == 'application/json' for row in rows)
    assert read(tenant=uuid4()) == []
    assert read(token=uuid4()) == []
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert read() == []
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s', (producer,))
        assert read() == []
    assert call('release_v27_semantic_assembly', expansion, lease) == [(True,)]
    assert read() == []
    for role in ('photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_assembly_source_inventory(uuid,uuid,uuid,uuid)','EXECUTE')", (role,)).fetchone()[0]
