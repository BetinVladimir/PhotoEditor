"""Real SQL metadata fixtures; no qualification or object-store proof implied."""
from uuid import uuid4,uuid5
from psycopg.types.json import Jsonb
from test_database import tenant


def fixture(db, snapshot_authority=False, qualification=None, qualification_objects=None, calibration_input="pinned", protection_pins=False, transfer_params_builder=None, calibration_body=None, source_builder=None, original_body=None, protection_builder=None, transfer_resource_estimate=None, physical_closure=False,snapshot_builder=None,physical_assembly=False,defer_assembly=False):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Semantic final gate') RETURNING id",(tid,)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,CASE WHEN %s THEN clock_timestamp()+interval '1 hour' ELSE NULL END) RETURNING id",(tid,actor,'a'*64,snapshot_authority)).fetchone()[0]
    def artifact(digest,task=None,media='application/json',role=None,body=None):
        value=uuid4()
        bucket,version,size='fixture','v1',2
        if body is not None:
            from io import BytesIO
            assert qualification_objects is not None and isinstance(body,bytes)
            client,bucket=qualification_objects
            version=client.put_object(bucket,str(value),BytesIO(body),len(body),content_type=media,metadata={'sha256':digest}).version_id
            size=len(body)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,%s,%s)",(value,tid,bucket,str(value),digest,size,media,version,'document' if task else role,task,1 if task else None,1 if task else None))
        return value
    authority=None
    config={'project_id':str(project)}
    if snapshot_authority:
        session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
        from hashlib import sha256
        original_sha=sha256(original_body).hexdigest() if original_body is not None else '6'*64
        original=artifact(original_sha,media='image/jpeg',body=original_body)
        source_sha='5'*64;source_body=None;source_inputs=[]
        if source_builder is not None:
            from hashlib import sha256
            from photo_worker.contracts.images import WorkingImage
            source_body=source_builder(artifact,original_sha)
            image=WorkingImage.model_validate_json(source_body)
            source_sha=sha256(source_body).hexdigest()
            source_inputs=[(image.pixels.artifact_id,image.pixels.sha256,'NpyArray')]
            if image.alpha is not None:source_inputs.append((image.alpha.artifact_id,image.alpha.sha256,'NpyArray'))
            for step in image.transform_chain.steps:
                if step.kind=='dense_warp':source_inputs.append((step.inverse_field.artifact_id,step.inverse_field.sha256,'NpyArray'))
        source=artifact(source_sha,role='working_image',body=source_body)
        snapshot_root=artifact('8'*64)
        asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original)).fetchone()[0]
        version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,source_sha)).fetchone()[0]
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tid,snapshot,version,source,source_sha))
        db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
        config.update(input_snapshot_id=str(snapshot),v27_semantic=dict(auth_session_id=str(session),snapshot=dict(artifact_id=str(snapshot_root),sha256='8'*64),snapshot_digest='7'*64))
        authority=dict(session=session,actor=actor,project=project,snapshot=snapshot,root=snapshot_root,source=source,original=original,original_sha=original_sha,asset=asset,version=version,source_sha=source_sha,source_body=source_body,source_inputs=source_inputs)
    if qualification is None:qualification=snapshot_authority
    if qualification:
        assert authority is not None
        from semantic_qualification_fixture import frozen_qualification
        calibration_ref=None
        if calibration_body is not None:
            from hashlib import sha256
            from photo_worker.operations.session.contracts import Calibration
            calibration=Calibration.model_validate_json(calibration_body)
            assert calibration.model_lock_sha=='a'*64 and calibration.dataset_sha=='d'*64
            cal_sha=sha256(calibration_body).hexdigest()
            cal_id=artifact(cal_sha,body=calibration_body)
            calibration_ref=dict(artifact_id=str(cal_id),sha256=cal_sha)
            authority['calibration_body']=calibration_body
        authority['qualification']=frozen_qualification(db,tid,project,actor,objects=qualification_objects,calibration_ref=calibration_ref)
        config['v27_semantic']['qualification']=authority['qualification']
    protection_inputs=[]
    if protection_pins or protection_builder is not None:
        assert authority is not None
        authority['protection']=protection_builder(authority,artifact,tid) if protection_builder is not None else {key:dict(artifact_id=str(artifact(sha)),sha256=sha) for key,sha in [('source_protection','2'*64),('target_protection','3'*64)]}
        from uuid import UUID
        protection_inputs=[(UUID(ref['artifact_id']),ref['sha256'],'MaskRef') for ref in authority['protection'].values()]
    transfer_sha='f'*64
    body=None
    if transfer_params_builder is not None:
        from hashlib import sha256
        assert authority is not None
        body=transfer_params_builder(authority,artifact)
        assert isinstance(body,bytes)
        transfer_sha=sha256(body).hexdigest()
        authority['transfer_params_body']=body
        authority['transfer_params_sha']=transfer_sha
    transfer_params=artifact(transfer_sha,body=body)
    if snapshot_builder is not None:
        from hashlib import sha256
        from photo_worker.operations.session.contracts import InputSnapshot
        from photo_worker.contracts.base import canonical_hash
        snapshot_body=snapshot_builder(authority,artifact)
        typed_snapshot=InputSnapshot.model_validate_json(snapshot_body)
        assert typed_snapshot.snapshot_id==snapshot
        assert typed_snapshot.snapshot_digest==canonical_hash(typed_snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
        root_sha=sha256(snapshot_body).hexdigest()
        root=artifact(root_sha,body=snapshot_body)
        authority.update(root=root,snapshot_sha=root_sha,snapshot_body=snapshot_body)
        config['v27_semantic'].update(snapshot=dict(artifact_id=str(root),sha256=root_sha),snapshot_digest=typed_snapshot.snapshot_digest)
    run=uuid4();workflow=uuid5(run,'workflow')
    if authority:
        config['v27_semantic']['nodes']=[dict(task_id=str(uuid5(workflow,'semantic.transfer:0')),params_artifact_id=str(transfer_params),params_sha=transfer_sha)]
    if protection_pins or protection_builder is not None:config['v27_semantic']['nodes'][0].update(authority['protection'])
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,'fail_fast',false)",(run,tid,snapshot,Jsonb(config),'b'*64,actor))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,tid,project,run,snapshot,'c'*64))
    def task(operation,params,children):
        value=uuid5(workflow,operation+':0')
        params_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(params,)).fetchone()[0]
        entries=[(params,params_sha,'Params'),*children]
        registered=[]
        if physical_closure:
            from hashlib import sha256
            from photo_worker.contracts.base import ArtifactRef
            from photo_worker.v2.input_closure import InputClosureEntry,InputClosureManifest
            from photo_worker.v2.input_closure_builder import build_input_closure_from_sorted
            uploaded={}
            def upload(raw,contract):
                digest=sha256(raw).hexdigest();aid=artifact(digest,body=raw)
                ref=ArtifactRef(artifact_id=aid,sha256=digest);uploaded[aid]=raw
                return ref
            def rows(first,values):
                registered.extend((first+index,entry.ref.artifact_id,entry.ref.sha256,entry.contract) for index,entry in enumerate(values))
            def transport(ref,contract,ordinal):registered.append((ordinal,ref.artifact_id,ref.sha256,contract))
            ordered=sorted(entries,key=lambda value:(value[0].bytes,value[1],value[2]))
            closure=build_input_closure_from_sorted(value,[InputClosureEntry(ref=ArtifactRef(artifact_id=ref,sha256=digest),contract=contract) for ref,digest,contract in ordered],upload,rows,transport)
            manifest_ref=closure.manifest.model_dump(mode='json')
            entries_sha=InputClosureManifest.model_validate_json(uploaded[closure.manifest.artifact_id]).entries_sha
        else:
            manifest=artifact('d'*64);chunk=artifact('e'*64)
            manifest_ref=dict(artifact_id=str(manifest),sha256='d'*64);entries_sha='a'*64
            registered=[(ordinal,*entry) for ordinal,entry in enumerate([*entries,(chunk,'e'*64,'InputClosureChunk'),(manifest,'d'*64,'InputClosureManifest')])]
        profile=dict(renderer_version='v2.2',backend='cpu' if operation=='v2.semantic.assemble' else 'cuda',
            contracts_sha='4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',
            v2_input_manifest=manifest_ref,v2_entry_count=len(entries),v2_entries_sha=entries_sha)
        if operation=='semantic.transfer' and transfer_resource_estimate is not None:profile['resource_estimate']=transfer_resource_estimate
        if physical_assembly and operation=='v2.semantic.assemble':profile['resource_estimate']=dict(ram_bytes=256*1024**2,scratch_bytes=32*1024**2,vram_bytes=0,output_bytes=8*1024**2)
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1',%s,%s,%s,%s,%s,1,1)",(value,tid,run,operation+':0',operation,'cpu' if operation=='v2.semantic.assemble' else 'gpu',Jsonb([manifest_ref]),params,'b'*64,Jsonb(profile)))
        for ordinal,ref,digest,contract in registered:
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,project,value,ref,digest,contract,ordinal))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),%s,%s)",(tid,value,uuid4(),profile['backend'],Jsonb(profile)))
        output_sha='9'*64;output_body=None
        if physical_assembly and operation=='semantic.transfer':
            from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate
            from photo_worker.contracts.base import canonical_bytes
            from hashlib import sha256
            transfer=SemanticTransferParams.model_validate_json(authority['transfer_params_body'])
            output_body=canonical_bytes(TransferCandidate(schema_version=1,target=transfer.target,mask=None,transform=None,confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE'))
            output_sha=sha256(output_body).hexdigest()
        output=artifact(output_sha,value,body=output_body)
        location=db.execute('SELECT bucket,object_key,object_version,bytes FROM photo.artifacts WHERE id=%s',(output,)).fetchone()
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document',%s,%s,%s,%s,'application/json','ready',%s)",(tid,value,uuid4(),'a'*64,uuid4(),output,location[0],location[1],output_sha,location[3],location[2]))
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,value,Jsonb([dict(artifact_id=str(output),sha256=output_sha)]),'c'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,value))
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(value,)).fetchone()[0]
        return value,result,output
    calibration_inputs=[]
    if authority and authority.get('qualification'):
        cal=authority['qualification']['calibration']
        from uuid import UUID
        assert calibration_input in ('pinned','alternate','absent')
        if calibration_input!='absent':
            identifier=artifact(cal['sha256']) if calibration_input=='alternate' else UUID(cal['artifact_id'])
            calibration_inputs=[(identifier,cal['sha256'],'Calibration')]
    producer,producer_result,candidate=task('semantic.transfer',transfer_params,[*calibration_inputs,*protection_inputs,*(authority.get('protection_pixel_inputs',[]) if authority else []),*(authority.get('transfer_inputs',[]) if authority else []),*(authority.get('source_inputs',[]) if authority else []),*([(authority['source'],authority['source_sha'],'WorkingImage')] if authority else [])])
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,producer,producer_result))
    candidate_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    children=[(candidate,candidate_sha,'TransferCandidate'),*calibration_inputs]
    if authority:children += [(authority['root'],authority.get('snapshot_sha','8'*64),'InputSnapshot'),(authority['source'],authority['source_sha'],'WorkingImage')]
    assembly_params=artifact('f'*64)
    if physical_assembly:
        from photo_worker.contracts.semantic import V2SemanticAssembleParams,SemanticTransferParams
        from photo_worker.contracts.base import ArtifactRef,canonical_bytes
        from hashlib import sha256
        transfer=SemanticTransferParams.model_validate_json(authority['transfer_params_body'])
        assembly_body=canonical_bytes(V2SemanticAssembleParams(schema_version=1,intent=transfer.intent,candidates=[ArtifactRef(artifact_id=candidate,sha256=candidate_sha)],model_lock_sha=transfer.model_lock_sha,calibration=transfer.calibration))
        assembly_params=artifact(sha256(assembly_body).hexdigest(),body=assembly_body)
        children.extend(authority['transfer_inputs'])
        authority.update(assembly_params_body=assembly_body,candidate_sha=candidate_sha)
    assembly,result=None,None
    if not defer_assembly:assembly,result,_=task('v2.semantic.assemble',assembly_params,children)
    if authority:authority['assembly']=assembly
    db.execute('UPDATE photo.runs SET graph_sealed=true WHERE id=%s',(run,))
    result_tuple=(tid,assembly,result,producer,producer_result,candidate)
    return (*result_tuple,authority) if snapshot_authority else result_tuple


def test_semantic_final_gate_positive_metadata_and_stale_producer(db):
    tid,assembly,result,producer,producer_result,candidate=fixture(db)
    def ready():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:
                return db.execute('SELECT photo.v27_semantic_assembly_inputs_ready(%s,%s,1,1,%s)',(tid,assembly,result)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert ready()
    for query,args in [
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(producer,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(candidate,)),
        ("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tid,producer,producer_result)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert not ready()
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert not db.execute("SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,'[]',%s)",(tid,assembly,result,'a'*64)).fetchone()[0]
    assert ready()


def test_semantic_final_gate_private_predecessor_and_scheduler_boundary(db):
    signature='photo.v27_semantic_assembly_inputs_ready(uuid,uuid,integer,bigint,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
    predecessor='photo.adopt_v2_plan_pre_semantic_producers(uuid,uuid,integer,bigint,uuid,jsonb,text)'
    assert not db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(predecessor,)).fetchone()[0]


def test_semantic_final_gate_holds_producer_generation_lock(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as setup:
        tid,assembly,result,producer,_,candidate,scope=fixture(setup,snapshot_authority=True)
        setup.commit()
    with psycopg.connect(**database) as adoption, psycopg.connect(**database) as competing:
        root=adoption.execute('SELECT outputs->0 FROM photo.task_results WHERE id=%s',(result,)).fetchone()[0]
        producer_params=adoption.execute('SELECT params_artifact_id FROM photo.tasks WHERE id=%s',(producer,)).fetchone()[0]
        adoption.execute('SET LOCAL ROLE photo_scheduler')
        inventory=[dict(root,purpose='a'*64,ordinal=0),dict(artifact_id=str(candidate),sha256='9'*64,purpose='b'*64,ordinal=1)]
        # Metadata-only SQL acceptance, not a qualified SemanticPlan document.
        assert adoption.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
            (tid,assembly,result,Jsonb(inventory),'a'*64)).fetchone()[0]
        for query,args in [
            ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(producer_params,)),
            ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
            ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
            ("UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s AND user_id=%s",(tid,scope['actor'])),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['source'],)),
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(scope['qualification']['policy_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['qualification']['report']['artifact_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['qualification']['calibration']['artifact_id'],)),
        ]:
            competing.execute("SET LOCAL lock_timeout='150ms'")
            with pytest.raises(psycopg.errors.LockNotAvailable):
                competing.execute(query,args)
            competing.rollback()
        adoption.rollback()
        competing.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,))
        competing.commit()
        adoption.execute('SET LOCAL ROLE photo_scheduler')
        assert not adoption.execute('SELECT photo.v27_semantic_assembly_inputs_ready(%s,%s,1,1,%s)',
            (tid,assembly,result)).fetchone()[0]
