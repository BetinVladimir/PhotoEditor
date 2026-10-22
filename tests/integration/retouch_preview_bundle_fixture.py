"""Immutable eight-output retouch metadata fixture over an own current review.

Actual synthetic NPY/PNG and typed JSON bytes are built before metadata insertion.
No inference, ICC conversion, photographic quality or object-store proof is
claimed. Default mode isolates the bundle predicate without admission. Explicit
admitted mode uses real API activation with synthetic imported qualification
metadata, then records synthetic execution results for separate receipt tests.
"""
from hashlib import sha256
from io import BytesIO
from uuid import uuid4,uuid5

import numpy as np
from PIL import Image
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic_preview import RetouchPreviewNumericalEvidence,RetouchPreviewPresentation
from retouch_apply_documents_fixture import retouch_context_producer_fixture,save_retouch_context_draft


def retouch_bundle_fixture(db,*,with_alpha=False,mutation=None,admitted=False,dense_warps=0,reference_dense_warps=None,review=None,qualification_policies=None,historical_writer_head=None):
    if admitted:
        from retouch_preview_admission_fixture import prepared_retouch_preview_fixture,activate_retouch_preview_fixture
        prepared=prepared_retouch_preview_fixture(db,with_alpha=with_alpha,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,review=review,qualification_policies=qualification_policies,historical_writer_head=historical_writer_head)
        activate_retouch_preview_fixture(db,prepared)
        args,parent,scope,target,run=(prepared[key] for key in ('args','parent','scope','target','run'))
    else:
        args,parent,scope,target=retouch_context_producer_fixture(db,with_alpha=with_alpha,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,review=review,qualification_policies=qualification_policies)
        run=save_retouch_context_draft(db,args,scope)
    task=uuid5(run,'semantic.preview_retouch_region:'+str(target))
    worker=uuid4()
    params=scope['retouch_params']
    source=WorkingImage.model_validate_json(scope['source_body'])
    width,height=source.canvas.width,source.canvas.height
    bodies={};refs={};media={}

    def stage(role,raw,content_type):
        assert isinstance(raw,bytes)
        bodies[role]=raw
        refs[role]=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        media[role]=content_type
        return refs[role]

    stream=BytesIO()
    np.save(stream,np.full((height,width,3),.5,dtype=np.float32),allow_pickle=False)
    # This identity fixture shares bytes, never the source artifact identity.
    assert sha256(stream.getvalue()).hexdigest()==source.pixels.sha256
    pixels=stage('pixels',stream.getvalue(),'application/x-npy')
    image=source.model_copy(deep=True,update=dict(pixels=pixels))
    image_ref=stage('working_image',canonical_bytes(image),'application/json')
    evidence=RetouchPreviewNumericalEvidence(schema_version=1,params=params,working_image=image_ref,pixels=pixels,
        algorithm='beautyfilter_v1',changed_pixels=0,protected_pixels_unchanged=True,
        outside_mask_unchanged=True,alpha_unchanged=True,roi=[0,0,width,height])
    evidence_ref=stage('preview_evidence',canonical_bytes(evidence),'application/json')
    for role in ('before','after','affected','protected'):
        stream=BytesIO()
        png=Image.new('RGB',(width,height),(128,128,128)) if role in ('before','after') else Image.new('RGBA',(width,height),(0,0,0,0))
        try:
            png.save(stream,format='PNG')
        finally:
            png.close()
        stage(role,stream.getvalue(),'image/png')
    presentation=RetouchPreviewPresentation(schema_version=1,params=params,working_image=image_ref,pixels=pixels,
        numerical_evidence=evidence_ref,**{role:refs[role] for role in ('before','after','affected','protected')},
        width=width,height=height,icc_lock_sha='b'*64,srgb_sha='c'*64,
        resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')
    root=stage('preview_presentation',canonical_bytes(presentation),'application/json')
    params_raw=canonical_bytes(params)
    params_id=uuid5(task,'Params')
    if not admitted:
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json','metadata-only-v1',clock_timestamp())",
            (params_id,args[0],str(params_id),args[6],len(params_raw)))
    operation='semantic.preview_background_cleanup' if mutation=='foreign_operation' else 'semantic.preview_retouch_region'
    if not admitted:
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1','cpu','ready','[]',%s,%s,%s,1,1)",
            (task,args[0],run,'semantic.preview_retouch_region:'+str(target),operation,params_id,'e'*64,Jsonb(dict(backend='cpu',renderer_version='v2.2'))))
    else:
        assert mutation is None, 'admission branch does not mutate the approved producer'
        db.execute('UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s',(task,))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",
        (args[0],task,worker,Jsonb(prepared['node']['execution_profile'] if admitted else dict(backend='cpu'))))

    def publish(role,ref,raw,content_type):
        if mutation=='missing_'+role:
            return
        produced_fence=2 if mutation=='stale_'+role else 1
        actual_role='document' if mutation=='wrong_role_'+role else role
        actual_media='application/octet-stream' if mutation=='wrong_media_'+role else content_type
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'metadata-only-v1',clock_timestamp(),%s,%s,1,%s)",
            (ref.artifact_id,args[0],str(ref.artifact_id),ref.sha256,len(raw),actual_media,actual_role,task,produced_fence))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,%s,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,%s,'ready','metadata-only-v1',clock_timestamp())",
            (args[0],task,produced_fence,worker,'a'*64,uuid4(),ref.artifact_id,actual_role,str(ref.artifact_id),ref.sha256,len(raw),actual_media))

    for role,raw in bodies.items():
        publish(role,refs[role],raw,media[role])
    if mutation=='extra_output':
        publish('pixels',ArtifactRef(artifact_id=uuid4(),sha256=pixels.sha256),bodies['pixels'],media['pixels'])
    # A missing root cannot be named by a result: ready_artifact_reference
    # rejects dangling refs. Empty outputs is a legal initial incomplete result
    # and leaves the original missing-root inventory negative reachable.
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
        (args[0],task,Jsonb([] if mutation in ('missing_preview_presentation','empty_result') else [root.model_dump(mode='json')]),'f'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s",(run,))
    return dict(args=args,parent=parent,scope=scope,target=target,run=run,task=task,result=result,
        refs=refs,bodies=bodies,image=image,evidence=evidence,presentation=presentation)
