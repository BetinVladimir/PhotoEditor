"""Pre-intent source-bound producer discovery; synthetic producer metadata."""
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_person import seed_person_inputs
from test_v28_relight_manual import seed_manual_inputs


@pytest.mark.parametrize('kind',['depth','person','manual_target','manual_guard'])
def test_relight_preintent_discovery_and_revocation(db,kind):
    check_discovery(db,kind)


@pytest.mark.migration_head(1000)
def test_relight_preintent_upgrade(db):
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/01001_v28_relight_preintent_producer.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    check_discovery(db,'manual_guard')


def check_discovery(db,kind,ambiguous=False):
    tenant,*_,scope=fixture(db,snapshot_authority=True)
    retained={}
    if kind=='depth':seed_depth_inputs(db,tenant,scope,retained);selected=scope['depth_ref']
    elif kind=='person':seed_person_inputs(db,tenant,scope,retained);selected=scope['subject_ref']
    else:
        seed_manual_inputs(db,tenant,scope,retained,ambiguous=ambiguous)
        selected=scope['protected_ref'] if kind=='manual_guard' else scope['subject_ref']
    base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)
    def discover(ref=selected,operation=kind,tid=tenant):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.v28_relight_preintent_producer(%s,%s,%s,%s,%s,%s,%s,%s)',(tid,scope['project'],scope['snapshot'],scope['actor'],scope['session'],Jsonb(base),Jsonb(ref),operation)).fetchone()[0]
            db.execute('RESET ROLE');return result
    value=discover()
    if ambiguous:
        assert value is None
        return
    assert value is not None and value['selected']['artifact_id']==selected['artifact_id']
    assert value['params']['artifact_id']==str(retained['params'])
    assert set(value)=={'selected','root','params'}
    assert discover(operation='other') is None
    assert discover(tid=uuid4()) is None
    assert discover(ref=dict(selected,sha256='0'*64)) is None
    for query,args in (
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(retained['task'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
    ):
        with db.transaction(force_rollback=True):db.execute(query,args);assert discover() is None
        assert discover()==value
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND v28_relight_intent IS NOT NULL',(tenant,)).fetchone()[0]==0
    signature='photo.v28_relight_preintent_producer(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text)'
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')


@pytest.mark.parametrize('kind',['manual_target','manual_guard'])
def test_relight_preintent_ambiguous_canonical_roots_refused(db,kind):
    check_discovery(db,kind,ambiguous=True)
