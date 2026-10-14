"""No saved intent: both original manual companions and external vectors."""
from pathlib import Path
import pytest
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture
from test_v28_relight_manual import seed_manual_inputs


@pytest.mark.parametrize('external',[False,True])
@pytest.mark.parametrize('protected',[False,True])
def test_preintent_manual_objects(db,external,protected):
    check_objects(db,external,protected)


@pytest.mark.migration_head(1001)
def test_preintent_manual_objects_upgrade(db):
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/01002_v28_relight_preintent_manual_object.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    check_objects(db,True,True)


def check_objects(db,external,protected):
    tenant,*_,scope=fixture(db,snapshot_authority=True)
    retained={};seed_manual_inputs(db,tenant,scope,retained,external=external)
    selected=scope['protected_ref'] if protected else scope['subject_ref']
    mask=retained['guard_raw'] if protected else retained['target_raw']
    selection='manual_guard' if protected else 'manual_target'
    base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)
    def locate(kind,root=retained['root_raw'],body=mask):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.v28_relight_preintent_manual_object(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,scope['project'],scope['snapshot'],scope['actor'],scope['session'],Jsonb(base),Jsonb(selected),selection,body.decode(),root.decode(),kind)).fetchone()[0]
            db.execute('RESET ROLE');return result
    for kind,role in [('target','mask_target'),('guard','mask_protected')]:
        value=locate(kind)
        assert value is not None and value['artifact_id']==str(retained['pins'][role].artifact_id)
        for other in ('mask_target','mask_protected'):
            with db.transaction(force_rollback=True):
                db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins'][other].artifact_id,))
                assert locate(kind) is None
            assert locate(kind)==value
    assert (locate('vectors') is not None) is external
    assert locate('other') is None
    assert locate('target',root=b' '+retained['root_raw']) is None
    assert locate('target',body=b' '+mask) is None
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],))
        assert locate('target') is None
    signature='photo.v28_relight_preintent_manual_object(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text,text,text,text)'
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND v28_relight_intent IS NOT NULL',(tenant,)).fetchone()[0]==0
