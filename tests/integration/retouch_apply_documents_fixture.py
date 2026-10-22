"""Own real review scope for retouch SQL scenarios, metadata-only object evidence.

No retouch preview receipt, CPU qualification, face provenance or Apply authority
is fabricated. Those additional producers must be constructed explicitly.
"""
from functools import partial
from hashlib import sha256
import json
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture
from background_cleanup_preview_documents_fixture import (
    working_image_builder,protection_builder,transfer_params_builder,candidate_builder,
    review_calibration_body,review_snapshot_builder,review_plan_builder,
)


def prepared_retouch_review_fixture(db,*,with_alpha=False,dense_warps=0,reference_dense_warps=None,objects=None,qualification_policies=None):
    return prepared_retouch_review_fixtures(db,with_alpha=with_alpha,dense_warps=dense_warps,
        reference_dense_warps=reference_dense_warps,target_count=1,objects=objects,qualification_policies=qualification_policies)[0]


def prepared_retouch_review_fixtures(db,*,with_alpha=False,dense_warps=0,reference_dense_warps=None,target_count=2,objects=None,qualification_policies=None):
    """Return each own typed target of one actual review, before preview admission."""
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import canonical_hash
    args,parent,scope,target=saved_authority_fixture(db,metadata_bodies=objects is None,target_count=target_count,objects=objects,qualification_policies=qualification_policies,
        source_builder=working_image_builder(with_alpha=with_alpha,dense_warps=dense_warps),protection_builder=protection_builder(db),
        transfer_params_builder=partial(transfer_params_builder,kind='retouch_region'),
        transfer_output_builder=candidate_builder(db,bind_transfer_model=True),calibration_body=review_calibration_body(),
        snapshot_builder=review_snapshot_builder,assembly_output_builder=review_plan_builder,
        reference_builder=None if reference_dense_warps is None else working_image_builder(with_alpha=with_alpha,dense_warps=reference_dense_warps))
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
    assert json.loads(scope['intent_body'])['kind']=='retouch_region'
    identity.pop('model_id')
    prepared=[]
    shared=('root','snapshot_sha','snapshot_body','plan_body','assembly_params_body','assembly','calibration_body')
    for row,producer in zip(scope['review_targets'],producers):
        own={**scope,**producer['scope']}
        own.update({key:scope[key] for key in shared if key in scope})
        image=WorkingImage.model_validate_json(own['source_body'])
        current=dict(identity,kind='retouch_region',target_id=str(row['target']),target_revision=str(row['revision']),
            source=dict(asset_version_id=str(row['version']),source=dict(artifact_id=str(own['source']),sha256=own['source_sha']),canvas_hash=canonical_hash(image.canvas)),
            candidate=dict(artifact_id=str(row['candidate']),sha256=sha256(own['candidate_body']).hexdigest()),
            protection=own['protection_documents']['target_protection']['ref'],
            model_lock_sha='1'*64,runtime_lock_sha='2'*64,device_profile_sha='3'*64)
        params=SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(current))
        raw=canonical_bytes(params).decode()
        own.update(review_documents=documents,retouch_params=params,retouch_params_body=raw,retouch_params_sha=sha256(raw.encode()).hexdigest())
        prepared.append(((*args[:4],row['target'],raw,own['retouch_params_sha']),parent,own,row['target']))
    assert len(prepared)==target_count
    return prepared


def retouch_context_producer_fixture(db,*,with_alpha=False,dense_warps=0,reference_dense_warps=None,review=None,objects=None,qualification_policies=None):
    """Real relational Face68 provenance plus own immutable draft content.

    Landmark coordinates are synthetic schema-valid test data. Ready object rows
    describe metadata only; this is no model/photographic qualification or preview
    receipt. The authority predicate itself is never replaced.
    """
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.images import Face68,FaceBox,NormalizedRect,Landmark,MaskRef,WorkingImage
    from photo_worker.contracts.face import FaceBoxes,LandmarkParams
    from photo_worker.contracts.beauty import BeautyFace
    from photo_worker.contracts.style import Beauty
    from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext

    args,parent,scope,target=review if review is not None else prepared_retouch_review_fixture(db,with_alpha=with_alpha,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,objects=objects,qualification_policies=qualification_policies)
    tenant,_,actor,_,_,_,_=args
    params=scope['retouch_params']
    image=WorkingImage.model_validate_json(scope['source_body'])
    face=Face68(object_id=uuid4(),canvas=image.canvas,transform_chain=image.transform_chain,
        points=[Landmark(x=.2+.01*(i%10),y=.2+.01*(i//10),confidence=1.) for i in range(68)])
    face_raw=canonical_bytes(face)
    face_sha=sha256(face_raw).hexdigest()
    task,artifact,worker=uuid4(),uuid4(),uuid4()
    source=params.source.source.model_dump(mode='json')
    # Schema-valid synthetic detector content and actual LandmarkParams bytes;
    # detector inference/provenance is outside this landmark-origin unit scope.
    def publish_landmark_bytes(aid,raw):
        if objects is None:return 'fixture','metadata-only-v1'
        from io import BytesIO
        client,bucket=objects
        uploaded=client.put_object(bucket,str(aid),BytesIO(raw),len(raw),content_type='application/json',
            metadata={'sha256':sha256(raw).hexdigest()})
        if uploaded.version_id in (None,'','null'):
            raise ValueError('landmark fixture requires versioned object storage')
        return bucket,uploaded.version_id
    def input_document(value):
        raw=canonical_bytes(value);digest=sha256(raw).hexdigest();aid=uuid4()
        bucket,version=publish_landmark_bytes(aid,raw)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp())",
            (aid,tenant,bucket,str(aid),digest,len(raw),version))
        return ArtifactRef(artifact_id=aid,sha256=digest)
    boxes=input_document(FaceBoxes(image=params.source.source,
        faces=[FaceBox(object_id=face.object_id,bbox_normalized=NormalizedRect(x0=.1,y0=.1,x1=.8,y1=.8),confidence=1.)],
        model_id='sfd',model_lock_id='1'*64,backend='cpu',preprocessing_id='sfd_long1024_bgr_v1',
        preview_width=image.canvas.width,preview_height=image.canvas.height,
        candidate_count=1,accepted_count=1,alpha_excluded_fraction=0.,warnings=[]))
    landmark_params=LandmarkParams(image=params.source.source,faces=boxes,model_lock_id='1'*64)
    landmark_params_ref=input_document(landmark_params)
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','completed',%s,%s,%s,%s,true) RETURNING id",
        (tenant,scope['snapshot'],Jsonb(dict(project_id=str(scope['project']))),'b'*64,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'landmark/00','face.landmarks','1','cpu','ready',%s,%s,%s,%s,1,1)",
        (task,tenant,run,Jsonb([source,boxes.model_dump(mode='json')]),landmark_params_ref.artifact_id,'c'*64,Jsonb(dict(backend='cpu'))))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",
        (tenant,task,worker,Jsonb(dict(backend='cpu'))))
    face_bucket,face_version=publish_landmark_bytes(artifact,face_raw)
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'face_landmarks',%s,1,1)",
        (artifact,tenant,face_bucket,str(artifact),face_sha,len(face_raw),face_version,task))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,1,%s,%s,%s,%s,'face_landmarks',%s,%s,%s,%s,'application/json','ready',%s,clock_timestamp())",
        (tenant,task,worker,'a'*64,uuid4(),artifact,face_bucket,str(artifact),face_sha,len(face_raw),face_version))
    face_ref=ArtifactRef(artifact_id=artifact,sha256=face_sha)
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
        (tenant,task,Jsonb([face_ref.model_dump(mode='json')]),'d'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    manual=MaskRef.model_validate_json(scope['candidate_mask_body'])
    context=RetouchPreviewAdapterContext(schema_version=1,algorithm='beautyfilter_v1',
        source=params.source,candidate=params.candidate,transferred_mask=ArtifactRef.model_validate_json(canonical_bytes(scope['candidate_mask_ref'])),
        protection=params.protection,faces=[BeautyFace(landmarks=face_ref,manual_mode=True,manual=manual)],
        beauty=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=.5,tone_evenness=.2,
            texture_keep=.8,skin_mask='face68_conservative_v1',protect_features=True),
        model_lock_sha=params.model_lock_sha,runtime_lock_sha=params.runtime_lock_sha,device_profile_sha=params.device_profile_sha)
    raw=canonical_bytes(context)
    scope.update(retouch_context=context,retouch_context_body=raw,retouch_context_ref=ArtifactRef(
        artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),landmark_task=task,landmark_run=run,
        landmark_result=result,landmark_ref=face_ref,landmark_body=face_raw,
        landmark_boxes_ref=boxes,landmark_params_ref=landmark_params_ref)
    return args,parent,scope,target


def save_retouch_context_draft(db,args,scope):
    ref=scope['retouch_context_ref']
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
        db.execute('SET LOCAL ROLE photo_api')
        run=db.execute('SELECT photo.save_semantic_retouch_preview('+','.join(['%s']*10)+')',
            (*args,ref.artifact_id,ref.sha256,scope['retouch_context_body'].decode())).fetchone()[0]
        db.execute('RESET ROLE')
        return run
