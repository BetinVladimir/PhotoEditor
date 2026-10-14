"""Read-only adoption boundary privileges and nonexistent source refusal."""
from uuid import UUID,uuid4
import psycopg
import pytest


def test_derivative_source_scope_is_scheduler_only(database):
    with psycopg.connect(**database) as conn:
        for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
            granted=conn.execute("SELECT has_function_privilege(%s,'photo.v26_render_derivative_source(uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]
            assert granted==(role=='photo_scheduler')
            assert conn.execute("SELECT has_function_privilege(%s,'photo.enqueue_next_v26_render_derivative(text)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')
        conn.execute('SET ROLE photo_scheduler')
        assert conn.execute('SELECT * FROM photo.v26_render_derivative_source(%s,%s)',(uuid4(),uuid4())).fetchall()==[]


def test_derivative_source_requires_current_adoption_and_live_scope(db):
    from uuid import uuid5
    from psycopg.types.json import Jsonb
    from test_v26_render_finalizer_tenant_scope import seed_failed_batch
    tenant,batch=seed_failed_batch(db)
    child,task,actor,project,snapshot=db.execute("SELECT r.id,t.id,r.created_by,(r.config_snapshot->>'project_id')::uuid,r.input_snapshot_id FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id AND t.tenant_id=r.tenant_id WHERE r.tenant_id=%s AND r.kind='v2'",(tenant,)).fetchone()
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cuda','{}')",(tenant,task,uuid4()))
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
    from photo_worker.operations.optical.contracts import ImageResult
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.operations.media.derivative import media_profile_hash
    from hashlib import sha256
    pixel_ref=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    image_model=WorkingImage(pixels=pixel_ref,alpha=None,dtype='float32',canvas=Canvas(width=5,height=5,orientation=1,space_id='result',source_sha256='d'*64),color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='e'*64),transform_chain=TransformChain.create([]))
    image_bytes=canonical_bytes(image_model)
    image_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(image_bytes).hexdigest())
    result_model=ImageResult(schema_version=1,working_image=image_ref,report=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),changed_mask=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),generated_mask=None)
    root_bytes=canonical_bytes(result_model);root_sha=sha256(root_bytes).hexdigest()
    root=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,%s,'application/json','document',now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),root_sha,len(root_bytes),task)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET execution_profile='{\"renderer_version\":\"v2.2\"}' WHERE id=%s",(task,))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(uuid5(child,'workflow'),tenant,project,child,snapshot,root_sha))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,'document',bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(task,uuid4(),root_sha,uuid4(),root))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([dict(artifact_id=str(root),sha256=root_sha)]),'c'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s,terminal_reason=NULL WHERE id=%s",(result,task))
    db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",(child,))
    def source():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT * FROM photo.v26_render_derivative_source(%s,%s)',(tenant,task)).fetchall()
        finally:db.execute('RESET ROLE')
    assert source()==[]
    def produce(profile='b'*64):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT photo.enqueue_next_v26_render_derivative(%s)',(profile,)).fetchone()[0]
        finally:db.execute('RESET ROLE')
    assert produce() is False
    marker=db.execute('SELECT photo.v2_adoption_event_id(%s,%s)',(task,result)).fetchone()[0]
    receipt=db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',%s,now()) RETURNING id",(tenant,marker)).fetchone()[0]
    assert source()==[]
    assert produce() is False
    event=db.execute("INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(%s,1,%s,%s,'principal','v2.task.adopted',%s) RETURNING id",(tenant,child,actor,Jsonb(dict(task_id=str(task),result_id=str(result))))).fetchone()[0]
    expected=[(project,child,result,root,root_sha,1,1)]
    assert source()==expected
    def presentation(principal=actor,session_override=None,scope=tenant):
        session=db.execute("SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s",(child,)).fetchone()[0]
        db.execute('SET LOCAL ROLE photo_api')
        try:return db.execute('SELECT * FROM photo.v26_render_presentation_status(%s,%s,%s,%s)',(principal,session if session_override is None else session_override,scope,child)).fetchall()
        finally:db.execute('RESET ROLE')
    assert presentation()==[(1,1,0,0,0,0)]
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(root,))
        assert presentation()==[(1,0,0,0,0,1)]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.projects SET archived_at=now() WHERE id=%s',(project,))
        assert presentation()==[]
    mutations=[
        ("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s",task),
        ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",task),
        ("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",child),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",root),
        ("UPDATE photo.projects SET archived_at=now() WHERE id=%s",project),
        ("UPDATE photo.tenants SET status='deleting' WHERE id=%s",tenant),
        ("DELETE FROM photo.inbox WHERE id=%s",receipt),
        ("UPDATE photo.run_events SET payload=jsonb_set(payload,'{result_id}',to_jsonb(%s::text)) WHERE id='"+str(event)+"'",uuid4()),
    ]
    for sql,identity in mutations:
        with db.transaction(force_rollback=True):
            db.execute(sql,(identity,));assert source()==[]
        assert source()==expected

    def enqueue(result_id=result,profile='b'*64):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT photo.enqueue_v26_render_derivative(%s,%s,%s,%s)',(tenant,task,result_id,profile)).fetchone()[0]
        finally:db.execute('RESET ROLE')
    assert enqueue(uuid4()) is None
    assert produce('invalid') is False
    assert produce() is True and produce() is False
    intent=enqueue();assert intent is not None and enqueue()==intent
    assert enqueue(profile='c'*64) is None
    stored=db.execute('SELECT kind,status,graph_sealed,created_by,input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(intent,)).fetchone()
    assert stored[:5]==('preview','draft',False,actor,snapshot)
    assert stored[5]['source_result_id']==str(result) and stored[5]['root']['artifact_id']==str(root)
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(intent,)).fetchone()==(0,)
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,))
        assert enqueue() is None
    assert enqueue()==intent

    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.enqueue_v26_render_derivative(uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')
    with pytest.raises(psycopg.errors.InsufficientPrivilege,match='immutable'),db.transaction():
        db.execute("UPDATE photo.runs SET config_snapshot='{}' WHERE id=%s",(intent,))
    assert enqueue()==intent
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(intent,))
        assert enqueue() is None

    def next_intent(after=None):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT * FROM photo.next_v26_render_derivative(%s)',(after,)).fetchall()
        finally:db.execute('RESET ROLE')
    assert next_intent()==[(UUID(str(tenant)),intent,project,task,result,root,root_sha,1,1,'b'*64)]
    assert next_intent(intent)==[]
    def derivative_artifact(scope=tenant,artifact=root,digest=root_sha):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT * FROM photo.v26_render_derivative_artifact(%s,%s,%s,%s)',(scope,intent,artifact,digest)).fetchall()
        finally:db.execute('RESET ROLE')
    metadata=derivative_artifact()
    assert len(metadata)==1 and metadata[0][0]==root and metadata[0][3]=='v1' and metadata[0][4]==root_sha and metadata[0][5]==len(root_bytes)
    assert derivative_artifact(scope=uuid4())==[]
    assert derivative_artifact(artifact=uuid4())==[]
    assert derivative_artifact(digest='0'*64)==[]
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v26_render_derivative_artifact(uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')
    for sql,identity in [("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",intent),("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",child),("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",root)]:
        with db.transaction(force_rollback=True):
            db.execute(sql,(identity,));assert next_intent()==[]
            assert derivative_artifact()==[]
    def cancel_stale():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT photo.cancel_stale_v26_render_derivative()').fetchone()[0]
        finally:db.execute('RESET ROLE')
    assert cancel_stale() is False
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.runs SET created_at=now()-interval '2 hours' WHERE id=%s",(intent,))
        assert cancel_stale() is True
        assert next_intent()==[]
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(root,))
        assert cancel_stale() is True and cancel_stale() is False
        assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(intent,)).fetchone()==('cancelling',True)
        assert next_intent()==[]
    assert len(next_intent())==1
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.next_v26_render_derivative(uuid)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')

    def reserve():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT photo.reserve_v26_render_derivative(%s,%s)',(tenant,intent)).fetchone()[0]
        finally:db.execute('RESET ROLE')
    assert reserve() is False  # Controlled fixture has no usage row until accounting is seeded.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tenant,))
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1000000000') WHERE id=%s",(tenant,))
    before=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0') WHERE id=%s",(tenant,))
        assert reserve() is False
        assert db.execute('SELECT count(*) FROM photo.quota_reservations WHERE holder_id=%s',(intent,)).fetchone()==(0,)
    assert reserve() is True and reserve() is True
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(before+111149056,)
    assert db.execute("SELECT count(*),sum(bytes) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,intent)).fetchone()==(1,111149056)
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.quota_reservations SET expires_at=now()-interval '1 second' WHERE holder_id=%s",(intent,))
        assert reserve() is False
        assert cancel_stale() is True and cancel_stale() is False
        assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE holder_id=%s",(intent,)).fetchone()==('reserved',111149056)
    assert cancel_stale() is False
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,))
        assert reserve() is False

    # Controlled adopted child metadata; pin/retry remains SQL-only without object publication.
    with db.transaction(force_rollback=True):
        for ref,role,body in [(pixel_ref,'pixels',b'fixture-pixels'),(image_ref,'working_image',image_bytes)]:
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,'test',%s,'v1',%s,%s,'application/json',%s,now())",(ref.artifact_id,tenant,str(ref.artifact_id),ref.sha256,len(body),role))
            db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,%s,0)",(tenant,project,uuid5(child,'workflow'),ref.artifact_id,ref.sha256,sha256(role.encode()).hexdigest()))
        params=canonical_bytes(dict(image=image_ref.model_dump(mode='json'),variant='preview1600',profile_hash=media_profile_hash('preview1600','b'*64),source_kind='working',suffix=None,matte_rgb=None)).decode()
        def pin(bucket='test',body=params):
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.pin_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s)',(tenant,intent,'preview1600',root_bytes.decode(),image_bytes.decode(),body,bucket)).fetchone()[0]
            finally:db.execute('RESET ROLE')
        pinned=pin();assert pinned is not None and pin()==pinned
        assert pin('another-bucket') is None
        assert pin(body=params.replace('working','master')) is None
        assert db.execute('SELECT v26_derivative_params,bytes FROM photo.backend_documents WHERE artifact_id=%s',(pinned,)).fetchone()==(params,len(params.encode()))
        assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(before+111149056+len(params.encode()),)
        owner,token,params_sha,params_size=db.execute('SELECT owner_id,token,sha256,bytes FROM photo.backend_documents WHERE artifact_id=%s',(pinned,)).fetchone()
        def ticket(owner_id=owner):
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT * FROM photo.reserve_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,intent,owner_id,'preview1600',params_sha,params_size,'test',root_bytes.decode(),image_bytes.decode(),params)).fetchall()
            finally:db.execute('RESET ROLE')
        assert ticket(uuid4())==[]
        assert ticket()==[(pinned,token,False)]
        storage_before=db.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
        def complete(fence_token=token,version='immutable-v1'):
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.complete_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,intent,owner,pinned,fence_token,params_sha,params_size,'test',version,root_bytes.decode(),image_bytes.decode())).fetchone()[0]
            finally:db.execute('RESET ROLE')
        assert complete(uuid4()) is False
        assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(pinned,)).fetchone()==(0,)
        assert complete() is True and complete() is True
        task_inputs=[image_ref.model_dump(mode='json'),pixel_ref.model_dump(mode='json')]
        params_ref=dict(artifact_id=str(pinned),sha256=params_sha)
        digest=sha256(canonical_bytes(dict(inputs=task_inputs,operation='media.preview',operation_version='1',params=params_ref,version='task_input_v1'))).hexdigest()
        task_plan=dict(id=str(owner),node_key='render.derivative:preview1600',operation='media.preview',operation_version='1',resource_class='media',timeout_seconds=300,params=params_ref,inputs=task_inputs,input_digest=digest,execution_profile=dict(backend='cpu',precision='float32',resource_estimate=dict(ram_bytes=25*24+25*96+134217728,vram_bytes=0,scratch_bytes=67108864+25*12,output_bytes=54525952,pixel_count=25)))
        def materialize(plan=task_plan):
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.materialize_v26_render_derivative_task(%s,%s,%s,%s,%s,%s)',(tenant,intent,'preview1600',root_bytes.decode(),image_bytes.decode(),Jsonb(plan))).fetchone()[0]
            finally:db.execute('RESET ROLE')
        import copy
        for field,value in [('input_digest','0'*64),('timeout_seconds',301),('inputs',list(reversed(task_inputs)))]:
            bad=copy.deepcopy(task_plan);bad[field]=value;assert materialize(bad) is False
        bad=copy.deepcopy(task_plan);bad['execution_profile']['resource_estimate']['ram_bytes']-=1
        assert materialize(bad) is False
        assert materialize() is True and materialize() is True
        assert db.execute('SELECT state,inputs,params_artifact_id FROM photo.tasks WHERE id=%s',(owner,)).fetchone()==('blocked',task_inputs,pinned)
        assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(intent,)).fetchone()==(1,)
        def seal():
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.seal_v26_render_derivative(%s,%s,%s,%s)',(tenant,intent,root_bytes.decode(),image_bytes.decode())).fetchone()[0]
            finally:db.execute('RESET ROLE')
        assert seal() is False
        with db.transaction(force_rollback=True):
            params2=canonical_bytes(dict(image=image_ref.model_dump(mode='json'),variant='preview2560',profile_hash=media_profile_hash('preview2560','b'*64),source_kind='working',suffix=None,matte_rgb=None)).decode()
            db.execute('SET LOCAL ROLE photo_scheduler')
            pinned2=db.execute('SELECT photo.pin_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s)',(tenant,intent,'preview2560',root_bytes.decode(),image_bytes.decode(),params2,'test')).fetchone()[0]
            db.execute('RESET ROLE')
            owner2,token2,sha2,size2=db.execute('SELECT owner_id,token,sha256,bytes FROM photo.backend_documents WHERE artifact_id=%s',(pinned2,)).fetchone()
            plan2=copy.deepcopy(task_plan);plan2.update(id=str(owner2),node_key='render.derivative:preview2560',params=dict(artifact_id=str(pinned2),sha256=sha2))
            plan2['input_digest']=sha256(canonical_bytes(dict(inputs=task_inputs,operation='media.preview',operation_version='1',params=plan2['params'],version='task_input_v1'))).hexdigest()
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.complete_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,intent,owner2,pinned2,token2,sha2,size2,'test','immutable-v1',root_bytes.decode(),image_bytes.decode())).fetchone()[0] is True
            assert db.execute('SELECT photo.materialize_v26_render_derivative_task(%s,%s,%s,%s,%s,%s)',(tenant,intent,'preview2560',root_bytes.decode(),image_bytes.decode(),Jsonb(plan2))).fetchone()[0] is True
            db.execute('RESET ROLE')
            with db.transaction(force_rollback=True):
                db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,))
                assert seal() is False
                assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s AND state='blocked'",(intent,)).fetchone()==(2,)
            revision=db.execute('SELECT revision FROM photo.runs WHERE id=%s',(intent,)).fetchone()[0]
            assert seal() is True and seal() is True
            assert db.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s',(intent,)).fetchone()==('running',True,revision+1)
            assert derivative_artifact()==metadata
            assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s AND state='ready'",(intent,)).fetchone()==(2,)
            with db.transaction(force_rollback=True):
                worker=uuid4();outputs=[]
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(tenant,owner,worker))
                for role,mime in [('derivative','image/jpeg'),('derivative_manifest','application/json')]:
                    digest_out=sha256(role.encode()).hexdigest();output_size=42
                    if role=='derivative_manifest':
                        result_manifest=canonical_bytes(dict(source=image_ref.model_dump(mode='json'),derivative=outputs[0],variant='preview1600',profile_hash=media_profile_hash('preview1600','b'*64),width=5,height=5,media_type='image/jpeg',color_profile='srgb',metadata_policy='stripped',alpha=False)).decode()
                        digest_out=sha256(result_manifest.encode()).hexdigest();output_size=len(result_manifest.encode())
                    output_id=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'out-v1',%s,%s,%s,%s,now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),digest_out,output_size,mime,role,owner)).fetchone()[0]
                    outputs.append(dict(artifact_id=str(output_id),sha256=digest_out))
                    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,%s,bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(owner,worker,'c'*64,uuid4(),role,output_id))
                result_out=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,owner,Jsonb(outputs),'a'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result_out,owner))
                def result_metadata(scope=tenant):
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute('SELECT * FROM photo.v26_render_derivative_result(%s,%s,%s)',(scope,intent,'preview1600')).fetchall()
                    finally:db.execute('RESET ROLE')
                result_rows=result_metadata()
                assert len(result_rows)==2 and [row[5] for row in result_rows]==['derivative','derivative_manifest']
                assert all(row[:4]==(owner,result_out,1,1) for row in result_rows)
                db.execute('SET LOCAL ROLE photo_scheduler')
                context_rows=db.execute('SELECT * FROM photo.v26_render_derivative_context(%s,%s,%s)',(tenant,intent,'preview1600')).fetchall()
                db.execute('RESET ROLE')
                assert context_rows==[(owner,result_out,1,1,pinned,params_sha,UUID(outputs[1]['artifact_id']),outputs[1]['sha256'])]
                def evidence(identity,result_identity=result_out):
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute('SELECT * FROM photo.v26_render_derivative_evidence(%s,%s,%s,%s,1,1,%s)',(tenant,intent,'preview1600',result_identity,identity)).fetchall()
                    finally:db.execute('RESET ROLE')
                assert evidence(pinned)[0][6:]==('task_params','input')
                assert evidence(image_ref.artifact_id)[0][6:]==('working_image','input')
                assert evidence(UUID(outputs[0]['artifact_id']))[0][6:]==('derivative','output')
                assert evidence(uuid4())==[] and evidence(pinned,uuid4())==[]
                assert result_metadata(uuid4())==[]
                def record_receipt(attempt=1):
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute('SELECT photo.record_v26_render_derivative_receipt(%s,%s,%s,%s,%s,1,%s)',(tenant,intent,'preview1600',result_out,attempt,result_manifest)).fetchone()[0]
                    finally:db.execute('RESET ROLE')
                def pending_receipts():
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute('SELECT * FROM photo.pending_v26_render_derivative_receipts(NULL)').fetchall()
                    finally:db.execute('RESET ROLE')
                assert pending_receipts()==[(UUID(str(tenant)),intent,'preview1600',owner)]
                assert record_receipt(2) is False
                with db.transaction(force_rollback=True):
                    def reject_result(fence=1,code='RENDER_DERIVATIVE_ENCODING_INVALID'):
                        db.execute('SET LOCAL ROLE photo_scheduler')
                        try:return db.execute('SELECT photo.reject_v26_render_derivative(%s,%s,%s,%s,1,%s,%s)',(tenant,intent,'preview1600',result_out,fence,code)).fetchone()[0]
                        finally:db.execute('RESET ROLE')
                    assert reject_result(2) is False and reject_result(code='offline') is False
                    assert reject_result() is True and reject_result() is True
                    assert db.execute('SELECT state,canonical_result_id,terminal_reason FROM photo.tasks WHERE id=%s',(owner,)).fetchone()==('failed',result_out,'RENDER_DERIVATIVE_ENCODING_INVALID')
                assert record_receipt() is True and record_receipt() is True
                assert pending_receipts()==[]
                receipt_saved=db.execute('SELECT v26_derivative_receipt FROM photo.backend_documents WHERE artifact_id=%s',(pinned,)).fetchone()[0]
                assert receipt_saved['result_id']==str(result_out) and receipt_saved['manifest']==result_manifest
                def finalize_success():
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute('SELECT photo.finalize_successful_v26_render_derivative()').fetchone()[0]
                    finally:db.execute('RESET ROLE')
                assert finalize_success() is False
                with db.transaction(force_rollback=True):
                    outputs2=[];worker2=uuid4()
                    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(tenant,owner2,worker2))
                    for role,mime in [('derivative','image/jpeg'),('derivative_manifest','application/json')]:
                        digest2=sha256((role+'2560').encode()).hexdigest();size_out=42
                        if role=='derivative_manifest':
                            manifest2=canonical_bytes(dict(source=image_ref.model_dump(mode='json'),derivative=outputs2[0],variant='preview2560',profile_hash=media_profile_hash('preview2560','b'*64),width=5,height=5,media_type='image/jpeg',color_profile='srgb',metadata_policy='stripped',alpha=False)).decode()
                            digest2=sha256(manifest2.encode()).hexdigest();size_out=len(manifest2.encode())
                        artifact2=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'out-v1',%s,%s,%s,%s,now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),digest2,size_out,mime,role,owner2)).fetchone()[0]
                        outputs2.append(dict(artifact_id=str(artifact2),sha256=digest2))
                        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,%s,bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(owner2,worker2,'c'*64,uuid4(),role,artifact2))
                    result2=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,owner2,Jsonb(outputs2),'b'*64)).fetchone()[0]
                    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result2,owner2))
                    assert finalize_success() is False
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    assert db.execute('SELECT photo.record_v26_render_derivative_receipt(%s,%s,%s,%s,1,1,%s)',(tenant,intent,'preview2560',result2,manifest2)).fetchone()==(True,)
                    db.execute('RESET ROLE')
                    with db.transaction(force_rollback=True):
                        db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(root,))
                        assert finalize_success() is False
                    assert finalize_success() is True and finalize_success() is False
                    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(intent,)).fetchone()==('completed',revision+2)
                    assert presentation()==[(1,0,1,0,0,0)]
                    auth_session=db.execute("SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s",(child,)).fetchone()[0]
                    def api_outputs(session=auth_session,principal=actor,scope=tenant):
                        db.execute('SET LOCAL ROLE photo_api')
                        try:return db.execute('SELECT * FROM photo.v26_render_derivative_outputs(%s,%s,%s,%s)',(principal,session,scope,child)).fetchall()
                        finally:db.execute('RESET ROLE')
                    delivered=api_outputs()
                    assert len(delivered)==4 and {row[1] for row in delivered}=={'derivative','derivative_manifest'}
                    assert api_outputs(uuid4())==[]
                    for cancellation_sql,identity in [
                        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',child),
                        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',task),
                        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',intent),
                    ]:
                        with db.transaction(force_rollback=True):
                            db.execute(cancellation_sql,(identity,))
                            assert api_outputs()==[]
                            assert presentation()==[(1,0,0,0,1,0)]
                    with db.transaction(force_rollback=True):
                        viewer=db.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)",(str(uuid4())+'@test.example',)).fetchone()[0]
                        viewer_session=uuid4()
                        db.execute("INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,%s,'native',now()+interval '1 hour')",(viewer_session,viewer,sha256(str(uuid4()).encode()).digest(),uuid4()))
                        db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(%s,%s,'viewer','active')",(tenant,viewer))
                        db.execute("UPDATE photo.projects SET visibility='restricted' WHERE id=%s",(project,))
                        assert api_outputs(viewer_session,viewer)==[]
                        assert presentation(viewer,viewer_session)==[]
                        db.execute("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES(%s,%s,%s,'viewer')",(tenant,project,viewer))
                        assert len(api_outputs(viewer_session,viewer))==4
                        assert presentation(viewer,viewer_session)==[(1,0,1,0,0,0)]
                        viewer_tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(viewer,)).fetchone()[0]
                        assert api_outputs(viewer_session,viewer,viewer_tenant)==[]
                        assert presentation(viewer,viewer_session,viewer_tenant)==[]
                        db.execute('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(viewer_session,))
                        assert api_outputs(viewer_session,viewer)==[]
                        assert presentation(viewer,viewer_session)==[]
                    with db.transaction(force_rollback=True):
                        db.execute('UPDATE photo.projects SET archived_at=now() WHERE id=%s',(project,))
                        assert api_outputs()==[]
                    with db.transaction(force_rollback=True):
                        db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(root,))
                        assert api_outputs()==[]
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    assert db.execute('SELECT photo.release_v26_render_derivative_quota(%s,%s)',(tenant,intent)).fetchone()==(True,)
                    db.execute('RESET ROLE')
                with pytest.raises(psycopg.errors.InsufficientPrivilege,match='immutable'),db.transaction():
                    db.execute("UPDATE photo.backend_documents SET v26_derivative_receipt='{}' WHERE artifact_id=%s",(pinned,))
                for sql,arg in [("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s",owner),("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",UUID(outputs[1]['artifact_id'])),("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",child)]:
                    with db.transaction(force_rollback=True):
                        db.execute(sql,(arg,));assert result_metadata()==[]
            def finalize_failure():
                db.execute('SET LOCAL ROLE photo_scheduler')
                try:return db.execute('SELECT photo.finalize_failed_v26_render_derivative()').fetchone()[0]
                finally:db.execute('RESET ROLE')
            assert finalize_failure() is False
            db.execute("UPDATE photo.tasks SET state='failed',terminal_reason='fixture_failure' WHERE id=%s",(owner,))
            assert finalize_failure() is False
            db.execute("UPDATE photo.tasks SET state='cancelled',terminal_reason='fixture_cancel' WHERE id=%s",(owner2,))
            assert finalize_failure() is True and finalize_failure() is False
            assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(intent,)).fetchone()==('failed',revision+2)
            assert presentation()==[(1,0,0,1,0,0)]
            assert derivative_artifact()==[]
            with db.transaction(force_rollback=True):
                db.execute("UPDATE photo.backend_documents SET state='pending',object_version=NULL,completed_at=NULL WHERE artifact_id=%s",(pinned2,))
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT photo.release_v26_render_derivative_quota(%s,%s)',(tenant,intent)).fetchone()==(False,)
                db.execute('RESET ROLE')
                assert db.execute("SELECT state FROM photo.quota_reservations WHERE holder_id=%s",(intent,)).fetchone()==('reserved',)
                db.execute("UPDATE photo.backend_documents SET state='cleaning' WHERE artifact_id=%s",(pinned2,))
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT photo.release_v26_render_derivative_quota(%s,%s)',(tenant,intent)).fetchone()==(False,)
                assert db.execute('SELECT photo.release_next_v26_render_derivative_quota()').fetchone()==(False,)
                db.execute('RESET ROLE')
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.release_v26_render_derivative_quota(%s,%s)',(tenant,intent)).fetchone()==(True,)
            db.execute('RESET ROLE')
            assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE holder_id=%s",(intent,)).fetchone()==('released',0)
        assert ticket()==[(pinned,token,True)]
        assert complete(version='immutable-v2') is False
        assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(storage_before+params_size,before+111149056)
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(root,))
            assert complete() is False
            assert materialize() is False
    def release():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT photo.release_v26_render_derivative_quota(%s,%s)',(tenant,intent)).fetchone()[0]
        finally:db.execute('RESET ROLE')
    assert release() is False
    # Match the manually seeded adoption event cursor before invoking the real cancellation finalizer.
    db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,1) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=greatest(photo.tenant_event_cursors.last_cursor,1)',(tenant,))
    db.execute("UPDATE photo.runs SET cancel_requested=true,status='cancelling' WHERE id=%s",(intent,))
    assert release() is False
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.release_next_v26_render_derivative_quota()').fetchone()==(True,)
    assert db.execute('SELECT photo.release_next_v26_render_derivative_quota()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert release() is True and release() is True
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(before,)
    assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE holder_id=%s",(intent,)).fetchone()==('released',0)


def test_derivative_object_reads_refuse_explicit_autocommit_transaction(database):
    from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
    from photo_worker.v2.render_derivative_store import PostgresRenderDerivativeReader
    class NoObjects:
        def get_object(self,*args,**kwargs):
            raise AssertionError('Object IO must not start inside a database transaction')
    with psycopg.connect(**database,autocommit=True) as connection:
        reader=PostgresRenderDerivativeReader(connection,NoObjects(),tenant_id=uuid4(),
            run_id=uuid4(),task_id=uuid4(),result_id=uuid4(),variant='preview1600',
            attempt=1,fence=1,allowed_bucket='test')
        ref=ArtifactRef(uuid4(),'a'*64)
        deferred=reader.iter_bytes(ref,1024)
        with connection.transaction():
            for read in (lambda:reader.read_json(ref,1024),lambda:next(deferred),
                         lambda:reader.evidence(ref),reader.staged_outputs):
                with pytest.raises(AdoptionInvalid,match='READ_TRANSACTION_ACTIVE'):
                    read()
        # Recovery uses a fresh observation after the caller exits the TX.
        with pytest.raises(AdoptionInvalid,match='RESULT_STALE'):
            reader.staged_outputs()


def test_derivative_stream_refuses_transaction_between_blocks(database):
    from io import BytesIO
    from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef,ArtifactEvidence
    from photo_worker.v2.adoption_store import _Location
    from photo_worker.v2.render_derivative_store import PostgresRenderDerivativeReader
    payload=b'x'*70000
    response=BytesIO(payload)
    class Objects:
        calls=0
        def get_object(self,*args,**kwargs):
            self.calls+=1
            return response
    objects=Objects()
    with psycopg.connect(**database,autocommit=True) as connection:
        reader=PostgresRenderDerivativeReader(connection,objects,tenant_id=uuid4(),
            run_id=uuid4(),task_id=uuid4(),result_id=uuid4(),variant='preview1600',
            attempt=1,fence=1,allowed_bucket='test')
        ref=ArtifactRef(uuid4(),'a'*64)
        # Isolate the transaction guard after a previously admitted immutable
        # location. SQL scope and object digest are covered by separate tests.
        reader._locations[ref]=_Location('test','encoded','v1',
            ArtifactEvidence(ref,'image/jpeg',len(payload),'derivative','output'))
        stream=reader.iter_bytes(ref,len(payload))
        assert next(stream)==payload[:65536] and objects.calls==1
        with connection.transaction():
            with pytest.raises(AdoptionInvalid,match='READ_TRANSACTION_ACTIVE'):
                next(stream)
        assert response.closed


@pytest.mark.parametrize('active_connection', ['read', 'write'])
def test_derivative_coordinator_refuses_outer_transaction(database, active_connection):
    from photo_worker.v2.adoption import AdoptionInvalid
    from photo_worker.v2.render_derivative_service import RenderDerivativeAdoptionCoordinator
    class NoObjects:
        def get_object(self, *args, **kwargs):
            raise AssertionError('Object IO before transaction guard')
    with psycopg.connect(**database, autocommit=True) as read:
        with psycopg.connect(**database, autocommit=True) as write:
            coordinator = RenderDerivativeAdoptionCoordinator(read, write, NoObjects(),
                'test', target_icc_sha256='a'*64, working_icc_sha256='b'*64)
            connection = read if active_connection == 'read' else write
            with connection.transaction():
                with pytest.raises(AdoptionInvalid, match='COORDINATOR_TRANSACTION_ACTIVE'):
                    coordinator.adopt(uuid4(), uuid4(), 'preview1600')
            # The guard accepts both idle connections after the caller exits.
            coordinator._require_idle_connections()
