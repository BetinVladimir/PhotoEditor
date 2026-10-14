import json
import pytest


def profile():
    return {'backend':'cpu','renderer_version':'v2.2','resource_estimate':{
        'ram_bytes':268435456+12*64+4096*4,'scratch_bytes':4096*3+8388608,
        'output_bytes':4096+2097152,'pixel_count':12,'vram_bytes':0}}


@pytest.mark.parametrize('field',[None,'ram_bytes','scratch_bytes','output_bytes','pixel_count','vram_bytes'])
def test_metadata_sql_resource_floors(db,field):
    value=profile()
    if field:value['resource_estimate'][field]+=1 if field=='vram_bytes' else -1
    assert db.execute('SELECT photo.v30_metadata_resources_ready(%s::jsonb,4,3,4096)',(json.dumps(value),)).fetchone()==(field is None,)


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_resource_predicate_private_acl(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_metadata_resources_ready(jsonb,bigint,bigint,bigint)','EXECUTE')",(role,)).fetchone()==(False,)
