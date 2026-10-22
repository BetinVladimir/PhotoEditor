"""Own exposure shown-copy completion metadata; no physical execution proof."""
import hashlib
import json
from uuid import uuid4
from psycopg.types.json import Jsonb


def committed_copy_fixture(db):
    from exposure_preview_receipt_fixture import receipt_fixture
    from exposure_apply_documents_fixture import publish_exposure_decision_bundles, approved_params_fixture, admission_task_fixture
    data = receipt_fixture(db)
    bundle = dict(data=data, args=data[0], run=data[1], scope=data[2], target=data[0][4])
    request, artifact, body = publish_exposure_decision_bundles(db, [bundle])
    approved = approved_params_fixture(db, published=(data, request, artifact, body))
    values, task = admission_task_fixture(db, approved=approved)
    args = data[0]
    child = values[4]
    key = str(uuid4())
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) "
               "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
               (args[0], args[2], key, hashlib.sha256(request.encode()).hexdigest()))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        response = db.execute('SELECT photo.commit_semantic_shown_apply_review(' + ','.join(['%s']*12) + ')',
            (*args[:4], key, int(json.loads(args[5])['workflow_revision']), request, artifact, body,
             Jsonb(data[2]['review_documents']), child, Jsonb([task]))).fetchone()[0]
        db.execute('RESET ROLE')
    return values, task, response


def dispatched_copy_fixture(db, refusal=None, *, include_documents=False):
    """Run actual scheduler reservation before exercising actual worker claim."""
    from uuid import uuid4
    values, task, _ = committed_copy_fixture(db)
    tenant = values[0][0][0]
    task_id = task['node']['id']
    run, params, sha, fence, profile, inputs = db.execute(
        'SELECT t.run_id,t.params_artifact_id,a.sha256,t.fencing_token,t.execution_profile,t.inputs '
        'FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id '
        'WHERE t.id=%s', (task_id,)).fetchone()
    worker = uuid4()
    node = 'exposure-completion-' + str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',
        (node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) "
        "VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"semantic.adopt_shown_exposure\":[\"cpu\"]}','[\"v2.2\"]')",
        (tenant,worker,profile.get('model_lock_id','a'*64),node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
    estimate = profile['resource_estimate']
    event, correlation = uuid4(), uuid4()
    expected_refusals = dict(stale_capacity='waiting_resource',insufficient_ram='waiting_resource',
        unqualified_worker='waiting_worker',unavailable_input='input_unavailable')
    if refusal=='stale_capacity':
        db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(node,))
    elif refusal=='insufficient_ram':
        db.execute('UPDATE photo.resource_nodes SET ram_available_bytes=1 WHERE node_id=%s',(node,))
    elif refusal=='unqualified_worker':
        db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    elif refusal=='unavailable_input':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(inputs[0]['artifact_id'],))
    elif refusal is not None:
        raise AssertionError('Unknown dispatch refusal')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        dispatched = db.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,%s,0,%s,%s,%s,%s)',
            (task_id,node,estimate['ram_bytes'],estimate['scratch_bytes'],event,correlation,'')).fetchone()
        assert dispatched[0]==expected_refusals.get(refusal,'queued')
        db.execute('RESET ROLE')
    if refusal is not None:
        assert dispatched[1] is None
        assert db.execute('SELECT state,attempt,fencing_token,dispatch_command,dispatch_worker_id FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==('ready',0,fence,None,None)
        assert db.execute('SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',(tenant,task_id)).fetchone()==(0,)
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task_id,)).fetchone()==(0,)
        assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()==(0,)
        return
    command = dispatched[1]
    assert command['inputs']==inputs
    assert command['params']==dict(artifact_id=str(params),sha256=sha)
    assert command['operation']=='semantic.adopt_shown_exposure'
    assert command['model'] is None and command['attempt']==1
    assert db.execute('SELECT dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==(worker,command)
    route,payload = db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.cpu.semantic_adopt_shown_exposure'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task_id)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    result=(tenant,task_id,run,worker,command,fence)
    return (*result,values,task) if include_documents else result



def successful_copy_metadata_fixture(db):
    """Real dispatch/claim; synthetic ready output metadata, no object-store proof."""
    from photo_worker.contracts.base import canonical_bytes
    tenant,task_id,child,worker,command,_,values,task=dispatched_copy_fixture(db,include_documents=True)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        claim=db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task_id,worker,Jsonb(command))).fetchone()
        assert claim[0]=='claimed'
        db.execute('RESET ROLE')
    fence=claim[1]
    shown=task['documents']['image']
    output=json.loads(shown)
    pixel=uuid4();root=uuid4()
    output['pixels']['artifact_id']=str(pixel)
    raw=canonical_bytes(output).decode()
    sha=hashlib.sha256(raw.encode()).hexdigest()
    pixel_sha=output['pixels']['sha256']
    pixel_size=db.execute('SELECT bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s',
        (tenant,json.loads(shown)['pixels']['artifact_id'])).fetchone()[0]
    for aid,digest,size,media,role in ((pixel,pixel_sha,pixel_size,'application/x-npy','pixels'),
            (root,sha,len(raw.encode()),'application/json','working_image')):
        key='metadata-only/'+str(aid)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'metadata-only-v1',clock_timestamp(),%s,%s,1,%s)",
            (aid,tenant,key,digest,size,media,role,task_id,fence))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,%s,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,%s,'ready','metadata-only-v1',clock_timestamp())",
            (tenant,task_id,fence,worker,'a'*64,uuid4(),aid,role,key,digest,size,media))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,%s,%s,'{}',%s) RETURNING id",
        (tenant,task_id,fence,Jsonb([dict(artifact_id=str(root),sha256=sha)]),sha)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE tenant_id=%s AND id=%s",(result,tenant,task_id))
    inventory=[dict(artifact_id=str(aid),sha256=digest,purpose=hashlib.sha256(('fixture/'+str(aid)).encode()).hexdigest(),ordinal=i)
        for i,(aid,digest) in enumerate(((root,sha),(pixel,pixel_sha)))]
    digest=hashlib.sha256(canonical_bytes(inventory)).hexdigest()
    commit=(tenant,task_id,1,fence,result,Jsonb(inventory),digest,task['params_artifact_id'],task['params_sha'],task['params_body'],root,sha,raw,shown)
    return values,task,commit


def adopt(db, commit):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        accepted = db.execute('SELECT photo.adopt_shown_exposure(' + ','.join(['%s']*14) + ')', commit).fetchone()[0]
        db.execute('RESET ROLE')
        return accepted
