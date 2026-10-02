"""SQL publication lifecycle; byte fixtures test fencing, not lens qualification."""
from hashlib import sha256
from uuid import uuid4,uuid5
import pytest
from psycopg.types.json import Jsonb
from test_database import tenant
from photo_worker.contracts.base import canonical_bytes


@pytest.mark.parametrize('purpose',['v25_lens_params','v25_lens_original_binding'])
@pytest.mark.parametrize('duplicate',[False,True])
def test_lens_document_publication_replay_fence_and_revocation(db,purpose,duplicate):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Lens publish') RETURNING id",(tid,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,0) RETURNING id",(tid,actor,'a'*64)).fetchone()[0]
    run_id=uuid4();workflow=uuid5(run_id,'workflow');owner=uuid5(workflow,'optical.lens:0');body='{"schema_version":1}';digest=sha256(body.encode()).hexdigest()
    profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':{'ram_bytes':536871152,'scratch_bytes':67109104,'output_bytes':33554544,'pixel_count':4,'vram_bytes':0}}
    node={'profile':profile,'task_id':str(owner),'params':{'schema_version':1},'params_sha':digest,'params_jcs':body,
          'original':{'schema_version':1},'original_sha':digest,'original_jcs':body}
    config={'project_id':str(project),'v25_lens':{'auth_session_id':str(session),'nodes':([node,node] if duplicate else [node])}}
    run=db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'fail_fast') RETURNING id",(run_id,tid,snapshot,Jsonb(config),'b'*64,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(workflow,tid,project,run,snapshot,'c'*64))
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1024,1,clock_timestamp()+interval '1 hour')",(tid,actor,run))
    db.execute("INSERT INTO photo.tenant_usage(tenant_id,reserved_bytes) VALUES(%s,1024)",(tid,))
    reserve='SELECT outcome,artifact_id,token,ready FROM photo.reserve_v25_lens_document(%s,%s,%s,%s,%s,%s,%s)'
    complete='SELECT photo.complete_v25_lens_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    args=(tid,run,owner,purpose,digest,len(body),'test')
    db.execute('SET LOCAL ROLE photo_scheduler')
    if duplicate:
        assert db.execute(reserve,args).fetchone()==('forbidden',None,None,False)
        db.execute('RESET ROLE')
        assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(1024,)
        assert db.execute('SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s',(tid,)).fetchone()==(0,)
        return
    wrong=(tid,run,owner,purpose,digest,len(body)+1,'test')
    assert db.execute(reserve,wrong).fetchone()==('forbidden',None,None,False)
    first=db.execute(reserve,args).fetchone();assert first[0]=='created' and not first[3]
    second=db.execute(reserve,args).fetchone();assert second[0]=='resumed' and second[1]==first[1] and second[2]!=first[2]
    def finish(token,version='v1'):
        return db.execute(complete,(tid,run,owner,purpose,first[1],token,digest,len(body),'test',version)).fetchone()[0]
    assert not finish(first[2])
    assert not finish(second[2],'null')
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(session,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not finish(second[2])
    assert db.execute(reserve,args).fetchone()==('forbidden',None,None,False)
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s",(session,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert finish(second[2])
    assert finish(second[2])
    assert not finish(second[2],'v2')
    replay=db.execute(reserve,args).fetchone();assert replay==('replayed',first[1],second[2],True)
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(1024,len(body))
    assert db.execute('SELECT sha256,bytes,object_version,output_role FROM photo.artifacts WHERE id=%s',(first[1],)).fetchone()==(digest,len(body),'v1',purpose)

    # Missing second document cannot stage even though the first is ready.
    db.execute('SET LOCAL ROLE photo_scheduler')
    params_id=first[1] if purpose=='v25_lens_params' else uuid4()
    stage='SELECT photo.stage_v25_lens_task(%s,%s,%s,0,%s,%s,%s,%s)'
    stage_args=(tid,run,owner,params_id,digest,'d'*64,Jsonb(profile))
    assert not db.execute(stage,stage_args).fetchone()[0]
    other='v25_lens_original_binding' if purpose=='v25_lens_params' else 'v25_lens_params'
    ticket=db.execute(reserve,(tid,run,owner,other,digest,len(body),'test')).fetchone()
    assert ticket[0]=='created'
    assert db.execute(complete,(tid,run,owner,other,ticket[1],ticket[2],digest,len(body),'test','v1')).fetchone()[0]
    params_id=first[1] if purpose=='v25_lens_params' else ticket[1]
    stage_args=(tid,run,owner,params_id,digest,'d'*64,Jsonb(profile))
    assert db.execute(stage,stage_args).fetchone()[0]
    assert db.execute(stage,stage_args).fetchone()[0]
    assert not db.execute(stage,(tid,run,owner,params_id,digest,'e'*64,Jsonb(profile))).fetchone()[0]
    assert not db.execute(stage,(tid,run,uuid4(),params_id,digest,'d'*64,Jsonb(profile))).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute("SELECT state,attempt,operation,node_key,params_artifact_id,input_digest FROM photo.tasks WHERE tenant_id=%s AND id=%s",(tid,owner)).fetchone()==('blocked',0,'optical.lens','optical.lens:0',params_id,'d'*64)


    db.execute('SET LOCAL ROLE photo_scheduler')
    original_id=first[1] if purpose=='v25_lens_original_binding' else ticket[1]
    entries=sorted([
        {'ref':{'artifact_id':str(params_id),'sha256':digest},'contract':'Params'},
        {'ref':{'artifact_id':str(original_id),'sha256':digest},'contract':'OriginalLensBinding'},
    ],key=lambda entry:entry['ref']['artifact_id'])
    entries_sha=sha256(canonical_bytes(entries)).hexdigest()
    def transport(name,value):
        raw=canonical_bytes(value);sha=sha256(raw).hexdigest()
        reservation=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,%s,%s)',
            (tid,run,owner,name,sha,len(raw),'test')).fetchone()
        assert reservation[0]=='created'
        assert db.execute('SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tid,run,owner,name,reservation[1],reservation[2],sha,len(raw),'test','v1')).fetchone()[0]
        return {'artifact_id':str(reservation[1]),'sha256':sha}
    chunk=transport('v2_input_chunk_0',{'schema_version':1,'entries':entries})
    manifest=transport('v2_input_manifest',{'schema_version':1,'task_id':str(owner),'entry_count':2,'entries_sha':entries_sha,'chunks':[chunk]})
    assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)',(tid,run,owner,Jsonb(entries))).fetchone()[0]==2
    direct=[entry['ref'] for entry in entries]
    assert db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,2,%s,%s)',
        (tid,run,owner,Jsonb(manifest),Jsonb([chunk]),entries_sha,Jsonb(direct))).fetchone()[0]
    acknowledge='SELECT photo.v25_lens_registered_inputs(%s,%s,%s,%s,%s,%s,%s,2,%s,%s)'
    ack_args=(tid,run,owner,params_id,digest,Jsonb(manifest),Jsonb([chunk]),entries_sha,Jsonb(direct))
    assert db.execute(acknowledge,ack_args).fetchone()[0]
    assert not db.execute(acknowledge,(tid,run,owner,params_id,digest,Jsonb(manifest),Jsonb([chunk]),'e'*64,Jsonb(direct))).fetchone()[0]
    assert not db.execute(acknowledge,(tid,run,owner,params_id,digest,Jsonb(manifest),Jsonb([chunk]),entries_sha,Jsonb([]))).fetchone()[0]
    assert db.execute(stage,stage_args).fetchone()[0]
    assert not db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tid,run)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(owner,)).fetchone()[0]
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(owner,)).fetchone()==(4,)
    assert db.execute('SELECT graph_sealed,status FROM photo.runs WHERE id=%s',(run,)).fetchone()==(False,'draft')
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(session,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not db.execute(acknowledge,ack_args).fetchone()[0]
    assert not db.execute(stage,stage_args).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE tenant_id=%s',(tid,)).fetchone()==(1,)
