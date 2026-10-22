"""Own cleanup decision/copy SQL fixtures over actual cleanup previews.

Original cleanup intent and cleanup receipt are required. Imported qualification and
versioned object metadata are synthetic; no physical or quality qualification.
"""
import hashlib
import json

def publish_cleanup_decision_bundles(db,bundles,*,reverse_targets=False,decisions=None):
    """Publish one decision for genuinely admitted targets in one review.

    This does not create additional targets or project synthetic producer IDs.
    Approved targets require their own admitted preview bundle; rejected targets
    require only the current review source and no preview. Scope and decision
    inventory are checked before any receipt/publication write.
    """
    from cleanup_mixed_preview_receipt_fixture import seal
    if type(reverse_targets) is not bool:
        raise ValueError('reverse_targets must be boolean')
    if not bundles or len(bundles)>100:
        raise ValueError('cleanup decision requires 1..100 admitted bundles')
    first=bundles[0]
    args,scope=first['args'],first['scope']
    params=json.loads(args[5])
    documents=scope['review_documents']
    plan=next(d for d in documents if d['contract']=='SemanticPlan')
    intent=next(d for d in documents if d['contract']=='EditIntent')
    shared=(args[:4],params['workflow_id'],params['workflow_revision'],scope['snapshot'],documents)
    seen=set()
    ordered=sorted(bundles,key=lambda bundle:str(bundle['target']),reverse=reverse_targets)
    for bundle in ordered:
        other_args,other_scope=bundle['args'],bundle['scope']
        other=json.loads(other_args[5])
        identity=(other_args[:4],other['workflow_id'],other['workflow_revision'],other_scope['snapshot'],other_scope['review_documents'])
        if identity!=shared or bundle['target'] in seen or str(bundle['target'])!=other['target_id'] or str(other_args[4])!=other['target_id']:
            raise ValueError('cleanup decision bundles must be distinct targets of one review')
        if other['plan']!={k:plan[k] for k in ('artifact_id','sha256')} or other['intent']!={k:intent[k] for k in ('artifact_id','sha256')}:
            raise ValueError('cleanup decision plan/intent mismatch')
        seen.add(bundle['target'])
    choices_by_id = {str(bundle['target']): 'approve' for bundle in bundles} if decisions is None else {
        str(target): decision for target, decision in decisions.items()}
    if (decisions is not None and len(choices_by_id) != len(decisions)
            or set(choices_by_id) != {str(target) for target in seen}
            or any(decision not in ('approve', 'reject') for decision in choices_by_id.values())):
        raise ValueError('cleanup decisions must name exactly the selected bundles')
    parent=db.execute('SELECT w.run_id FROM photo.semantic_intents i JOIN photo.v2_workflows w '
        'ON w.id=i.workflow_id AND w.tenant_id=i.tenant_id WHERE i.tenant_id=%s AND i.id=%s',args[:2]).fetchone()[0]
    choices=[];targets=[]
    for bundle in ordered:
        current=json.loads(bundle['args'][5])
        candidate=json.loads(bundle['scope']['candidate_body'])
        decision = choices_by_id[str(bundle['target'])]
        choice = dict(target_id=str(bundle['target']), decision=decision)
        target = dict(target_id=str(bundle['target']), target_revision=current['target_revision'],
            decision=decision, source=candidate['target'], candidate=current['candidate'])
        if decision == 'approve':
            receipt=seal(db,bundle["data"])
            selection=dict(render=receipt['working_image'],evidence=receipt['presentation'])
            choice['preview'] = selection
            target['preview'] = selection
        choices.append(choice)
        targets.append(target)
    request=dict(run_id=str(parent),request=dict(expected_revision=params['workflow_revision'],plan_sha=plan['sha256'],target_decisions=choices))
    body=dict(schema_version=1,kind='semantic_review',run_id=str(parent),expected_revision=params['workflow_revision'],
        plan={k:plan[k] for k in ('artifact_id','sha256')},intent={k:intent[k] for k in ('artifact_id','sha256')},
        snapshot_id=str(scope['snapshot']),targets=targets)
    encode=lambda value:json.dumps(value,sort_keys=True,separators=(',', ':'))
    request_raw,body_raw=encode(request),encode(body)
    request_sha=hashlib.sha256(request_raw.encode()).hexdigest()
    sha=hashlib.sha256(body_raw.encode()).hexdigest()
    base=(*args[:4],int(params['workflow_revision']),request_sha)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        reserved=db.execute('SELECT * FROM photo.reserve_semantic_decision_document('+','.join(['%s']*9)+')',
            (*base,sha,len(body_raw.encode()),'fixture')).fetchone()
        assert reserved[0]=='created' and reserved[3] is False
        assert db.execute('SELECT photo.complete_semantic_decision_document('+','.join(['%s']*12)+')',
            (*base,reserved[1],reserved[2],sha,len(body_raw.encode()),'fixture','decision-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return request_raw,reserved[1],body_raw




def approved_params_fixture(db, *, published=None, child=None):
    """Prepare canonical approved edit publication inputs without an Apply child."""
    from uuid import uuid4,uuid5
    if published is None:
        raise ValueError("explicit own cleanup decision required")
    data,request,decision_id,decision_body = published
    args,_,scope = data[:3]
    params = json.loads(args[5])
    candidate,intent = json.loads(scope['candidate_body']),json.loads(scope['intent_body'])
    assert intent["kind"] == "background_cleanup" and intent["ev"] is None
    child = child if child is not None else uuid4()
    owner = uuid5(child,'semantic.edit:'+str(args[4]))
    aid = uuid5(owner,'Params')
    edit = dict(schema_version=1,target_id=str(args[4]),source=candidate['target'],intent=params['intent'],
        candidate=params['candidate'],mask=candidate['mask'],transform=candidate['transform'],
        kind=intent['kind'],strength=intent['strength'],ev=intent.get('ev'),prompt=intent.get('prompt'),
        preserve_persons=intent['preserve_persons'])
    from photo_worker.contracts.base import canonical_bytes
    raw = canonical_bytes(edit).decode()
    sha = hashlib.sha256(raw.encode()).hexdigest()
    base = (*args[:4],int(params['workflow_revision']),args[4],int(params['target_revision']),
        candidate['target']['asset_version_id'],params['candidate']['artifact_id'],params['candidate']['sha256'])
    return data,request,decision_id,decision_body,child,owner,aid,raw,sha,base


def shown_copy_params_fixture(db, *, approved=None):
    """Publish approved edit, then prepare receipt-bound own copy Params."""
    from uuid import uuid5
    values = approved if approved is not None else approved_params_fixture(db)
    data,request,decision,decision_body,child,owner,aid,raw,sha,base = values
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        reserved = db.execute('SELECT * FROM photo.reserve_semantic_approved_params_document('+','.join(['%s']*15)+')',
            (*base,owner,aid,sha,len(raw.encode()),'fixture')).fetchone()
        assert reserved[0]=='created'
        assert db.execute('SELECT photo.complete_semantic_approved_params_document('+','.join(['%s']*17)+')',
            (*base,owner,aid,reserved[2],sha,len(raw.encode()),'fixture','approved-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    args,preview = data[:2]
    params = json.loads(args[5])
    receipt = db.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(preview,)).fetchone()[0]
    copy = dict(schema_version=1,tenant_id=str(args[0]),child_run_id=str(child),target_id=str(args[4]),
        approved_edit=dict(artifact_id=str(aid),sha256=sha),preview_run_id=str(preview),
        preview_task_id=receipt['task_id'],preview_result_id=receipt['result_id'],
        preview_attempt=receipt['attempt'],preview_fence=receipt['fence'],
        **{key:receipt[key] for key in ('presentation','numerical_evidence','working_image','pixels')},
        **{key:params[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    copy_raw = json.dumps(copy,sort_keys=True,separators=(',', ':'))
    copy_sha = hashlib.sha256(copy_raw.encode()).hexdigest()
    task = uuid5(child,'semantic.adopt_shown_background_cleanup:'+str(args[4]))
    copy_id = uuid5(task,'Params')
    publication = (*args[:4],int(params['workflow_revision']),args[4],int(params['target_revision']),
        params['plan']['sha256'],params['candidate']['sha256'],copy_raw,copy_sha,child)
    return values,copy,task,copy_id,copy_raw,copy_sha,publication


def shown_copy_transport_fixture(db, *, approved=None):
    """Exact dependency/transport projection; execution profile supplied later."""
    from uuid import UUID,uuid5
    values,copy,task,aid,raw,sha,base = shown_copy_params_fixture(db, approved=approved)
    data = values[0]
    args,_,scope,presentation = data[:4]
    params = json.loads(args[5])
    candidate,intent,source,mask = [json.loads(scope[key]) for key in
        ('candidate_body','intent_body','source_body','candidate_mask_body')]
    reference_body = scope.get('reference_body', scope['source_body'])
    reference = json.loads(reference_body)
    assert hashlib.sha256(reference_body).hexdigest() == intent['reference']['source']['sha256']
    assert candidate['target']==params['source']
    protection = json.loads(scope['protection_documents']['target_protection']['body'])
    roots = [(dict(artifact_id=str(aid),sha256=sha),'Params'),(copy['approved_edit'],'ApprovedSemanticEdit'),
        (copy['presentation'],'BackgroundCleanupPreviewPresentation'),
        (copy['numerical_evidence'],'BackgroundCleanupPreviewNumericalEvidence'),
        (copy['working_image'],'WorkingImage'),(copy['pixels'],'NpyArray')]
    roots += [(presentation[k],'EncodedImage') for k in ('before','after','affected','protected')]
    roots += [(params['plan'],'SemanticPlan'),(params['intent'],'EditIntent'),(params['candidate'],'TransferCandidate'),
        (params['source']['source'],'WorkingImage'),(params['protection'],'MaskRef'),
        (candidate['mask'],'MaskRef'),(candidate['transform'],'ResolvedTransform'),
        (intent['reference']['source'],'WorkingImage'),(source['pixels'],'NpyArray'),
        (reference['pixels'],'NpyArray'),(mask['artifact'],'NpyArray'),(protection['artifact'],'NpyArray')]
    for image in (source,reference):
        if image.get('alpha') is not None: roots.append((image['alpha'],'NpyArray'))
        roots += [(step['inverse_field'],'NpyArray') for step in image['transform_chain']['steps'] if step['kind']=='dense_warp']
    unique = {}
    for ref,contract in roots:
        entry = dict(ref=ref,contract=contract)
        assert ref['artifact_id'] not in unique or unique[ref['artifact_id']]==entry
        unique[ref['artifact_id']] = entry
    entries = sorted(unique.values(),key=lambda e:UUID(e['ref']['artifact_id']).int)
    encode = lambda body: json.dumps(body,sort_keys=True,separators=(',', ':')).encode()
    digest = lambda body: hashlib.sha256(encode(body)).hexdigest()
    entries_sha = digest(entries)
    documents = [('task_params',raw.encode()),('v2_input_chunk_0',encode(dict(entries=entries,schema_version=1)))]
    chunk = dict(artifact_id=str(uuid5(task,'v2_input_chunk_0')),sha256=hashlib.sha256(documents[1][1]).hexdigest())
    documents.append(('v2_input_manifest',encode(dict(chunks=[chunk],entry_count=len(entries),entries_sha=entries_sha,schema_version=1,task_id=str(task)))))
    published = {}
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        for purpose,body in documents:
            doc_id = uuid5(task,'Params' if purpose=='task_params' else purpose)
            doc_sha = hashlib.sha256(body).hexdigest()
            reserved = db.execute('SELECT * FROM photo.reserve_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*18)+')',
                (*base,purpose,task,doc_id,doc_sha,len(body),'fixture')).fetchone()
            assert reserved[0]=='created'
            assert db.execute('SELECT photo.complete_semantic_shown_background_cleanup_transport_document('+','.join(['%s']*20)+')',
                (*base,purpose,task,doc_id,reserved[2],doc_sha,len(body),'fixture','copy-transport-fixture-v1')).fetchone()[0] is True
            published[purpose] = dict(artifact_id=str(doc_id),sha256=doc_sha)
        db.execute('RESET ROLE')
    node = dict(id=str(task),node_key='semantic.adopt_shown_background_cleanup:'+copy['target_id'],
        operation='semantic.adopt_shown_background_cleanup',operation_version='1',resource_class='cpu',
        params_artifact_id=str(aid),params_sha=sha,input_digest=digest(dict(entries=entries,operation='semantic.adopt_shown_background_cleanup',task=str(task),version='1')),
        execution_profile={})  # Pure transport gate does not qualify execution.
    transport = dict(task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,
        manifest=published['v2_input_manifest'],chunks=[published['v2_input_chunk_0']],direct_roots=[e['ref'] for e in entries])
    inventory = dict(task_id=str(task),entries=entries)
    return values,copy,node,transport,inventory


def admission_task_fixture(db, *, approved=None, qualification=None):
    """Own imported CPU qualification metadata; no measured hardware claim."""
    from semantic_qualification_fixture import frozen_qualification
    values,params,node,transport,inventory = shown_copy_transport_fixture(db, approved=approved)
    data = values[0]
    args,_,scope = data[:3]
    image = json.loads(data[6])
    count = image['canvas']['width']*image['canvas']['height']
    pin = qualification if qualification is not None else frozen_qualification(db,args[0],scope['project'],args[2],
        operation='semantic.adopt_shown_background_cleanup',backend='cpu',
        model_lock_sha=params['model_lock_sha'],runtime_lock_sha=params['runtime_lock_sha'])
    assert pin['device_profile_sha']==params['device_profile_sha']
    node['execution_profile'] = dict(backend='cpu',renderer_version='v2.2',qualification=pin,
        resource_estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*count+32768+16777216,
            output_bytes=12*count+16384+1048576,pixel_count=count),
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    docs = dict(approved=values[7],presentation=data[4],numerical=data[5],image=data[6],
        **{name:scope[key].decode() for name,key in (('candidate','candidate_body'),('intent','intent_body'),
            ('source','source_body'),('mask','candidate_mask_body'))},
        reference=scope.get('reference_body',scope['source_body']).decode(),
        protection=scope['protection_documents']['target_protection']['body'].decode())
    task = dict(target_id=params['target_id'],params_artifact_id=node['params_artifact_id'],params_sha=node['params_sha'],
        params_body=json.dumps(params,sort_keys=True,separators=(',', ':')),node=node,transport=transport,inventory=inventory,documents=docs)
    return values,task

