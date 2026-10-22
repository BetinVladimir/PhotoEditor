from retouch_historical_transport_fixture import manifest_bytes
"""Own API publication/quota/activation fixture; imported synthetic SQL evidence.

No operator measurement or physical object upload is implied. This helper only
runs inside explicitly provisioned, rolled-back integration test transactions.
All authority functions remain real and no production capability is enabled.
"""
from hashlib import sha256
import json
from uuid import UUID,uuid5
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from retouch_apply_documents_fixture import retouch_context_producer_fixture,save_retouch_context_draft
from semantic_qualification_fixture import frozen_qualification,qualification_policy_for

RETOUCH_CHECKS=[
    'retouch.real_photo_review','retouch.outside_mask_exact','retouch.protected_pixels_exact',
    'retouch.alpha_preserved','retouch.strength_zero_exact','retouch.strength_once','retouch.texture_keep',
    'retouch.full_face_roi','retouch.landmark_source','retouch.model_runtime_pins','retouch.cancellation',
    'retouch.resource_admission','retouch.hdr_refusal',
]


def prepared_retouch_preview_fixture(db,*,with_alpha=False,dense_warps=0,reference_dense_warps=None,reserve_quota=True,review=None,objects=None,qualification_policies=None,historical_writer_head=None):
    args,parent,scope,target=retouch_context_producer_fixture(db,with_alpha=with_alpha,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,review=review,objects=objects,qualification_policies=qualification_policies)
    run=save_retouch_context_draft(db,args,scope)
    task=uuid5(run,'semantic.preview_retouch_region:'+str(target))
    params=scope['retouch_params'].model_dump(mode='json')
    source=json.loads(scope['source_body'])
    reference=json.loads(scope.get('reference_body',scope['source_body']))
    candidate=json.loads(scope['candidate_body'])
    intent=json.loads(scope['intent_body'])
    mask=json.loads(scope['candidate_mask_body'])
    protection=json.loads(scope['protection_documents']['target_protection']['body'])
    context=scope['retouch_context'].model_dump(mode='json')
    context_ref=scope['retouch_context_ref'].model_dump(mode='json')
    params_ref=dict(artifact_id=str(uuid5(task,'Params')),sha256=args[6])
    roots=[(params_ref,'Params'),(params['plan'],'SemanticPlan'),(params['intent'],'EditIntent'),
        (params['candidate'],'TransferCandidate'),(params['source']['source'],'WorkingImage'),
        (params['protection'],'MaskRef'),(candidate['mask'],'MaskRef'),(candidate['transform'],'ResolvedTransform'),
        (intent['reference']['source'],'WorkingImage'),(source['pixels'],'NpyArray'),
        (mask['artifact'],'NpyArray'),(protection['artifact'],'NpyArray'),(context_ref,'RetouchPreviewAdapterContext')]
    if source.get('alpha') is not None:
        roots.append((source['alpha'],'NpyArray'))
    roots.extend((step['inverse_field'],'NpyArray') for step in source['transform_chain']['steps'] if step['kind']=='dense_warp')
    roots.append((reference['pixels'],'NpyArray'))
    if reference.get('alpha') is not None:
        roots.append((reference['alpha'],'NpyArray'))
    roots.extend((step['inverse_field'],'NpyArray') for step in reference['transform_chain']['steps'] if step['kind']=='dense_warp')
    for face in context['faces']:
        roots.append((face['landmarks'],'Face68' if face['object_id'] is None else 'FaceLandmarks'))
        roots.append((face['manual']['artifact'],'NpyArray'))
        if face['skin'] is not None:
            roots.append((face['skin']['artifact'],'NpyArray'))
    unique={}
    for ref,contract in roots:
        entry=dict(ref=ref,contract=contract)
        assert ref['artifact_id'] not in unique or unique[ref['artifact_id']]==entry
        unique[ref['artifact_id']]=entry
    entries=sorted(unique.values(),key=lambda e:UUID(e['ref']['artifact_id']).int)
    digest=lambda value:sha256(canonical_bytes(value)).hexdigest()
    entries_sha=digest(entries)
    chunk_body=canonical_bytes(dict(schema_version=1,entries=entries))
    chunk=dict(artifact_id=str(uuid5(task,'v2_input_chunk_0')),sha256=sha256(chunk_body).hexdigest())
    manifest_body=manifest_bytes(dict(schema_version=1,task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,chunks=[chunk]), historical_writer_head=historical_writer_head)
    manifest=dict(artifact_id=str(uuid5(task,'v2_input_manifest')),sha256=sha256(manifest_body).hexdigest())
    published=[('task_params',params_ref,args[5].encode()),
        ('retouch_adapter_context',context_ref,scope['retouch_context_body']),
        ('v2_input_chunk_0',chunk,chunk_body),('v2_input_manifest',manifest,manifest_body)]
    publication_base=(args[0],args[1],args[2],args[3],target,run,args[5],args[6])
    publication_bucket='fixture' if objects is None else objects[1]
    with db.transaction():
        # This positive fixture creates tenants directly, unlike bootstrap/Create,
        # so provision their normal usage ledger and an explicit operator budget.
        # Publication and admission still exercise the real quota checks.
        db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING',(args[0],))
        assert db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb(1073741824::bigint)) WHERE id=%s",(args[0],)).rowcount==1
        tenant_budget=db.execute(
            "SELECT t.quotas->>'storage_bytes',u.storage_bytes,u.reserved_bytes FROM photo.tenants t LEFT JOIN photo.tenant_usage u ON u.tenant_id=t.id WHERE t.id=%s",
            (args[0],)).fetchone()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
        db.execute('SET LOCAL ROLE photo_api')
        for purpose,ref,raw in published:
            assert ref['sha256']==sha256(raw).hexdigest()
            reserve=db.execute('SELECT * FROM photo.reserve_semantic_retouch_preview_document('+','.join(['%s']*14)+')',
                (*publication_base,purpose,task,ref['artifact_id'],ref['sha256'],len(raw),publication_bucket)).fetchone()
            assert reserve[0]=='created' and str(reserve[1])==ref['artifact_id'], dict(
                purpose=purpose,outcome=reserve[0],artifact_matches=str(reserve[1])==ref['artifact_id'],
                requested_bytes=len(raw),tenant_budget=tenant_budget)
            version='metadata-only-v1'
            if objects is not None:
                from io import BytesIO
                key=f"tenants/{args[0]}/runs/{run}/documents/{ref['artifact_id']}"
                uploaded=objects[0].put_object(publication_bucket,key,BytesIO(raw),len(raw),
                    content_type='application/json',metadata={'sha256':ref['sha256']})
                version=uploaded.version_id
                if version in (None,'','null'):raise ValueError('preview publication requires versioned storage')
            assert db.execute('SELECT photo.complete_semantic_retouch_preview_document('+','.join(['%s']*16)+')',
                (*publication_base,purpose,task,ref['artifact_id'],reserve[2],ref['sha256'],len(raw),publication_bucket,version)).fetchone()[0] is True
        db.execute('RESET ROLE')
    pin=scope.get('retouch_preview_qualification') or frozen_qualification(db,args[0],scope['project'],args[2],objects=objects,operation='semantic.preview_retouch_region',backend='cpu',
        model_lock_sha=params['model_lock_sha'],runtime_lock_sha=params['runtime_lock_sha'],
        device_profile_sha=params['device_profile_sha'],required_checks=RETOUCH_CHECKS,
        calibration_ref=scope['qualification']['calibration'],existing_policy=qualification_policy_for(qualification_policies,'semantic.preview_retouch_region'))
    scope['retouch_preview_qualification']=pin
    pixels=source['canvas']['width']*source['canvas']['height'];display=min(pixels,6553600)
    estimate=dict(ram_bytes=960*pixels+805306368+256*4194304+1,vram_bytes=0,
        scratch_bytes=64*pixels+64*(16*min(pixels,4194304)+16384)+67108864+4*16384+16777216+5*display+1048576+65536,
        output_bytes=12*pixels+16384+20*display+4*65536+8*1048576,pixel_count=pixels)
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate,qualification=pin,
        model_lock_id=params['model_lock_sha'],retouch_context=context_ref,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    if reserve_quota:
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT photo.reserve_semantic_retouch_preview_quota('+','.join(['%s']*10)+')',
                (*publication_base,scope['source_body'].decode(),Jsonb(profile))).fetchone()[0] is True
            db.execute('RESET ROLE')
    node=dict(id=str(task),node_key='semantic.preview_retouch_region:'+str(target),operation='semantic.preview_retouch_region',
        operation_version='1',resource_class='cpu',params_artifact_id=params_ref['artifact_id'],params_sha=args[6],
        input_digest=digest(dict(entries=entries,operation='semantic.preview_retouch_region',task=str(task),version='1')),
        execution_profile=profile)
    transport=dict(task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,manifest=manifest,chunks=[chunk],
        direct_roots=[entry['ref'] for entry in entries])
    inventory=dict(task_id=str(task),entries=entries)
    document_args=tuple((scope.get('reference_body',scope['source_body']) if key=='reference_body' else scope[key]).decode() for key in ('candidate_body','intent_body','source_body','reference_body',
        'candidate_mask_body','candidate_transform_body'))+(scope['protection_documents']['target_protection']['body'].decode(),)
    admission_args=(*publication_base,*document_args,Jsonb(node),Jsonb(transport),Jsonb(inventory))
    return dict(args=args,parent=parent,scope=scope,target=target,run=run,task=task,node=node,
        transport=transport,inventory=inventory,published=published,admission_args=admission_args,pin=pin)


def activate_retouch_preview_fixture(db,data,*,historical_schema_pin=None):
    from photo_worker.v2.contract_lock import COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
    if historical_schema_pin not in (None, COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256):
        raise ValueError('RETOUCH_FIXTURE_SCHEMA_PIN_INVALID')
    if historical_schema_pin not in (None, COPY_FENCE_SCHEMAS_SHA256):
        # Historical writer fixture, not an alteration of a sealed task and not
        # evidence that the current API emits an old build pin. Register actual
        # inputs, then write the historical immutable seal exactly once.
        with db.transaction():
            args=data['admission_args']; tenant=data['args'][0]
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_fn_owner')
            count=db.execute('SELECT photo.register_semantic_retouch_preview_inputs('+','.join(['%s']*18)+')',args).fetchone()[0]
            assert count==len(data['inventory']['entries'])+2
            node=data['node']; transport=data['transport']
            profile={**node['execution_profile'],'renderer_version':'v2.2','contracts_sha':historical_schema_pin,
                'v2_input_manifest':transport['manifest'],'v2_entry_count':transport['entry_count'],
                'v2_entries_sha':transport['entries_sha']}
            sealed=[transport['manifest'],*transport['direct_roots']]
            assert db.execute("UPDATE photo.tasks SET inputs=%s,execution_profile=%s WHERE tenant_id=%s AND id=%s AND run_id=%s AND state='blocked' AND attempt=0 AND NOT cancel_requested AND inputs='[]'::jsonb AND execution_profile=%s",
                (Jsonb(sealed),Jsonb(profile),tenant,data['task'],data['run'],Jsonb(node['execution_profile']))).rowcount==1
            assert db.execute('SELECT photo.semantic_retouch_preview_closure_ready(%s),photo.semantic_retouch_preview_run_current(%s,%s)',
                (data['task'],tenant,data['run'])).fetchone()==(True,True)
            assert db.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved' AND principal_type='user' AND principal_id=%s AND files=0 AND share_id IS NULL AND bytes=%s AND expires_at>clock_timestamp()",
                (tenant,data['run'],data['args'][2],profile['resource_estimate']['output_bytes'])).fetchone()==(1,)
            assert db.execute("UPDATE photo.tasks SET state='ready' WHERE tenant_id=%s AND id=%s AND state='blocked' AND attempt=0 AND NOT cancel_requested",(tenant,data['task'])).rowcount==1
            assert db.execute("UPDATE photo.runs SET graph_sealed=true,status='queued',revision=revision+1 WHERE tenant_id=%s AND id=%s AND kind='preview' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=%s AND revision<9007199254740991",
                (tenant,data['run'],data['args'][2])).rowcount==1
            db.execute('RESET ROLE')
            return data['task']
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(data['args'][0]),))
        db.execute('SET LOCAL ROLE photo_api')
        task=db.execute('SELECT photo.commit_semantic_retouch_preview('+','.join(['%s']*18)+')',data['admission_args']).fetchone()[0]
        db.execute('RESET ROLE')
        assert task==data['task']
        return task
