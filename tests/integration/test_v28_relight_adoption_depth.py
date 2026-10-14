"""Completed depth SQL lineage; no model inference or original Params IO."""
from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_adoption_source import complete_relight_task


def test_completed_depth_origin_without_worker_lease_and_revocation(db):
    retained={}
    def setup(db,tenant,scope):return seed_depth_inputs(db,tenant,scope,retained)
    def build(pin,scope):return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),calibration=pin['calibration'],depth=scope['depth_ref']))
    args,scope,pin=relight_lease_fixture(db,qualified=True,params_builder=build,extra_inputs_factory=setup)
    result,_=complete_relight_task(db,args)
    def origin(body=retained['body']):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.v28_relight_adoption_depth_origin(%s,%s,1,1,%s,%s,%s)',(args[0],args[2],result,args[8],body.decode())).fetchone()[0]
            db.execute('RESET ROLE');return value
    actual=origin()
    assert actual is not None and actual['artifact_id']==str(retained['params'])
    assert origin(b' '+retained['body']) is None
    for query,values in (
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pixels'],)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['invalid'],)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['root'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert origin() is None
        assert origin()==actual
    signature='photo.v28_relight_adoption_depth_origin(uuid,uuid,integer,bigint,uuid,text,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
