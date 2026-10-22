"""Positive real initializer prerequisite for full cleanup Apply; not commit proof."""
import json

from psycopg.types.json import Jsonb
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from background_cleanup_apply_documents_fixture import prepared_review_fixture


def test_background_cleanup_full_review_documents_reinitialize_same_intent(database):
    args,parent,scope,target = prepared_review_fixture(database)
    params = json.loads(args[5])
    before = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(target,)).fetchone()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        for _ in range(2):
            assert database.execute('SELECT photo.initialize_semantic_review(%s,%s,%s,%s,%s,%s)',
                (args[0],params['workflow_id'],args[2],args[3],int(params['workflow_revision']),Jsonb(scope['review_documents']))).fetchone()[0] == args[1]
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(target,)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.semantic_targets WHERE tenant_id=%s AND intent_id=%s',args[:2]).fetchone()[0] == 1


def test_background_cleanup_full_review_documents_survive_preview_activation_and_receipt(database):
    from test_v27_background_cleanup_preview_receipt import receipt_fixture,seal,lock_selection
    data = receipt_fixture(database,review_documents=True)
    args,preview,scope = data[:3]
    receipt = seal(database,data)
    assert lock_selection(database,data,receipt) == [(preview,receipt)]
    params = json.loads(args[5])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        initialized = database.execute('SELECT photo.initialize_semantic_review(%s,%s,%s,%s,%s,%s)',
            (args[0],params['workflow_id'],args[2],args[3],int(params['workflow_revision']),Jsonb(scope['review_documents']))).fetchone()[0]
        database.execute('RESET ROLE')
    assert initialized == args[1]
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(preview,)).fetchone()[0] == receipt
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == ('proposed',None,None)


def test_background_cleanup_published_decision_locks_exact_selection_without_child(database):
    from background_cleanup_apply_documents_fixture import published_decision_fixture
    data,request,artifact,body = published_decision_fixture(database)
    args,preview,scope = data[:3]
    params = json.loads(args[5])
    values = (*args[:4],int(params['workflow_revision']),request,artifact,body,Jsonb(scope['review_documents']))
    before = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()
    for _ in range(2):
        owner = database.execute('SELECT photo.lock_semantic_shown_background_cleanup_decision_batch('+','.join(['%s']*9)+')',values).fetchone()[0]
        assert owner is not None
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == before


def test_background_cleanup_published_decision_refuses_changed_bytes_and_revoked_session(database):
    import psycopg
    import pytest
    from background_cleanup_apply_documents_fixture import published_decision_fixture
    data,request,artifact,body = published_decision_fixture(database)
    args,preview,scope = data[:3]
    params = json.loads(args[5])
    values = (*args[:4],int(params['workflow_revision']),request,artifact,body,Jsonb(scope['review_documents']))
    before = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()
    with pytest.raises(psycopg.errors.CheckViolation):
        with database.transaction(force_rollback=True):
            changed = list(values); changed[7] += ' '
            database.execute('SELECT photo.lock_semantic_shown_background_cleanup_decision_batch('+','.join(['%s']*9)+')',changed)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with database.transaction(force_rollback=True):
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
            database.execute('SELECT photo.lock_semantic_shown_background_cleanup_decision_batch('+','.join(['%s']*9)+')',values)
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == before


def test_background_cleanup_approved_params_publication_retry_preserves_target(database):
    from background_cleanup_apply_documents_fixture import approved_params_fixture
    data,request,decision,decision_body,child,owner,aid,raw,sha,base = approved_params_fixture(database)
    args = data[0]
    before = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()
    def reserve():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            value = database.execute('SELECT * FROM photo.reserve_semantic_approved_params_document('+','.join(['%s']*15)+')',
                (*base,owner,aid,sha,len(raw.encode()),'fixture')).fetchone()
            database.execute('RESET ROLE')
            return value
    def complete(token,version='approved-fixture-v1'):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            value = database.execute('SELECT photo.complete_semantic_approved_params_document('+','.join(['%s']*17)+')',
                (*base,owner,aid,token,sha,len(raw.encode()),'fixture',version)).fetchone()[0]
            database.execute('RESET ROLE')
            return value
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    first = reserve()
    assert first[0]=='created' and first[1]==aid and first[3] is False
    retry = reserve()
    assert retry[0]=='resumed' and retry[2]!=first[2]
    assert complete(first[2]) is False
    assert complete(retry[2]) is True
    assert complete(retry[2]) is True
    assert complete(retry[2],'changed-version') is False
    assert reserve()[0]=='replayed'
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()==(usage[0]+len(raw.encode()),usage[1])
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()==before
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(child,)).fetchone()[0]==0
    assert database.execute('SELECT sha256,bytes,output_role,object_version FROM photo.artifacts WHERE id=%s',(aid,)).fetchone()==(sha,len(raw.encode()),'task_params','approved-fixture-v1')


def test_background_cleanup_copy_params_publication_bound_to_shown_receipt(database):
    from background_cleanup_apply_documents_fixture import shown_copy_params_fixture
    values,copy,task,aid,raw,sha,base = shown_copy_params_fixture(database)
    data,_,_,_,child = values[:5]
    args = data[0]
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        reserved = database.execute('SELECT * FROM photo.reserve_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*18)+')',
            (*base,'task_params',task,aid,sha,len(raw.encode()),'fixture')).fetchone()
        assert reserved[0]=='created' and reserved[1]==aid and reserved[3] is False
        assert database.execute('SELECT photo.complete_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*20)+')',
            (*base,'task_params',task,aid,reserved[2],sha,len(raw.encode()),'fixture','copy-params-fixture-v1')).fetchone()[0] is True
        replay = database.execute('SELECT * FROM photo.reserve_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*18)+')',
            (*base,'task_params',task,aid,sha,len(raw.encode()),'fixture')).fetchone()
        assert replay[0]=='replayed' and replay[1]==aid and replay[3] is True
        database.execute('RESET ROLE')
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()==(usage[0]+len(raw.encode()),usage[1])
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()==('proposed',None,None)
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0


def test_background_cleanup_copy_params_rejects_changed_producer_before_publication(database):
    import hashlib
    import psycopg
    import pytest
    from background_cleanup_apply_documents_fixture import shown_copy_params_fixture
    values,copy,task,aid,raw,sha,base = shown_copy_params_fixture(database)
    args = values[0][0]
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    for field in ('preview_attempt','preview_fence','runtime_lock_sha'):
        changed = dict(copy)
        changed[field] = copy[field]+1 if field!='runtime_lock_sha' else '0'*64
        changed_raw = json.dumps(changed,sort_keys=True,separators=(',', ':'))
        changed_sha = hashlib.sha256(changed_raw.encode()).hexdigest()
        changed_base = (*base[:9],changed_raw,changed_sha,base[11])
        with pytest.raises(psycopg.errors.InvalidParameterValue):
            with database.transaction(force_rollback=True):
                database.execute('SET LOCAL ROLE photo_api')
                database.execute('SELECT * FROM photo.reserve_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*18)+')',
                    (*changed_base,'task_params',task,aid,changed_sha,len(changed_raw.encode()),'fixture'))
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()==usage
    assert database.execute('SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s AND owner_id=%s',(args[0],task)).fetchone()[0]==0


def test_background_cleanup_copy_exact_inventory_and_published_transport(database):
    import copy as copying
    from background_cleanup_apply_documents_fixture import shown_copy_transport_fixture
    values,params,node,transport,inventory = shown_copy_transport_fixture(database)
    data = values[0]
    args,_,scope,presentation = data[:4]
    child = values[4]
    def transport_ready(t=transport,i=inventory):
        return database.execute('SELECT photo.semantic_shown_background_cleanup_transport_ready(%s,%s,%s,%s,%s)',
            (child,Jsonb(params),Jsonb(node),Jsonb(t),Jsonb(i))).fetchone()[0]
    assert transport_ready() is True
    document_args = [params,presentation,json.loads(args[5]),node,inventory,
        json.loads(scope['candidate_body']),json.loads(scope['intent_body']),json.loads(scope['source_body']),
        json.loads(scope['source_body']),json.loads(scope['candidate_mask_body']),
        json.loads(scope['protection_documents']['target_protection']['body'])]
    assert database.execute('SELECT photo.semantic_shown_background_cleanup_inventory_exact('+','.join(['%s']*11)+')',
        tuple(Jsonb(v) for v in document_args)).fetchone()[0] is True
    changed = copying.deepcopy(transport); changed['manifest']['sha256']='0'*64
    assert transport_ready(changed) is False
    changed = copying.deepcopy(transport); changed['direct_roots'].reverse()
    assert transport_ready(changed) is False
    changed = copying.deepcopy(inventory); changed['entries'].pop()
    assert transport_ready(i=changed) is False
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()==('proposed',None,None)
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0


def test_background_cleanup_copy_resource_profile_preserves_image_contract(database):
    import copy as copying
    from background_cleanup_apply_documents_fixture import shown_copy_transport_fixture
    values,params,node,transport,inventory = shown_copy_transport_fixture(database)
    image = json.loads(values[0][6])
    pixels = image['canvas']['width']*image['canvas']['height']
    estimate = dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*pixels+32768+16777216,
        output_bytes=12*pixels+16384+1048576,pixel_count=pixels)
    profile = dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate,dataset_sha='d'*64,qualification={},
        **{k:params[k] for k in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    # Shape/resource check only. Empty qualification must never be dispatched.
    def bound(p=profile):
        return database.execute('SELECT photo.semantic_shown_background_cleanup_profile_bound(%s,%s,%s)',
            (Jsonb(params),Jsonb(p),Jsonb(image))).fetchone()[0]
    assert bound() is True
    for field in ('ram_bytes','scratch_bytes','output_bytes'):
        changed = copying.deepcopy(profile); changed['resource_estimate'][field]-=1
        assert bound(changed) is False
    for field,value in (('vram_bytes',1),('pixel_count',pixels+1)):
        changed = copying.deepcopy(profile); changed['resource_estimate'][field]=value
        assert bound(changed) is False
    assert (image.keys()-{'pixels'})==(json.loads(values[0][2]['source_body']).keys()-{'pixels'})
    assert {k:v for k,v in image.items() if k!='pixels'}=={k:v for k,v in json.loads(values[0][2]['source_body']).items() if k!='pixels'}


def test_background_cleanup_full_document_preflight_with_own_cpu_policy(database):
    from background_cleanup_apply_documents_fixture import admission_task_fixture
    values,task = admission_task_fixture(database)
    data,_,_,_,child = values[:5]
    args,_,scope = data[:3]
    params = json.loads(args[5])
    arguments = (*args[:4],child,args[4],scope['parent_run'],int(params['workflow_revision']),int(params['target_revision']),
        task['params_body'],task['params_sha'],Jsonb(task['documents']),Jsonb(task['node']),Jsonb(task['transport']),Jsonb(task['inventory']))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_semantic_admission')
        assert database.execute('SELECT photo.semantic_shown_background_cleanup_admission_documents_ready('+','.join(['%s']*15)+')',arguments).fetchone()[0] is True
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()==('proposed',None,None)
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0


def test_background_cleanup_full_atomic_apply_queues_exact_copy_and_completes_receipt(database):
    import hashlib
    from uuid import uuid4
    from background_cleanup_apply_documents_fixture import admission_task_fixture
    values,task = admission_task_fixture(database)
    data,request,decision,decision_body,child = values[:5]
    args,preview,scope = data[:3]
    params = json.loads(args[5])
    key = str(uuid4())
    database.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (args[0],args[2],key,hashlib.sha256(request.encode()).hexdigest()))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_semantic_admission')
        response = database.execute('SELECT photo.commit_semantic_shown_background_cleanup_apply_review('+','.join(['%s']*12)+')',
            (*args[:4],key,int(params['workflow_revision']),request,decision,decision_body,Jsonb(scope['review_documents']),child,Jsonb([task]))).fetchone()[0]
        database.execute('RESET ROLE')
    assert response['child']==dict(run_id=str(child),state='queued')
    assert response['revision']==str(int(params['workflow_revision'])+1) and response['remaining']==0
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(child,)).fetchone()==('queued',True)
    assert database.execute('SELECT state,attempt,operation,params_artifact_id::text FROM photo.tasks WHERE run_id=%s',(child,)).fetchall()==[('ready',0,'semantic.adopt_shown_background_cleanup',task['params_artifact_id'])]
    target = database.execute('SELECT state,child_run_id,batch_id::text FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()
    assert target==('queued',child,response['batch_id'])
    expected_inputs = [(entry['ref']['artifact_id'],entry['ref']['sha256'],entry['contract'],ordinal)
        for ordinal,entry in enumerate(task['inventory']['entries'])]
    expected_inputs += [(task['transport']['chunks'][0]['artifact_id'],task['transport']['chunks'][0]['sha256'],'InputClosureChunk',1000000),
        (task['transport']['manifest']['artifact_id'],task['transport']['manifest']['sha256'],'InputClosureManifest',1000001)]
    assert database.execute('SELECT artifact_id::text,sha256,contract,ordinal FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s ORDER BY ordinal',
        (args[0],task['node']['id'])).fetchall()==expected_inputs
    stored_inputs,stored_profile = database.execute('SELECT inputs,execution_profile FROM photo.tasks WHERE tenant_id=%s AND id=%s',
        (args[0],task['node']['id'])).fetchone()
    assert stored_inputs==[task['transport']['manifest'],*task['transport']['direct_roots']]
    assert stored_profile['v2_input_manifest']==task['transport']['manifest']
    assert stored_profile['v2_entry_count']==task['transport']['entry_count']
    assert stored_profile['v2_entries_sha']==task['transport']['entries_sha']
    assert {k:v for k,v in stored_profile.items() if k not in ('contracts_sha','v2_input_manifest','v2_entry_count','v2_entries_sha')}==task['node']['execution_profile']
    assert database.execute("SELECT state,bytes,files,principal_type,principal_id,share_id FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",
        (args[0],child)).fetchone()==('reserved',task['node']['execution_profile']['resource_estimate']['output_bytes'],0,'user',args[2],None)
    assert database.execute("SELECT payload FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v27.review.batch'",
        (args[0],scope['parent_run'])).fetchall()==[(response,)]
    assert database.execute("SELECT payload->'result' FROM photo.outbox WHERE tenant_id=%s AND routing_key='v27.review.batch'",
        (args[0],)).fetchall()==[(response,)]
    assert database.execute('SELECT response_status,response_body,resource_id::text FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s',(args[0],key)).fetchone()==(200,response,response['batch_id'])
    assert database.execute('SELECT revision FROM photo.runs WHERE id=%s',(scope['parent_run'],)).fetchone()[0]==int(response['revision'])
    # Private SQL accepts new mutations only; API idempotency.Use owns replay.
    import psycopg
    import pytest
    with pytest.raises(psycopg.errors.SerializationFailure,match='SEMANTIC_SHOWN_RECEIPT_CONFLICT'):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_semantic_admission')
            database.execute('SELECT photo.commit_semantic_shown_background_cleanup_apply_review('+','.join(['%s']*12)+')',
                (*args[:4],key,int(params['workflow_revision']),request,decision,decision_body,Jsonb(scope['review_documents']),child,Jsonb([task])))
    assert database.execute('SELECT response_status,response_body FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s',(args[0],key)).fetchone()==(200,response)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(child,)).fetchone()[0]==1


def test_background_cleanup_late_event_cursor_failure_rolls_back_entire_apply(database):
    import hashlib
    from uuid import uuid4
    import psycopg
    import pytest
    from background_cleanup_apply_documents_fixture import admission_task_fixture
    values,task = admission_task_fixture(database)
    data,request,decision,decision_body,child = values[:5]
    args,preview,scope = data[:3]
    params = json.loads(args[5]); key = str(uuid4())
    database.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (args[0],args[2],key,hashlib.sha256(request.encode()).hexdigest()))
    # This real invariant fails after activation, parent revision and receipt
    # updates. A statement error must roll all of those writes back together.
    database.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=EXCLUDED.last_cursor',(args[0],))
    def snapshot():
        return (
            database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone(),
            database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(scope['parent_run'],)).fetchone(),
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone(),
            database.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s',(args[0],)).fetchone(),
            database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(args[0],)).fetchone())
    before = snapshot()
    with pytest.raises(psycopg.errors.CheckViolation,match='EVENT_CURSOR_EXHAUSTED'):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_semantic_admission')
            database.execute('SELECT photo.commit_semantic_shown_background_cleanup_apply_review('+','.join(['%s']*12)+')',
                (*args[:4],key,int(params['workflow_revision']),request,decision,decision_body,Jsonb(scope['review_documents']),child,Jsonb([task])))
    assert snapshot()==before
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(child,)).fetchone()[0]==0
    assert database.execute('SELECT count(*) FROM photo.v2_review_batches WHERE child_run_id=%s',(child,)).fetchone()[0]==0
    assert database.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],child)).fetchone()[0]==0
    assert database.execute('SELECT response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s',(args[0],key)).fetchone()==(None,None,None)


def test_background_cleanup_full_preflight_refuses_each_changed_document_and_disabled_policy(database):
    import copy
    from background_cleanup_apply_documents_fixture import admission_task_fixture
    values,task = admission_task_fixture(database)
    data,_,_,_,child = values[:5]
    args,_,scope = data[:3]
    params = json.loads(args[5])
    def ready(draft=task):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_semantic_admission')
            value = database.execute('SELECT photo.semantic_shown_background_cleanup_admission_documents_ready('+','.join(['%s']*15)+')',
                (*args[:4],child,args[4],scope['parent_run'],int(params['workflow_revision']),int(params['target_revision']),
                draft['params_body'],draft['params_sha'],Jsonb(draft['documents']),Jsonb(draft['node']),Jsonb(draft['transport']),Jsonb(draft['inventory']))).fetchone()[0]
            database.execute('RESET ROLE')
            return value
    assert ready() is True
    for name in task['documents']:
        changed = copy.deepcopy(task)
        changed['documents'][name] += ' '
        assert ready(changed) is False
    policy = task['node']['execution_profile']['qualification']['policy_id']
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(policy,))
        assert ready() is False
    assert ready() is True
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()==('proposed',None,None)
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0
