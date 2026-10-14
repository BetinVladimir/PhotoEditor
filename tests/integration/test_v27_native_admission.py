"""Composed SQL admission evidence; synthetic metadata, no GPU execution."""
from copy import deepcopy
from hashlib import sha256
import json
from uuid import UUID, uuid5, uuid4

import pytest
from psycopg.types.json import Jsonb
from psycopg import errors
from photo_worker.contracts.base import canonical_bytes
from test_v27_native_anchor import seed_native_anchor


@pytest.mark.parametrize('operation,stage',[('face.detect','detector'),('mask.person','person')])
def test_native_composed_admission_exact_then_conflict(db,operation,stage):
    packet,case,_ = seed_native_anchor(db,operation,stage)
    tenant,project,actor = case['Tenant'],case['Project'],case['Actor']
    run = UUID(packet['run_id'])
    workflow = uuid5(run,'workflow')
    task = uuid5(workflow,operation+':0')
    params = packet['registration']['params']
    rows = packet['registration']['transport']['rows']
    profile = dict(backend='cuda',renderer_version='v2.2',resource_estimate=dict(ram_bytes=1073741824,vram_bytes=1073741824,scratch_bytes=1048576,output_bytes=1048576,pixel_count=64))
    policy_sha = sha256(packet['policy_text'].encode()).hexdigest()
    config = json.loads(packet['config_text'])
    config['v27_'+stage+'_prepare'].update(params_document=packet['params_document_id'],params=params,task_id=str(task),input_digest='d'*64,policy_sha=policy_sha)
    packet['config_text'] = canonical_bytes(config).decode()
    config_sha = sha256(packet['config_text'].encode()).hexdigest()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'fail_fast')",(run,tenant,case['Snapshot'],Jsonb(config),config_sha,actor))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,tenant,project,run,case['Snapshot'],policy_sha))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,%s,'1','gpu','[]',%s,%s,%s)",(task,tenant,run,operation+':0',operation,params['artifact_id'],'d'*64,Jsonb(profile)))
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,16777216,0,clock_timestamp()+interval '1 hour')",(tenant,actor,run))
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1073741824'::jsonb) WHERE id=%s",(tenant,))
    db.execute('UPDATE photo.tenant_usage SET reserved_bytes=16777216 WHERE tenant_id=%s',(tenant,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    def publish(purpose,digest):
        result = db.execute("SELECT * FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,128,'test')",(tenant,run,task,purpose,digest)).fetchone()
        assert result[0]=='created'
        assert db.execute("SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,128,'test','v1')",(tenant,run,task,purpose,result[1],result[2],digest)).fetchone()[0]
        return dict(artifact_id=str(result[1]),sha256=digest)
    chunk,manifest = publish('v2_input_chunk_0','e'*64),publish('v2_input_manifest','f'*64)
    direct = [params,packet['binding']['source'],packet['files'][2]['Ref']]
    assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)',(tenant,run,task,Jsonb(rows))).fetchone()[0]==3
    assert db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,3,%s,%s)',(tenant,run,task,Jsonb(manifest),Jsonb([chunk]),'9'*64,Jsonb(direct))).fetchone()[0]
    packet['registration'] = dict(schema_version=1,task_id=str(task),params=params,input_digest='d'*64,profile=profile,transport=dict(schema_version=1,rows=rows,manifest=manifest,chunks=[chunk],direct=direct,count=3,entries_sha='9'*64))
    def lock(candidate=packet):
        return db.execute('SELECT photo.lock_semantic_native_admission_evidence(%s,%s,%s)',(tenant,run,Jsonb(candidate))).fetchone()[0]
    assert lock()
    def recover(requested_task=task,requested_actor=actor):
        return db.execute('SELECT photo.semantic_native_registered_transport(%s,%s,%s,%s,%s)',(tenant,run,requested_task,requested_actor,case['Session'])).fetchone()[0]
    # Native output discovery must not turn a registered or queued task into
    # completed producer evidence. API-only ACL applies even to empty results.
    signature='photo.semantic_native_detector_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid)'
    for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] == (role=='photo_api')
    db.execute('SET LOCAL ROLE photo_api')
    assert not db.execute('SELECT * FROM photo.semantic_native_detector_result_catalog(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,actor,case['Session'],packet['parent_run_id'],packet['request_sha'],task)).fetchall()
    db.execute('SET LOCAL ROLE photo_scheduler')
    retained = dict(schema_version=1,task_id=str(task),manifest=manifest,chunks=[chunk],count=3,entries_sha='9'*64)
    assert recover()==retained
    assert recover(requested_task=uuid5(run,'foreign-task')) is None
    assert recover(requested_actor=str(uuid5(run,'foreign-actor'))) is None
    for mutation in ['extra','params_document','input_digest','source_version','qualification','quota_profile']:
        changed = deepcopy(packet)
        if mutation=='extra': changed['extra']=True
        elif mutation=='params_document': changed['params_document_id']=str(uuid5(run,'foreign'))
        elif mutation=='input_digest': changed['registration']['input_digest']='8'*64
        elif mutation=='source_version': changed['source_pins'][1]['version']='changed'
        elif mutation=='qualification': changed['qualification']['version']='changed'
        elif mutation=='quota_profile': changed['registration']['profile']['resource_estimate']['output_bytes']=16777216
        assert not lock(changed),mutation
    assert lock()
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('draft',False)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]=='blocked'

    admission_key = stage+'-prep:'+str(run)
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,resource_id,expires_at) VALUES(%s,'user',%s,'V2.Admit',%s,%s,%s,clock_timestamp()+interval '1 hour')",(tenant,actor,admission_key,packet['request_sha'],run))
    db.execute('UPDATE photo.operation_registry SET v2_enabled=true WHERE operation=%s AND version=%s',(operation,'1'))
    db.execute('SET LOCAL ROLE photo_scheduler')
    def seal(candidate=packet):
        return db.execute('SELECT photo.seal_semantic_native_preparation(%s,%s,%s)',(tenant,run,Jsonb(candidate))).fetchone()[0]
    db.execute('RESET ROLE')
    db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991)',(tenant,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    with pytest.raises(errors.CheckViolation):
        with db.transaction():
            seal()
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('draft',False)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]=='blocked'
    assert db.execute("SELECT response_status FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s",(tenant,admission_key)).fetchone()[0] is None
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s",(tenant,str(run))).fetchone()[0]==0
    db.execute('UPDATE photo.tenant_event_cursors SET last_cursor=0 WHERE tenant_id=%s',(tenant,))
    for role in ['photo_api','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance','photo_semantic_admission']:
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                seal()
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                recover()
    db.execute('SET LOCAL ROLE photo_scheduler')
    ticket = seal()
    assert ticket == dict(run_id=str(run),workflow_id=str(workflow),state='queued')
    assert seal() == ticket
    assert recover()==retained
    changed = deepcopy(packet)
    changed['registration']['input_digest']='8'*64
    assert seal(changed) is None
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('queued',True)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]=='ready'
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'",(tenant,run)).fetchone()[0]==1
    receipt = db.execute("SELECT response_status,response_body FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s",(tenant,admission_key)).fetchone()
    assert receipt==(202,ticket)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='v2.run.queued' AND payload->>'run_id'=%s",(tenant,str(run))).fetchone()[0]==1

    if operation=='face.detect':
        # Synthetic completed output metadata exercises the positive catalog;
        # this does not claim actual SFD inference or FaceBoxes byte validity.
        output_id,worker,key = uuid4(),uuid4(),str(uuid4())
        output = dict(artifact_id=str(output_id),sha256='7'*64)
        db.execute("UPDATE photo.runs SET status='running' WHERE id=%s",(run,))
        db.execute("UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s",(task,))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cuda',%s)",(tenant,task,worker,Jsonb(dict(backend='cuda'))))
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'test',%s,%s,128,'application/json','v1',clock_timestamp(),'face_boxes',%s,1,1)",(output_id,tenant,key,output['sha256'],task))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,1,%s,%s,%s,%s,'face_boxes','test',%s,%s,128,'application/json','ready','v1',clock_timestamp())",(tenant,task,worker,'a'*64,uuid4(),output_id,key,output['sha256']))
        result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([output]),'6'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        args=[tenant,project,actor,case['Session'],packet['parent_run_id'],packet['request_sha'],task]
        def detector_catalog(candidate=None):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                result_rows=db.execute('SELECT * FROM photo.semantic_native_detector_result_catalog(%s,%s,%s,%s,%s,%s,%s)',candidate or args).fetchall()
                db.execute('RESET ROLE')
                return result_rows
        assert detector_catalog()==[(result,output,params)]
        lock_signature='photo.lock_semantic_native_detector_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)'
        for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,lock_signature)).fetchone()[0] == (role=='photo_scheduler')
        files=[]
        for role,ref in [('detector_params',params),('face_boxes',output)]:
            bucket,key,version,size,media=db.execute('SELECT bucket,object_key,object_version,bytes,media_type FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,ref['artifact_id'])).fetchone()
            files.append(dict(role=role,**ref,bucket=bucket,key=key,version=version,bytes=size,media_type=media))
        expected_result=dict(schema_version=1,result_id=str(result),output=output,params=params,files=files)
        def lock_detector(candidate=expected_result):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                locked=db.execute('SELECT photo.lock_semantic_native_detector_result(%s,%s,%s,%s,%s,%s,%s,%s)',tuple(args)+(Jsonb(candidate),)).fetchone()[0]
                db.execute('RESET ROLE')
                return locked
        assert lock_detector()
        for field in ['result_id','output','params','files','extra']:
            altered=deepcopy(expected_result)
            if field=='result_id':altered[field]=str(uuid4())
            elif field in ('output','params'):altered[field]['sha256']='8'*64
            elif field=='files':altered[field][0]['version']='changed'
            else:altered[field]=True
            assert not lock_detector(altered)

        for index,value in [(0,str(uuid4())),(1,str(uuid4())),(2,str(uuid4())),(3,str(uuid4())),(4,str(uuid4())),(5,'8'*64),(6,str(uuid4()))]:
            changed=list(args);changed[index]=value
            assert not detector_catalog(changed)
        for query,parameters in [
            ("UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=%s",(output_id,)),
            ("UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s",(output_id,)),
            ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",(task,)),
            ("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",(run,)),
            ("UPDATE photo.task_attempts SET backend='cpu' WHERE task_id=%s",(task,)),
            ("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s",(task,)),
        ]:
            db.execute('SAVEPOINT detector_catalog_mutation')
            db.execute(query,parameters)
            assert not detector_catalog()
            db.execute('ROLLBACK TO SAVEPOINT detector_catalog_mutation')
            db.execute('RELEASE SAVEPOINT detector_catalog_mutation')
            assert detector_catalog()==[(result,output,params)]

    else:
        # LRASPP publishes four separately verified outputs, not a document root.
        db.execute("UPDATE photo.runs SET status='running' WHERE id=%s",(run,))
        db.execute("UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s",(task,))
        worker=uuid4()
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cuda',%s)",(tenant,task,worker,Jsonb(dict(backend='cuda'))))
        outputs=[]
        for role,media in [('person_mask','application/octet-stream'),('person_mask_png','image/png'),('person_mask_ref','application/json'),('person_report','application/json')]:
            aid,key=uuid4(),str(uuid4());ref=dict(artifact_id=str(aid),sha256='7'*64);outputs.append(ref)
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'test',%s,%s,128,%s,'v1',clock_timestamp(),%s,%s,1,1)",(aid,tenant,key,ref['sha256'],media,role,task))
            db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'test',%s,%s,128,%s,'ready','v1',clock_timestamp())",(tenant,task,worker,'a'*64,uuid4(),aid,role,key,ref['sha256'],media))
        ready=lambda refs: db.execute('SELECT photo.semantic_native_preparation_result_ready(%s,%s,1,1,%s)',(tenant,task,Jsonb(refs))).fetchone()[0]
        assert ready(outputs)
        for bad in [outputs[:1],list(reversed(outputs)),[outputs[0]]*4,outputs+[outputs[0]]]:
            assert not ready(bad)
            with pytest.raises(errors.CheckViolation,match='V27_NATIVE_PREPARATION_OUTPUTS_UNVERIFIED'):
                with db.transaction():
                    db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",(tenant,task,Jsonb(bad),'6'*64))
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb(outputs),'6'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        assert ready(outputs)
        for output in outputs:
            db.execute('SAVEPOINT person_output_mutation')
            db.execute('UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=%s',(output['artifact_id'],))
            assert not ready(outputs)
            db.execute('ROLLBACK TO SAVEPOINT person_output_mutation')
            db.execute('RELEASE SAVEPOINT person_output_mutation')
        for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
            assert not db.execute("SELECT has_function_privilege(%s,'photo.semantic_native_preparation_result_ready(uuid,uuid,integer,bigint,jsonb)','EXECUTE')",(role,)).fetchone()[0]
