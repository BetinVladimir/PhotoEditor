"""Durable compact parent identity; child execution/recovery is separate."""
import json
from copy import deepcopy
from uuid import uuid4,uuid5
from hashlib import sha256
from layer_original_fixture import seed_original_composition
from photo_worker.contracts.base import canonical_bytes


def test_render_batch_manifest_replay_scope_and_gap_denial(db):
    case=seed_original_composition(db)
    tenant,project,snapshot,actor,session=(case[k] for k in ('Tenant','Project','Snapshot','Actor','Session'))
    batch=uuid4()
    body=dict(policy_sha='a'*64,schema_version=1,project_id=project,input_snapshot_id=snapshot,auth_session_id=session,parts=[dict(run_id=str(uuid5(batch,'render.batch:0')),first_ordinal=0,frame_count=1,config_sha='a'*64,output_bytes=33555032)])
    raw=canonical_bytes(body).decode()
    query='SELECT photo.begin_v26_render_batch('+','.join(['%s']*7)+')'
    args=(tenant,project,snapshot,actor,session,batch)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(query,(*args,raw)).fetchone()==(True,)
    assert db.execute(query,(*args,raw)).fetchone()==(True,)
    row=db.execute('SELECT * FROM photo.v26_render_batch_manifest(%s,%s,%s,%s)',(tenant,batch,actor,session)).fetchone()
    assert row==(body,sha256(raw.encode()).hexdigest(),False)
    assert db.execute('SELECT * FROM photo.v26_render_batch_parts(%s,%s,%s,%s)',(tenant,batch,actor,session)).fetchone()==(0,uuid5(batch,'render.batch:0'),0,1,'missing',0,0,0,0)
    assert db.execute('SELECT * FROM photo.v26_render_batch_manifest(%s,%s,%s,%s)',(tenant,batch,actor,uuid4())).fetchall()==[]
    for field,value in [('first_ordinal',1),('frame_count',2),('config_sha','b'*64),('run_id',str(uuid4())),('output_bytes',9007199254740992)]:
        changed=deepcopy(body);changed['parts'][0][field]=value
        assert db.execute(query,(*args,json.dumps(changed,sort_keys=True,separators=(',',':')))).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(1,)
    for name in ('begin_v26_render_batch(uuid,uuid,uuid,uuid,uuid,uuid,text)','v26_render_batch_manifest(uuid,uuid,uuid,uuid)','v26_render_batch_parts(uuid,uuid,uuid,uuid)'):
        for role in ('photo_api','photo_worker'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,'photo.'+name)).fetchone()==(False,)
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(query,(*args,raw)).fetchone()==(False,)
    assert db.execute('SELECT * FROM photo.v26_render_batch_manifest(%s,%s,%s,%s)',(tenant,batch,actor,session)).fetchall()==[]
