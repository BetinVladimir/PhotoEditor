"""Own removal activation fixture: real SQL, synthetic versioned object metadata.

No worker execution, photographic qualification or physical store proof.
"""
import hashlib
import json
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from photo_worker.v2.removal_qualification_report import REMOVAL_REQUIRED_CHECKS
from semantic_qualification_fixture import frozen_qualification
from removal_apply_documents_fixture import prepared_review_fixture, save_preview


def prepared_params_fixture(db, *, with_alpha=False, review_documents=True, review=None, objects=None):
    args, parent, scope, target = review if review is not None else prepared_review_fixture(db, with_alpha=with_alpha, objects=objects)
    # Explicit capacity for positive admission; quota tests override it later.
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas, '{storage_bytes}', '107374182400'::jsonb) WHERE id=%s", (args[0],))
    pin = scope.get('removal_preview_qualification')
    if pin is None:
        pin = frozen_qualification(db, args[0], scope['project'], scope['actor'],
            objects=objects, operation='semantic.preview_remove_object', backend='cpu', required_checks=sorted(REMOVAL_REQUIRED_CHECKS))
    body = json.loads(args[5])
    body.update(protection=scope['protection']['target_protection'], context_pixels=128, feather_pixels=8,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    typed = SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(body))
    raw = canonical_bytes(typed).decode()
    args = (*args[:5], raw, hashlib.sha256(raw.encode()).hexdigest())
    run = save_preview(db, args)
    image = json.loads(scope['source_body']); pixels = image['canvas']['width']*image['canvas']['height']
    display = min(pixels, 2560*2560)
    estimate = dict(ram_bytes=768*pixels+536870912+1, vram_bytes=0,
        scratch_bytes=32*pixels+4*16384+16777216+5*display+1048576+65536,
        output_bytes=12*pixels+16384+20*display+4*65536+8*1048576, pixel_count=pixels)
    profile = dict(backend='cpu', renderer_version='v2.2', resource_estimate=estimate,
        model_lock_id=pin['model_lock_sha'], qualification=pin,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    scope.update(preview_params=typed, preview_profile=profile, parent_run=parent, target=target)
    return args, run, scope


def reserve(db,args,run,source,profile):
    if isinstance(source,bytes):
        source = source.decode('utf-8')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT photo.reserve_semantic_removal_preview_quota(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (*args[:5],run,args[5],args[6],source,Jsonb(profile))).fetchone()[0]
        db.execute('RESET ROLE')
        return result

def publish_document(db,args,run,task,purpose,raw,*,objects=None):
    from uuid import uuid5
    ref = dict(artifact_id=str(uuid5(task,'Params' if purpose=='task_params' else purpose)),sha256=hashlib.sha256(raw).hexdigest())
    bucket='fixture' if objects is None else objects[1]
    values = (*args[:5],run,args[5],args[6],purpose,task,ref['artifact_id'],ref['sha256'],len(raw),bucket)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        outcome,aid,token,ready = db.execute('SELECT * FROM photo.reserve_semantic_removal_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()
        assert outcome in ('created','resumed','replayed') and str(aid)==ref['artifact_id']
        if not ready:
            version='metadata-only-v1'
            if objects is not None:
                from io import BytesIO
                # Use the exact production locator, not an unrelated prefix.
                key=f"tenants/{args[0]}/runs/{run}/documents/{ref['artifact_id']}"
                result=objects[0].put_object(bucket,key,BytesIO(raw),len(raw),content_type='application/json',metadata={'sha256':ref['sha256']})
                version=result.version_id
                if version in (None,'','null'):
                    raise ValueError('removal publication requires versioned storage')
            assert db.execute('SELECT photo.complete_semantic_removal_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (*args[:5],run,args[5],args[6],purpose,task,aid,token,ref['sha256'],len(raw),bucket,version)).fetchone()[0] is True
        db.execute('RESET ROLE')
    return ref

def published_transport_fixture(db, *, with_alpha=False, review_documents=False, review=None, objects=None):
    import json
    from uuid import UUID,uuid5
    args,run,scope = prepared_params_fixture(db,with_alpha=with_alpha,review_documents=review_documents,review=review,objects=objects)
    # This positive transport owns accounting; lower Params-only callers retain
    # missing-usage/capacity refusal control. Do not fabricate a reservation.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING', (args[0],))
    assert reserve(db,args,run,scope['source_body'].decode(),scope['preview_profile']) is True
    p = scope['preview_params'].model_dump(mode='json')
    task = uuid5(run,'semantic.preview_remove_object:'+p['target_id'])
    params = publish_document(db,args,run,task,'task_params',args[5].encode(),objects=objects)
    candidate = json.loads(scope['candidate_body'])
    source = json.loads(scope['source_body'])
    intent = json.loads(scope['intent_body'])
    reference = json.loads(scope.get('reference_body', scope['source_body']))
    assert hashlib.sha256(canonical_bytes(reference)).hexdigest() == intent['reference']['source']['sha256']
    mask = json.loads(scope['candidate_mask_body'])
    protection = json.loads(scope['protection_documents']['target_protection']['body'])
    roots = [(params,'Params'),(p['plan'],'SemanticPlan'),(p['intent'],'EditIntent'),
        (p['candidate'],'TransferCandidate'),(p['source']['source'],'WorkingImage'),(p['protection'],'MaskRef'),
        (candidate['mask'],'MaskRef'),(candidate['transform'],'ResolvedTransform'),
        (source['pixels'],'NpyArray'),(mask['artifact'],'NpyArray'),(protection['artifact'],'NpyArray')]
    roots.append((intent['reference']['source'], 'WorkingImage'))
    for image in (source, reference):
        roots.append((image['pixels'], 'NpyArray'))
        if image.get('alpha') is not None:
            roots.append((image['alpha'], 'NpyArray'))
        for step in image['transform_chain']['steps']:
            if step['kind'] == 'dense_warp':
                roots.append((step['inverse_field'], 'NpyArray'))
    unique = {}
    for ref, contract in roots:
        entry = dict(ref=ref, contract=contract)
        identity = ref['artifact_id']
        assert identity not in unique or unique[identity] == entry
        unique[identity] = entry
    entries = list(unique.values())
    entries.sort(key=lambda e:UUID(e['ref']['artifact_id']).int)
    entries_sha = hashlib.sha256(canonical_bytes(entries)).hexdigest()
    digest = hashlib.sha256(canonical_bytes(dict(entries=entries,operation='semantic.preview_remove_object',task=str(task),version='1'))).hexdigest()
    chunk = publish_document(db,args,run,task,'v2_input_chunk_0',canonical_bytes(dict(entries=entries,schema_version=1)),objects=objects)
    manifest = publish_document(db,args,run,task,'v2_input_manifest',canonical_bytes(dict(chunks=[chunk],entry_count=len(entries),entries_sha=entries_sha,schema_version=1,task_id=str(task))),objects=objects)
    node = dict(id=str(task),node_key='semantic.preview_remove_object:'+p['target_id'],
        operation='semantic.preview_remove_object',operation_version='1',resource_class='gpu',
        params_artifact_id=params['artifact_id'],params_sha=args[6],input_digest=digest,execution_profile=scope['preview_profile'])
    transport = dict(task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,manifest=manifest,chunks=[chunk],direct_roots=[e['ref'] for e in entries])
    inventory = dict(task_id=str(task),entries=entries)
    scope.update(preview_node=node,preview_transport=transport,preview_inventory=inventory)
    return args,run,scope

def commit_preview(db,args,run,scope):
    from psycopg.types.json import Jsonb
    docs = (scope['candidate_body'],scope['intent_body'],scope['source_body'],scope.get('reference_body',scope['source_body']),
            scope['candidate_mask_body'],scope['candidate_transform_body'],
            scope['protection_documents']['target_protection']['body'])
    values = (*args[:5],run,args[5],args[6],*(raw.decode() for raw in docs),
        Jsonb(scope['preview_node']),Jsonb(scope['preview_transport']),Jsonb(scope['preview_inventory']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        task = db.execute('SELECT photo.commit_semantic_removal_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
        db.execute('RESET ROLE')
        return task

