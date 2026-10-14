from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task


def test_completed_qualification_catalog_and_revocation(db):
    def build(pin,scope):return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),calibration=pin['calibration']))
    args,scope,pin=relight_lease_fixture(db,qualified=True,params_builder=build)
    result,root=complete_relight_task(db,args)
    def catalog():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            answer=db.execute('SELECT photo.v28_relight_adoption_qualification(%s,%s,1,1,%s,%s)',(args[0],args[2],result,args[8])).fetchone()[0]
            db.execute('RESET ROLE');return answer
    value=catalog()
    assert set(value)=={'pin','policy','report','calibration'} and value['pin']==pin
    for label in ('report','calibration'):
        assert value[label]['artifact_id']==pin[label]['artifact_id']
        assert value[label]['sha256']==pin[label]['sha256']
        assert value[label]['object_version'] not in ('','null',None)
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert catalog() is None
    assert catalog()==value
    signature='photo.v28_relight_adoption_qualification(uuid,uuid,integer,bigint,uuid,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
