"""Metadata boundary for V32 outer queue vs V28 exact authored receipt binding.
Authored only; not an admitted receipt/GPU/quality qualification fixture.
"""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_relight_strict_receipt_outcomes import scope

@pytest.mark.parametrize('reader',['strict','stored'])
@pytest.mark.parametrize('queue,extra,expected',[(None,False,'absent'),('interactive',False,'absent'),('batch',False,'conflict'),('interactive',True,'conflict')])
def test_queue_does_not_change_exact_authored_request(db,reader,queue,extra,expected):
    tenant,project,actor,session=scope(db)
    run=uuid4();request={'run_id':str(run)};pin={}
    config={'project_id':str(project),'v28_relight_request':request,'v28_relight_pin':pin,'auth_session_id':str(session)}
    if queue is not None:config['queue_class']=queue
    if extra:config['unknown_authored_field']=True
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'fixture-v1',%s,100,'application/json','working_image') RETURNING id",(tenant,str(uuid4()),'a'*64)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) VALUES(%s,'input','user',%s,%s,1,%s) RETURNING id",(tenant,actor,'a'*64,artifact)).fetchone()[0]
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,false)",(run,tenant,snapshot,Jsonb(config),'b'*64,actor))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        if reader=='stored':
            result=db.execute('SELECT photo.read_v28_relight_stored_request_outcome(%s,%s,%s,%s,%s,%s)',(tenant,project,run,actor,session,Jsonb(request))).fetchone()[0]
        else:
            result=db.execute('SELECT photo.read_v28_relight_request_outcome(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,run,actor,session,Jsonb(request),Jsonb(pin))).fetchone()[0]
        db.execute('RESET ROLE')
    assert result=={'outcome':expected}

@pytest.mark.parametrize('role',['photo_api','photo_scheduler','photo_worker','photo_gateway','photo_portal'])
def test_queue_normalizer_is_private(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v32_relight_receipt_authored_config(jsonb)','EXECUTE')",(role,)).fetchone()==(False,)
