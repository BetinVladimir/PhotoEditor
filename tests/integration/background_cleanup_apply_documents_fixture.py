"""Own background_cleanup review fixture. Synthetic object metadata, real SQL authority.

Common source/protection/candidate builders are reused with explicit background_cleanup
intent; this does not turn a cleanup result into a background_cleanup result.
"""
import hashlib
import json
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture
from background_cleanup_preview_documents_fixture import (
    working_image_builder, protection_builder, transfer_params_builder,
    candidate_builder, review_calibration_body, review_snapshot_builder, review_plan_builder,
)


def prepared_review_fixture(db, *, with_alpha=False, objects=None):
    args, parent, scope, target = saved_authority_fixture(db, kind='background_cleanup',
        metadata_bodies=objects is None, objects=objects, source_builder=working_image_builder(with_alpha=with_alpha),
        protection_builder=protection_builder(db),
        transfer_params_builder=lambda scope, publish: transfer_params_builder(scope, publish, kind='background_cleanup'),
        transfer_output_builder=candidate_builder(db), calibration_body=review_calibration_body(),
        snapshot_builder=review_snapshot_builder, assembly_output_builder=review_plan_builder)
    bodies = dict(SemanticPlan=scope['plan_body'], Params=scope['assembly_params_body'],
        InputSnapshot=scope['snapshot_body'], EditIntent=scope['intent_body'],
        Calibration=scope['calibration_body'], TransferCandidate=scope['candidate_body'])
    assert json.loads(bodies['EditIntent'])['kind'] == 'background_cleanup'
    assert json.loads(args[5])['kind'] == 'background_cleanup'
    rows = db.execute('SELECT contract,artifact_id,sha256,bytes FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',
        (args[0], json.loads(args[5])['workflow_id'], args[2], args[3])).fetchall()
    assert len(rows) == 6 and {row[0] for row in rows} == set(bodies)
    documents = []
    for contract, artifact, digest, size in rows:
        raw = bodies[contract]
        assert len(raw) == size and hashlib.sha256(raw).hexdigest() == digest
        documents.append(dict(contract=contract, artifact_id=str(artifact), sha256=digest, body=raw.decode()))
    scope['review_documents'] = documents
    return args, parent, scope, target


def save_preview(db, args):
    """Actual own background_cleanup draft SQL; no activation or acceptance implied."""
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT photo.save_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def saved_preview_fixture(db, *, with_alpha=False):
    args, parent, scope, target = prepared_review_fixture(db, with_alpha=with_alpha)
    preview = save_preview(db, args)
    return args, parent, scope, target, preview


def published_decision_fixture(db):
    """Real decision publication over a sealed selection, before child creation.

    Completion supplies synthetic version metadata, not object-store proof.
    """
    from test_v27_background_cleanup_preview_receipt import receipt_fixture,seal
    data = receipt_fixture(db,review_documents=True)
    args,preview,scope = data[:3]
    receipt = seal(db,data)
    params = json.loads(args[5])
    candidate = json.loads(scope['candidate_body'])
    plan = next(d for d in scope['review_documents'] if d['contract']=='SemanticPlan')
    intent = next(d for d in scope['review_documents'] if d['contract']=='EditIntent')
    selection = dict(render=receipt['working_image'],evidence=receipt['presentation'])
    # The parent review run is authoritative; the preview is a separate run.
    parent = db.execute('SELECT w.run_id FROM photo.semantic_intents i JOIN photo.v2_workflows w ON w.id=i.workflow_id AND w.tenant_id=i.tenant_id WHERE i.id=%s',(args[1],)).fetchone()[0]
    request = dict(run_id=str(parent),request=dict(expected_revision=str(params['workflow_revision']),plan_sha=plan['sha256'],
        target_decisions=[dict(target_id=str(args[4]),decision='approve',preview=selection)]))
    body = dict(schema_version=1,kind='semantic_review',run_id=str(parent),expected_revision=str(params['workflow_revision']),
        plan={k:plan[k] for k in ('artifact_id','sha256')},intent={k:intent[k] for k in ('artifact_id','sha256')},
        snapshot_id=str(scope['snapshot']),targets=[dict(target_id=str(args[4]),target_revision=str(params['target_revision']),
            decision='approve',source=candidate['target'],candidate=params['candidate'],preview=selection)])
    encode = lambda value: json.dumps(value,sort_keys=True,separators=(',', ':'))
    request_raw,body_raw = encode(request),encode(body)
    request_sha = hashlib.sha256(request_raw.encode()).hexdigest()
    sha = hashlib.sha256(body_raw.encode()).hexdigest()
    base = (*args[:4],int(params['workflow_revision']),request_sha)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        reserved = db.execute('SELECT * FROM photo.reserve_semantic_decision_document('+','.join(['%s']*9)+')',
            (*base,sha,len(body_raw.encode()),'fixture')).fetchone()
        assert reserved[0]=='created' and reserved[3] is False
        assert db.execute('SELECT photo.complete_semantic_decision_document('+','.join(['%s']*12)+')',
            (*base,reserved[1],reserved[2],sha,len(body_raw.encode()),'fixture','decision-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return data,request_raw,reserved[1],body_raw


def approved_params_fixture(db, *, published=None, child=None):
    """Prepare canonical approved edit publication inputs without an Apply child."""
    from uuid import uuid4,uuid5
    data,request,decision_id,decision_body = published if published is not None else published_decision_fixture(db)
    args,_,scope = data[:3]
    params = json.loads(args[5])
    candidate,intent = json.loads(scope['candidate_body']),json.loads(scope['intent_body'])
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


def committed_copy_fixture(db, *, before_commit=None):
    """Real atomic SQL Apply metadata fixture, before worker claim."""
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    values,task = admission_task_fixture(db)
    data,request,decision,body,child = values[:5]
    args = data[0]
    revision = int(json.loads(args[5])['workflow_revision'])
    key = str(uuid4())
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (args[0],args[2],key,hashlib.sha256(request.encode()).hexdigest()))
    if before_commit is not None:
        before_commit(db, values, task)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        response = db.execute('SELECT photo.commit_semantic_shown_background_cleanup_apply_review('+','.join(['%s']*12)+')',
            (*args[:4],key,revision,request,decision,body,Jsonb(data[2]['review_documents']),child,Jsonb([task]))).fetchone()[0]
        db.execute('RESET ROLE')
    return values,task,response


def prepared_background_cleanup_review_fixtures(db,*,with_alpha=False,target_count=2,dense_warps=0):
    """Return each own typed target of one actual review, before preview admission."""
    from functools import partial
    from hashlib import sha256
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import canonical_hash
    args,parent,scope,target=saved_authority_fixture(db,metadata_bodies=True,target_count=target_count,kind="background_cleanup",
        source_builder=working_image_builder(with_alpha=with_alpha,dense_warps=dense_warps),protection_builder=protection_builder(db),
        transfer_params_builder=partial(transfer_params_builder,kind='background_cleanup'),
        transfer_output_builder=candidate_builder(db),calibration_body=review_calibration_body(),
        snapshot_builder=review_snapshot_builder,assembly_output_builder=review_plan_builder)
    common=dict(SemanticPlan=scope['plan_body'],Params=scope['assembly_params_body'],
        InputSnapshot=scope['snapshot_body'],EditIntent=scope['intent_body'],Calibration=scope['calibration_body'])
    producers=scope.get('target_producers') or [dict(scope=scope,candidate=json.loads(args[5])['candidate']['artifact_id'])]
    candidate_bodies={str(entry['candidate']):entry['scope']['candidate_body'] for entry in producers}
    identity=json.loads(args[5])
    rows=db.execute('SELECT contract,artifact_id,sha256,bytes FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',
        (args[0],identity['workflow_id'],args[2],args[3])).fetchall()
    assert len(rows)==5+target_count and {row[0] for row in rows}==set(common)|{'TransferCandidate'}
    documents=[]
    for contract,aid,digest,size in rows:
        raw=candidate_bodies[str(aid)] if contract=='TransferCandidate' else common[contract]
        assert len(raw)==size and sha256(raw).hexdigest()==digest
        documents.append(dict(contract=contract,artifact_id=str(aid),sha256=digest,body=raw.decode()))
    assert json.loads(scope['intent_body'])['kind']=='background_cleanup'
    prepared=[]
    shared=('root','snapshot_sha','snapshot_body','plan_body','assembly_params_body','assembly','calibration_body')
    for row,producer in zip(scope['review_targets'],producers):
        own={**scope,**producer['scope']}
        own.update({key:scope[key] for key in shared if key in scope})
        image=WorkingImage.model_validate_json(own['source_body'])
        current=dict(identity,kind='background_cleanup',context_pixels=128,feather_pixels=8,target_id=str(row['target']),target_revision=str(row['revision']),
            source=dict(asset_version_id=str(row['version']),source=dict(artifact_id=str(own['source']),sha256=own['source_sha']),canvas_hash=canonical_hash(image.canvas)),
            candidate=dict(artifact_id=str(row['candidate']),sha256=sha256(own['candidate_body']).hexdigest()),
            protection=own['protection']['target_protection'],
            model_lock_sha='1'*64,runtime_lock_sha='2'*64,device_profile_sha='3'*64)
        params=SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(current))
        raw=canonical_bytes(params).decode()
        own.update(review_documents=documents,background_cleanup_params=params,background_cleanup_params_body=raw,background_cleanup_params_sha=sha256(raw.encode()).hexdigest())
        prepared.append(((*args[:4],row['target'],raw,own['background_cleanup_params_sha']),parent,own,row['target']))
    assert len(prepared)==target_count
    return prepared


