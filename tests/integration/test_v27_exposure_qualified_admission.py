from hashlib import sha256
from uuid import uuid4


def qualified_adopted_fixture(db):
    from psycopg.types.json import Jsonb
    from test_v27_semantic_final_producer_gate import fixture
    from photo_worker.contracts.base import canonical_bytes
    tid,assembly,_,_,_,candidate,scope=fixture(db,snapshot_authority=True,protection_pins=True)
    parent=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    workflow=db.execute('SELECT id FROM photo.v2_workflows WHERE run_id=%s',(parent,)).fetchone()[0]
    intent=db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,scope['project'],workflow,scope['version'],scope['snapshot'],scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,scope['project'],intent,scope['version'],candidate)).fetchone()[0]
    child,task,params,worker=uuid4(),uuid4(),uuid4(),uuid4()
    candidate_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    def ref(aid,digest):return dict(artifact_id=str(aid),sha256=digest)
    edit=dict(schema_version=1,target_id=str(target),source=dict(asset_version_id=str(scope['version']),source=ref(scope['source'],'5'*64),canvas_hash='b'*64),candidate=ref(candidate,candidate_sha),intent=ref(scope['source'],'5'*64),mask=ref(uuid4(),'a'*64),transform=ref(uuid4(),'a'*64),kind='local_exposure',ev=.25,strength=.5,prompt=None,preserve_persons=True)
    for pin in (edit['mask'],edit['transform']):
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','v1',clock_timestamp())",(pin['artifact_id'],tid,pin['artifact_id'],pin['sha256']))
    raw=canonical_bytes(edit).decode();digest=sha256(raw.encode()).hexdigest()
    config=dict(project_id=str(scope['project']),approved_semantic=dict(nodes=[dict(task_id=str(task),params_artifact_id=str(params),params_sha=digest,target_id=str(target))]))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(child,tid,scope['snapshot'],Jsonb(config),'b'*64,scope['actor']))
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id) VALUES(%s,%s,%s,%s,%s,'apply',%s) RETURNING id",(tid,scope['project'],workflow,scope['source'],scope['actor'],child)).fetchone()[0]
    db.execute("UPDATE photo.semantic_targets SET state='queued',batch_id=%s,child_run_id=%s WHERE id=%s",(batch,child,target))
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json','v1',clock_timestamp())",(params,tid,str(params),digest,len(raw.encode())))
    from semantic_qualification_fixture import frozen_qualification
    pin=frozen_qualification(db,tid,scope['project'],scope['actor'],operation='semantic.local_exposure',backend='cpu')
    profile=dict(qualification=pin,model_lock_sha=pin['model_lock_sha'],runtime_lock_sha=pin['runtime_lock_sha'],device_profile_sha=pin['device_profile_sha'],dataset_sha=pin['dataset_sha'],backend='cpu',renderer_version='v2.2',model_lock_id='a'*64)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,state,worker_id,lease_until,deadline) VALUES(%s,%s,%s,'semantic.local_exposure:0','semantic.local_exposure','1','cpu','[]',%s,%s,%s,1,1,'running',%s,clock_timestamp()+interval '60 seconds',clock_timestamp()+interval '120 seconds')",(task,tid,child,params,'b'*64,Jsonb(profile),worker))

    staged=[]
    for ordinal,(role,media,size) in enumerate((('document','application/json',2),('working_image','application/json',2),('pixels','application/x-npy',800),('report','application/json',2))):
        aid=uuid4();sha_value=str(ordinal+1)*64
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'v1',clock_timestamp(),%s,%s,1,1)",(aid,tid,str(aid),sha_value,size,media,role,task))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,%s,'ready','v1')",(tid,task,worker,'a'*64,uuid4(),aid,role,str(aid),sha_value,size,media))
        staged.append(dict(artifact_id=str(aid),sha256=sha_value,purpose=str(ordinal+5)*64,ordinal=ordinal))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb([dict(artifact_id=staged[0]['artifact_id'],sha256=staged[0]['sha256'])]),'c'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,lease_until=NULL,worker_id=NULL WHERE id=%s",(result,task))
    args=(tid,task,1,1,result,params,digest,raw)
    def ready():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.v27_exposure_adoption_ready(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
            db.execute('RESET ROLE');return value
    assert db.execute('SELECT photo.semantic_approved_child_edit(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (tid,child,target,scope['version'],candidate,candidate_sha,scope['source'],'5'*64,scope['source'],'5'*64)).fetchone()[0] is True
    assert db.execute('SELECT photo.v27_downstream_profile_ready(%s,%s,%s,%s,%s,%s)',
        (tid,scope['project'],'semantic.local_exposure','1','cpu',Jsonb(profile))).fetchone()[0] is True
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        context=db.execute('SELECT * FROM photo.v27_exposure_adoption_context(%s,%s)',(tid,task)).fetchone()
        assert context[:5]==(1,1,result,'semantic.local_exposure','1')
        assert (tid,child,task) in db.execute('SELECT * FROM photo.v27_pending_exposure_adoptions()').fetchall()
        db.execute('RESET ROLE')
    assert ready() is True
    record=db.execute('SELECT photo.v27_exposure_adoption_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
    assert record['pin']==pin and record['policy']['operation']=='semantic.local_exposure'
    assert record['report']['artifact_id']==pin['report']['artifact_id']
    for sql,values in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(edit['mask']['artifact_id'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(sql,values);assert ready() is False

    from uuid import UUID
    protection_task,protection_root,protection_binary=uuid4(),UUID(scope['protection']['target_protection']['artifact_id']),uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'protection:0','mask.rasterize','1','cpu','[]',%s,%s,'{}',1,1)",(protection_task,tid,child,params,'a'*64))
    for aid,role,media,digest_value,size in ((protection_root,'mask','application/json','3'*64,100),(protection_binary,'mask_pixels','application/x-npy','2'*64,384)):
        if aid==protection_root:
            db.execute("UPDATE photo.artifacts SET output_role=%s,created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s",(role,protection_task,aid))
        else:
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'v1',clock_timestamp(),%s,%s,1,1)",(aid,tid,str(aid),digest_value,size,media,role,protection_task))
    protection_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,protection_task,Jsonb([dict(artifact_id=str(protection_root),sha256='3'*64),dict(artifact_id=str(protection_binary),sha256='2'*64)]),'3'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(protection_result,protection_task))
    db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,'[]','{}',%s,'cpu')",(tid,child,protection_task,scope['source'],'a'*64))
    producer=db.execute('SELECT created_by_task_id FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    rows=db.execute('SELECT * FROM photo.v27_exposure_adoption_protection_pin(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchall()
    assert rows==[(protection_root,'3'*64)]
    origin_args=(*args,protection_root,'3'*64,protection_binary,'2'*64)
    def origin():return db.execute('SELECT photo.v27_exposure_adoption_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',origin_args).fetchone()[0]
    assert origin() is True
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(protection_task,));assert origin() is False
    import pytest
    import psycopg
    with pytest.raises(psycopg.errors.CheckViolation):
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.artifacts SET media_type='application/octet-stream' WHERE id=%s",(protection_binary,))
    assert origin() is True

    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        inventory_rows=db.execute('SELECT * FROM photo.v27_exposure_adoption_staged_outputs(%s,%s,%s,%s)',(tid,task,1,1)).fetchall()
        assert len(inventory_rows)==4 and all(row[2]=='ready' for row in inventory_rows)
        for item in staged:
            evidence=db.execute('SELECT * FROM photo.v27_exposure_adoption_artifact(%s,%s,%s,%s,%s)',
                (tid,task,1,1,item['artifact_id'])).fetchone()
            assert evidence[3]==item['sha256'] and evidence[7]=='output'
        assert db.execute('SELECT * FROM photo.v27_exposure_adoption_artifact(%s,%s,%s,%s,%s)',
            (tid,task,1,2,staged[0]['artifact_id'])).fetchone() is None
        assert db.execute('SELECT * FROM photo.v27_exposure_adoption_artifact(%s,%s,%s,%s,%s)',
            (tid,task,1,1,uuid4())).fetchone() is None
        db.execute('RESET ROLE')

    signatures=(
        'photo.v27_exposure_adoption_context(uuid,uuid)',
        'photo.v27_pending_exposure_adoptions()',
        'photo.v27_exposure_adoption_artifact(uuid,uuid,integer,bigint,uuid)',
        'photo.v27_exposure_adoption_staged_outputs(uuid,uuid,integer,bigint)',
    )
    for signature in signatures:
        for role in ('photo_scheduler','photo_api','photo_worker','photo_gateway'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                (role,signature)).fetchone()[0] is (role=='photo_scheduler')
    def output_evidence(tenant=tid):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            row=db.execute('SELECT * FROM photo.v27_exposure_adoption_artifact(%s,%s,%s,%s,%s)',
                (tenant,task,1,1,staged[0]['artifact_id'])).fetchone()
            db.execute('RESET ROLE');return row
    assert output_evidence(uuid4()) is None
    for statement,values in (
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(staged[0]['artifact_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(staged[0]['artifact_id'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(statement,values)
            assert output_evidence() is None
    assert output_evidence() is not None

    inventory_sha=sha256(canonical_bytes(staged)).hexdigest()
    commit_args=(tid,task,1,1,result,Jsonb(staged),inventory_sha,params,digest,raw,protection_root,'3'*64,protection_binary,'2'*64)
    def commit():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.adopt_semantic_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',commit_args).fetchone()[0]
            db.execute('RESET ROLE');return value
    assert commit() is True
    assert commit() is False
    assert (tid,child,task) not in db.execute('SELECT * FROM photo.v27_pending_exposure_adoptions()').fetchall()
    assert db.execute("SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s AND artifact_id=ANY(%s)",(tid,[row['artifact_id'] for row in staged])).fetchone()[0]==4
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tid,child)).fetchone()[0]==1
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert commit() is False
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) SELECT tenant_id,id,attempt,fencing_token,gen_random_uuid(),clock_timestamp(),clock_timestamp(),'cpu',execution_profile FROM photo.tasks WHERE tenant_id=%s AND id=ANY(%s)",(tid,[task,protection_task]))
    return dict(tenant=tid,task=task,result=result,scope=scope,pin=pin,commit_args=commit_args)


def test_commit_scope_blocks_concurrent_revocation(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as seed:
        data=qualified_adopted_fixture(seed)
    with psycopg.connect(**database) as verifier,psycopg.connect(**database) as competing:
        assert verifier.execute('SELECT photo.adopt_semantic_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',data['commit_args']).fetchone()[0] is False
        for statement,values in (
            ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(data['scope']['asset'],)),
            ('DELETE FROM photo.snapshot_items WHERE snapshot_id=%s',(data['scope']['snapshot'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(data['scope']['source'],)),
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(data['pin']['policy_id'],)),
        ):
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with competing.transaction():
                    competing.execute("SET LOCAL lock_timeout='100ms'")
                    competing.execute(statement,values)
        verifier.commit()
        with competing.transaction():
            competing.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(data['pin']['policy_id'],))
        assert verifier.execute('SELECT photo.adopt_semantic_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',data['commit_args']).fetchone()[0] is False


def test_positive_qualified_canonical_result_admission_and_report(db):
    qualified_adopted_fixture(db)
