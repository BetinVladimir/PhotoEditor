"""Synthetic SQL metadata boundary; no model execution or object-byte evidence."""
from uuid import UUID,uuid4,uuid5
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_v27_native_anchor import seed_native_anchor

@pytest.mark.parametrize('operation,roles',[
 ('face.landmarks',[('face_landmarks','application/json')]),
 ('face.parse',[('face_labels','application/octet-stream'),('face_labels_ref','application/json')]),
 ('mask.person',[('person_mask','application/octet-stream'),('person_mask_png','image/png'),('person_mask_ref','application/json'),('person_report','application/json')]),
])
def test_native_face_verified_outputs(db,operation,roles):
    packet,case,_=seed_native_anchor(db,'face.detect','detector')
    tenant,actor,project=case['Tenant'],case['Actor'],case['Project']
    run=uuid5(UUID(packet['parent_run_id']),'semantic.'+operation+':reference')
    workflow=uuid5(run,'workflow');task=uuid5(workflow,operation+':0');worker=uuid4()
    params=packet['registration']['params']['artifact_id']
    marker = 'v27_person_prepare' if operation == 'mask.person' else 'v27_face_prepare'
    ready_function = 'semantic_native_preparation_result_ready' if operation == 'mask.person' else 'semantic_native_face_result_ready'
    guard_error = 'V27_NATIVE_PREPARATION_OUTPUTS_UNVERIFIED' if operation == 'mask.person' else 'V27_NATIVE_FACE_OUTPUTS_UNVERIFIED'
    config=dict(project_id=str(project),**{marker:dict(schema_version=1,operation=operation,task_id=str(task),input_digest='d'*64,parent_run_id=packet['parent_run_id'],request_sha=packet['request_sha'],auth_session_id=str(case['Session']),source=packet['binding'],params=packet['registration']['params'],detector_task_id=str(uuid4()))})
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'running',true,'fail_fast')",(run,tenant,case['Snapshot'],Jsonb(config),'c'*64,actor))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,tenant,project,run,case['Snapshot'],'b'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1','gpu','[]',%s,%s,%s,1,1)",(task,tenant,run,operation+':0',operation,params,'d'*64,Jsonb(dict(backend='cuda',renderer_version='v2.2'))))
    if operation == 'mask.person':
        expected = dict(packet['expected'], Operation='mask.person')
        profile = dict(backend='cuda', renderer_version='v2.2', model_lock_sha=expected['ModelLockSHA'], runtime_lock_sha=expected['RuntimeLockSHA'], device_profile_sha=expected['DeviceProfileSHA'])
        db.execute('UPDATE photo.tasks SET execution_profile=%s WHERE tenant_id=%s AND id=%s',(Jsonb(profile),tenant,task))
        source = packet['binding']['source']
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,'WorkingImage',0)",(tenant,project,task,source['artifact_id'],source['sha256']))
    outputs=[]
    for role,media in roles:
        aid,key=uuid4(),str(uuid4());ref=dict(artifact_id=str(aid),sha256='7'*64);outputs.append(ref)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'test',%s,%s,128,%s,'v1',clock_timestamp(),%s,%s,1,1)",(aid,tenant,key,ref['sha256'],media,role,task))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'test',%s,%s,128,%s,'ready','v1',clock_timestamp())",(tenant,task,worker,'a'*64,uuid4(),aid,role,key,ref['sha256'],media))
    ready=lambda refs: db.execute(f'SELECT photo.{ready_function}(%s,%s,1,1,%s)',(tenant,task,Jsonb(refs))).fetchone()[0]
    assert ready(outputs)
    bad_outputs=[[],outputs+[outputs[0]], [dict(outputs[0],extra=True)],
                 [dict(outputs[0],sha256='8'*64)], [dict(outputs[0],artifact_id=str(uuid4()))]]
    if len(outputs)>1:
        bad_outputs.extend([list(reversed(outputs)),[outputs[0]]*len(outputs),outputs[:1]])
    if operation == 'mask.person':
        bad_outputs.extend([outputs[:i] + outputs[i+1:] for i in range(4)])
        bad_outputs.extend([outputs[:i] + [dict(outputs[i],sha256='8'*64)] + outputs[i+1:] for i in range(4)])
    for bad in bad_outputs:
        assert not ready(bad)
        with pytest.raises((errors.CheckViolation,errors.ForeignKeyViolation)) as rejected:
            with db.transaction():
                db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",(tenant,task,Jsonb(bad),'6'*64))
        assert (guard_error if isinstance(rejected.value,errors.CheckViolation) else 'ARTIFACT_REFERENCE_MISSING') in str(rejected.value)
    with db.transaction(force_rollback=operation == 'mask.person'):
        db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",(tenant,task,Jsonb(outputs),'6'*64))
    for output in outputs:
        with db.transaction(force_rollback=True):
            db.execute('UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=%s',(output['artifact_id'],))
            assert not ready(outputs)

    for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
        assert not db.execute(f"SELECT has_function_privilege(%s,'photo.{ready_function}(uuid,uuid,integer,bigint,jsonb)','EXECUTE')",(role,)).fetchone()[0]
    for attempt,fence in ((0,1),(1,0),(2,1),(1,2)):
        assert not db.execute(f'SELECT photo.{ready_function}(%s,%s,%s,%s,%s)',(tenant,task,attempt,fence,Jsonb(outputs))).fetchone()[0]
        with pytest.raises(errors.CheckViolation,match=guard_error):
            with db.transaction():
                db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,%s,%s,'{}',%s)",(tenant,task,attempt,fence,Jsonb(outputs),'6'*64))
    mutations=[
        ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",(task,)),
        ("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",(run,)),
        ("UPDATE photo.runs SET status='queued' WHERE id=%s",(run,)),
        ("UPDATE photo.runs SET graph_sealed=false WHERE id=%s",(run,)),
    ]
    for output in outputs:
        aid=output['artifact_id']
        mutations.extend([
            ("UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s",(aid,)),
            ("UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s",(aid,)),
            ("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(aid,)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(aid,)),
            ("UPDATE photo.output_uploads SET state='pending' WHERE artifact_id=%s",(aid,)),
            ("UPDATE photo.output_uploads SET object_version='changed' WHERE artifact_id=%s",(aid,)),
            ("UPDATE photo.output_uploads SET bytes=129 WHERE artifact_id=%s",(aid,)),
            ("UPDATE photo.output_uploads SET media_type=CASE WHEN media_type='image/png' THEN 'application/json' ELSE 'image/png' END WHERE artifact_id=%s",(aid,)),
        ])
    for output in outputs:
        with pytest.raises(errors.CheckViolation,match='immutable field artifacts.object_version'),db.transaction():
            db.execute("UPDATE photo.artifacts SET object_version='null' WHERE id=%s",(output['artifact_id'],))
    for query,args in mutations:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert not ready(outputs),query
            expected_error = 'ARTIFACT_REFERENCE_NOT_READY' if "expires_at=" in query or "object_version='null'" in query else guard_error
            with pytest.raises(errors.CheckViolation,match=expected_error):
                with db.transaction():
                    db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",(tenant,task,Jsonb(outputs),'6'*64))
        assert ready(outputs)
    if operation == 'mask.person':
        # Ready uploads alone must not enter the API candidate inventory.
        db.execute('SET LOCAL ROLE photo_api')
        try:
            binding = packet['binding']; source = binding['source']
            assert not db.execute('SELECT * FROM photo.semantic_proposal_person_candidates(%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,project,actor,case['Session'],binding['asset_version_id'],source['artifact_id'],source['sha256'],Jsonb(expected))).fetchall()
        finally:
            db.execute('RESET ROLE')
        from native_person_commit_probe import verify_person_worker_commit
        verify_person_worker_commit(db,tenant,run,task,worker,outputs,guard_error)
        from native_person_catalog_probe import verify_person_canonical_catalog
        verify_person_canonical_catalog(db,case,packet,task,outputs,expected)
    else:
        result=db.execute('SELECT id FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    # Metadata alone cannot delegate a native completed result to a caller:
    # this synthetic fixture has no original parent-owned completed detector.
    signature='photo.semantic_native_face_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid)'
    for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] == (role=='photo_api')
    db.execute('SET LOCAL ROLE photo_api')
    assert not db.execute('SELECT * FROM photo.semantic_native_face_result_catalog(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,actor,case['Session'],packet['parent_run_id'],packet['request_sha'],task)).fetchall()
    db.execute('RESET ROLE')
