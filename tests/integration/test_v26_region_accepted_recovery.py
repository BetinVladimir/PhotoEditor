"""Real SQL-role recovery fixture; no model execution or object IO is claimed."""
from hashlib import sha256
import json
from uuid import uuid4, uuid5
import pytest
from psycopg.types.json import Jsonb
from test_v26_region_source import _region_scope_fixture


def _accepted(db):
    tenant, actor, session, plan, *_ = _region_scope_fixture(db)
    project, asset = plan['project_id'], plan['asset_id']
    plan['request'].update(rect=dict(x0=0,y0=0,x1=1,y1=1),max_edge=512)
    plan['frozen_recipe'].update(icc_lock_sha='a'*64,model_lock_sha='b'*64)
    key=str(uuid4());run=uuid5(tenant,f'V2.Admit:{actor}:{key}');workflow=uuid5(run,'workflow');task=uuid5(workflow,'region.preview:0')
    body=json.dumps(plan,sort_keys=True,separators=(',',':'));digest=sha256(body.encode()).hexdigest()
    def artifact(suffix,pin):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,%s,'application/json','document',now()) RETURNING id",(tenant,str(uuid4())+suffix,pin,len(body))).fetchone()[0]
    params=artifact('params',digest);manifest=artifact('manifest','e'*64);chunk=artifact('chunk','f'*64)
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id",(tenant,actor,'a'*64)).fetchone()[0]
    profile=dict(renderer_version='v2.2',backend='cuda',icc_lock_sha='a'*64,model_lock_sha='b'*64,region_runtime_sha256='c'*64)
    node=dict(task_id=str(task),params=dict(artifact_id=str(params),sha256=digest),params_jcs=body,input_digest='d'*64,profile=profile)
    config=dict(project_id=project,input_snapshot_id=str(snapshot),v26_region=dict(auth_session_id=str(session),nodes=[node]))
    config_sha=sha256(json.dumps(config,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,'fail_fast',true)",(run,tenant,snapshot,Jsonb(config),config_sha,actor))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(workflow,tenant,project,run,snapshot,'e'*64))
    manifest_ref=dict(artifact_id=str(manifest),sha256='e'*64)
    execution=dict(profile,contracts_sha='4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',v2_input_manifest=manifest_ref,v2_entry_count=1,v2_entries_sha='a'*64)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'region.preview:0','region.preview','1','gpu',%s,%s,%s,%s)",(task,tenant,run,Jsonb([manifest_ref]),params,'d'*64,Jsonb(execution)))
    for ordinal,(aid,pin,contract) in enumerate([(params,digest,'Params'),(manifest,'e'*64,'InputClosureManifest'),(chunk,'f'*64,'InputClosureChunk')]):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,task,aid,pin,contract,ordinal))
    request_sha=sha256(json.dumps(dict(tenant_id=str(tenant),asset_id=asset,request=plan['request']),sort_keys=True,separators=(',',':')).encode()).hexdigest()
    ticket=dict(run_id=str(run),workflow_id=str(workflow),state='queued')
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,resource_id,response_status,response_body,expires_at) VALUES(%s,'user',%s,'V2.Admit',%s,%s,%s,202,%s,now()-interval '1 hour')",(tenant,actor,key,request_sha,run,Jsonb(ticket)))
    args=(tenant,asset,actor,session,key,request_sha,run,workflow,task)
    return args,ticket,params,chunk


def _probe(db,args,role='photo_api'):
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
        db.execute('SET LOCAL ROLE '+role)
        return db.execute('SELECT photo.v26_region_accepted_recovery(%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0]


def test_exact_region_recovery_survives_disabled_runtime_and_expired_receipt(db):
    args,ticket,params,chunk=_accepted(db)
    db.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='region.preview' AND version='1'")
    assert _probe(db,args)['ticket']==ticket
    assert _probe(db,args)['ticket']==ticket
    missing=list(args);missing[4]=str(uuid4());assert _probe(db,tuple(missing)) is None
    for index,value in [(1,uuid4()),(3,uuid4()),(5,'0'*64),(6,uuid4()),(7,uuid4()),(8,uuid4())]:
        changed=list(args);changed[index]=value
        with pytest.raises(Exception):_probe(db,tuple(changed))
    # No foreign-actor receipt is disclosed, even with knowledge of the key.
    foreign=list(args);foreign[2]=uuid4();assert _probe(db,tuple(foreign)) is None
    foreign=list(args);foreign[0]=uuid4();assert _probe(db,tuple(foreign)) is None
    renewed=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,'web',now()+interval '1 hour') RETURNING id",(args[2],uuid4().bytes,uuid4())).fetchone()[0]
    relogin=list(args);relogin[3]=renewed;assert _probe(db,tuple(relogin))['ticket']==ticket
    for sql,param in [('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',args[3]),('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',params),('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',chunk)]:
        with db.transaction(force_rollback=True):
            db.execute(sql,(param,))
            with pytest.raises(Exception):_probe(db,args)
    for role in ['photo_scheduler','photo_worker']:
        with pytest.raises(Exception):_probe(db,args,role)


def test_unsealed_region_receipt_is_not_claimed_as_accepted(db):
    args,_,_,_=_accepted(db)
    # An allocated receipt before a run exists must fall through preparation.
    pending=list(args);pending[4]=str(uuid4())
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'V2.Admit',%s,%s,now()+interval '1 hour')",(args[0],args[2],pending[4],args[5]))
    assert _probe(db,tuple(pending)) is None
    # A sealed graph with an incomplete receipt is never reported as accepted.
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.idempotency_records SET response_status=NULL,response_body=NULL WHERE tenant_id=%s AND principal_id=%s AND route='V2.Admit' AND key=%s",(args[0],args[2],args[4]))
        with pytest.raises(Exception):_probe(db,args)
