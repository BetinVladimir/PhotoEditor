"""Completed manual SQL metadata; no physical vector/raster replay."""
import pytest
from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_manual import seed_manual_inputs
from test_v28_relight_adoption_source import complete_relight_task

@pytest.mark.parametrize('protected',[False,True])
def test_completed_manual_origin_and_child_revocation(db,protected):
    retained={}
    def setup(db,tenant,scope):return seed_manual_inputs(db,tenant,scope,retained)
    def params(pin,scope):return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),subject=scope['subject_ref'],protected=scope['protected_ref'],calibration=pin['calibration']))
    args,scope,_=relight_lease_fixture(db,qualified=True,params_builder=params,extra_inputs_factory=setup)
    result,_=complete_relight_task(db,args)
    mask_raw=retained['guard_raw'] if protected else retained['target_raw']
    def origin(root=retained['root_raw'],mask=mask_raw):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.v28_relight_adoption_manual_origin('+','.join(['%s']*9)+')',(args[0],args[2],1,1,result,args[8],mask.decode(),None if root is None else root.decode(),protected)).fetchone()[0]
            db.execute('RESET ROLE');return value
    actual=origin()
    assert actual==dict(root=retained['root'].model_dump(mode='json'),params=dict(artifact_id=str(retained['params']),sha256=retained['params_sha']))
    assert origin(None)==actual
    assert origin(b' '+retained['root_raw']) is None
    assert origin(mask=b' '+mask_raw) is None
    mutations=[('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(retained['run'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],))]
    for role in ('mask_target_ref' if not protected else 'mask_guard_ref','mask_target','mask_protected','rasterized_mask'):
        mutations.append(('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins'][role].artifact_id,)))
    for query,values in mutations:
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert origin() is None
        assert origin()==actual

    signature='photo.v28_relight_adoption_manual_origin(uuid,uuid,integer,bigint,uuid,text,text,text,boolean)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
