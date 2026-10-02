"""Binary upload metadata lifecycle; completion uses synthetic object versions."""
from uuid import uuid4
import json
import pytest
import psycopg
from layer_original_fixture import seed_original_composition


def test_mask_pixels_reserve_complete_replay(db):
    case=seed_original_composition(db);version=json.loads(case['Params'])['base']['asset_version_id']
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
    db.execute('SET LOCAL ROLE photo_api')
    doc=uuid4();args=(case['Tenant'],version,doc,case['Actor'],case['Session'],'a'*64,228,'test',5,5)
    def reserve(values=args):return db.execute('SELECT * FROM photo.reserve_v25_user_mask_pixels(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()
    created=reserve();assert created[0]=='created' and created[3] is False
    resumed=reserve();assert resumed[0]=='resumed' and resumed[1]==created[1] and resumed[2]!=created[2]
    assert reserve((*args[:8],4,5))[0]=='conflict'
    assert db.execute('SELECT * FROM photo.reserve_v2_draft_upload(%s,%s,%s,%s,%s,%s,%s,%s)',args[:8]).fetchone()[0]=='conflict'
    completion=(*args[:5],resumed[1],resumed[2],args[5],args[6],args[7],'synthetic-v1',5,5)
    query='SELECT photo.complete_v25_user_mask_pixels('+','.join(['%s']*13)+')'
    stale=list(completion);stale[6]=created[2]
    assert db.execute(query,stale).fetchone()[0] is False
    assert db.execute(query,completion).fetchone()[0] is True
    assert db.execute(query,completion).fetchone()[0] is True
    json_complete='SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')'
    assert db.execute(json_complete,completion[:11]).fetchone()[0] is False
    ready=reserve();assert ready[0]=='replayed' and ready[3] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(228,0)
    assert db.execute('SELECT media_type,output_role,bytes,object_version FROM photo.artifacts WHERE id=%s',(resumed[1],)).fetchone()==('application/x-npy','mask_pixels',228,'synthetic-v1')
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
    db.execute('SET LOCAL ROLE photo_api')
    assert reserve()[0]=='forbidden'
    assert db.execute(query,completion).fetchone()[0] is False


def test_mask_pixels_dimension_bounds(db):
    db.execute('SET LOCAL ROLE photo_api')
    for width,height in [(0,1),(1,0),(-1,1),(2**63-1,1),(1,2**63-1),(200000000,2),(None,1)]:
        with pytest.raises(psycopg.errors.InvalidParameterValue),db.transaction():
            db.execute('SELECT * FROM photo.reserve_v25_user_mask_pixels(%s,%s,%s,%s,%s,%s,228,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,'test',width,height))
