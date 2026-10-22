"""Schema-valid tiny source documents for SQL metadata fixtures.

Publishing metadata does not verify object storage, ICC execution or photo quality.
Builders run only when explicitly called by a test, never during module import.
"""
from hashlib import sha256
from io import BytesIO
from pathlib import Path


def working_image_builder(*, with_alpha=False, dense_warps=0):
    if type(dense_warps) is not int or not 0 <= dense_warps <= 128:
        raise ValueError('dense_warps must respect TransformChain limit 128')
    def build(publish, original_sha):
        import numpy as np
        from photo_worker.contracts.base import ArtifactRef, canonical_bytes
        from photo_worker.contracts.images import Canvas, ColorSpace, WorkingImage, TransformChain, DenseWarpTransform
        def array_ref(values):
            stream = BytesIO()
            np.save(stream,values,allow_pickle=False)
            raw = stream.getvalue()
            digest = sha256(raw).hexdigest()
            artifact = publish(digest,media='application/x-npy',body=raw)
            return ArtifactRef(artifact_id=artifact,sha256=digest)
        pixels = array_ref(np.full((16,16,3),0.5,dtype=np.float32))
        alpha = array_ref(np.full((16,16),0.5,dtype=np.float32)) if with_alpha else None
        profile = Path(__file__).resolve().parents[2]/'resources/icc/work_rec2020_gamma24_v1.icc'
        canvas=Canvas(width=16,height=16,orientation=1,space_id='oriented',source_sha256=original_sha)
        steps=[]
        for index in range(dense_warps):
            # Valid distinct inverse fields, published before immutable image
            # bytes. Tiny offsets distinguish their hashes without huge arrays.
            yy,xx=np.mgrid[:16,:16]
            field=np.stack((xx,yy),axis=-1).astype(np.float32)
            field[...,0]+=np.float32((index+1)/4096)
            steps.append(DenseWarpTransform(kind='dense_warp',from_canvas=canvas,to_canvas=canvas,
                invertibility='invertible',valid_region=dict(x=0,y=0,width=16,height=16),
                inverse_field=array_ref(field)))
        image = WorkingImage(pixels=pixels,alpha=alpha,
            canvas=canvas,
            color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256=sha256(profile.read_bytes()).hexdigest(),encoding='gamma24'),
            dtype='float32',transform_chain=TransformChain.create(steps))
        return canonical_bytes(image)
    return build


def protection_builder(db):
    """Build real relational rasterize provenance; uploaded bytes depend on publisher."""
    def build(scope, publish, tenant):
        import numpy as np
        from uuid import uuid4
        from psycopg.types.json import Jsonb
        from photo_worker.contracts.base import ArtifactRef, canonical_bytes
        from photo_worker.contracts.images import MaskRef, WorkingImage
        image = WorkingImage.model_validate_json(scope['source_body'])
        producer = uuid4()
        mask_run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','running',%s,%s,%s,%s,true) RETURNING id",
            (tenant,scope['snapshot'],Jsonb(dict(project_id=str(scope['project']))),'b'*64,scope['actor'])).fetchone()[0]
        db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) VALUES(%s,%s,%s,'mask_manual',0,'mask.rasterize',%s,'[]','{}',%s,'cpu',%s,'complete')",
            (tenant,mask_run,producer,scope['source'],'c'*64,'d'*64))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'mask/00','mask.rasterize','1','cpu','ready','[]',%s,%s,'{}',1,1)",
            (producer,tenant,mask_run,scope['source'],'e'*64))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",
            (tenant,producer,uuid4()))
        roots, outputs = {}, []
        scope['protection_pixel_inputs'] = []
        scope['protection_documents'] = {}
        for name in ('source_protection','target_protection'):
            image = WorkingImage.model_validate_json(scope.get('reference_body',scope['source_body'])
                if name=='source_protection' else scope['source_body'])
            values = np.zeros((image.canvas.height,image.canvas.width),dtype=np.float32)
            values[0,0] = 1
            stream = BytesIO()
            np.save(stream,values,allow_pickle=False)
            raw = stream.getvalue()
            digest = sha256(raw).hexdigest()
            # Protected rasterizer outputs use the legacy binary media contract.
            # The payload remains NPY; candidate mask_pixels below uses x-npy.
            binary = publish(digest,task=producer,media='application/octet-stream',body=raw)
            mask = MaskRef(artifact=ArtifactRef(artifact_id=binary,sha256=digest),canvas=image.canvas,
                transform_chain=image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
            body = canonical_bytes(mask)
            root_sha = sha256(body).hexdigest()
            root = publish(root_sha,task=producer,body=body)
            db.execute("UPDATE photo.artifacts SET output_role='mask_protected' WHERE id=%s",(binary,))
            db.execute("UPDATE photo.artifacts SET output_role='mask_protected_ref' WHERE id=%s",(root,))
            roots[name] = dict(artifact_id=str(root),sha256=root_sha)
            outputs.extend([roots[name],dict(artifact_id=str(binary),sha256=digest)])
            scope['protection_pixel_inputs'].append((binary,digest,'NpyArray'))
            scope['protection_documents'][name] = dict(body=body,binary=raw,ref=roots[name],binary_ref=dict(artifact_id=str(binary),sha256=digest))
        result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
            (tenant,producer,Jsonb(outputs),'e'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
        scope['protection_producer'] = producer
        return roots
    return build


def transfer_params_builder(scope, publish, *, kind="background_cleanup"):
    """Build an explicit own intent kind before transfer producer rows."""
    if kind not in ("background_cleanup","retouch_region","remove_object","local_exposure"):
        raise ValueError("Unsupported review fixture kind")
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
    from photo_worker.contracts.images import WorkingImage, MaskRef
    from photo_worker.contracts.semantic import EditIntent, SemanticTransferParams
    image = WorkingImage.model_validate_json(scope['source_body'])
    binding = dict(asset_version_id=str(scope['version']),
        source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(image.canvas))
    reference=scope.get('reference_binding',binding)
    if not scope.get('reuse_review_intent',False):
        reference_image=WorkingImage.model_validate_json(scope.get('reference_body',scope['source_body']))
        stream = BytesIO()
        weights=np.ones((reference_image.canvas.height,reference_image.canvas.width),dtype=np.float32)
        if kind=='retouch_region':
            # protection_builder permanently protects this pixel in both domains.
            # Keep the complete face ROI available while selecting no protected pixel.
            weights[0,0]=0
            assert weights.sum()>0 and weights[0,0]==0
        np.save(stream,weights,allow_pickle=False)
        binary = stream.getvalue()
        binary_sha = sha256(binary).hexdigest()
        binary_id = publish(binary_sha,media='application/x-npy',body=binary)
        mask = MaskRef(artifact=ArtifactRef(artifact_id=binary_id,sha256=binary_sha),canvas=reference_image.canvas,
            transform_chain=reference_image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=False)
        mask_body = canonical_bytes(mask)
        mask_sha = sha256(mask_body).hexdigest()
        mask_id = publish(mask_sha,body=mask_body)
        scope['selected_ref'] = dict(artifact_id=str(mask_id),sha256=mask_sha)
        scope['selected_pixels'] = dict(artifact_id=str(binary_id),sha256=binary_sha)
        intent = EditIntent.model_validate_json(canonical_bytes(dict(schema_version=1,kind=kind,reference=reference,
            reference_mask=scope['selected_ref'],target_snapshot_id=str(scope['snapshot']),strength=1.,
            ev=.5 if kind=="local_exposure" else None,preserve_persons=True,prompt=None)))
        intent_body = canonical_bytes(intent)
        intent_sha = sha256(intent_body).hexdigest()
        intent_id = publish(intent_sha,body=intent_body)
        scope['intent_ref'] = dict(artifact_id=str(intent_id),sha256=intent_sha)
        scope['intent_body'] = intent_body
        scope['selected_mask_body'] = mask_body
        scope['selected_binary_body'] = binary
        scope['transfer_inputs'] = [(intent_id,intent_sha,'EditIntent'),(mask_id,mask_sha,'MaskRef'),(binary_id,binary_sha,'NpyArray')]
    else:
        intent=EditIntent.model_validate_json(scope['intent_body'])
        if intent.kind!=kind or intent.reference.model_dump(mode='json')!=reference or intent.target_snapshot_id!=scope['snapshot']:
            raise ValueError('shared review intent mismatch')
    params = SemanticTransferParams.model_validate_json(canonical_bytes(dict(schema_version=1,intent=scope['intent_ref'],
        reference=reference,target=binding,model_lock_sha=scope['qualification']['model_lock_sha'],calibration=scope['qualification']['calibration'])))
    return canonical_bytes(params)



def candidate_builder(db, *, bind_transfer_model=False):
    def build(scope,publish,producer):
        from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
        from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
        from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate
        params = SemanticTransferParams.model_validate_json(scope['transfer_params_body'])
        image = WorkingImage.model_validate_json(scope['source_body'])
        raw = scope['selected_binary_body']
        digest = sha256(raw).hexdigest()
        binary = publish(digest,task=producer,media='application/x-npy',body=raw)
        mask = MaskRef(artifact=ArtifactRef(artifact_id=binary,sha256=digest),canvas=image.canvas,
            transform_chain=image.transform_chain,selector='manual',confidence=1.,
            # Match stage_planar_transfer provenance while retaining manual mask semantics.
            model_lock_id=params.model_lock_sha if bind_transfer_model else None,protected=False)
        mask_body = canonical_bytes(mask)
        mask_sha = sha256(mask_body).hexdigest()
        mask_id = publish(mask_sha,task=producer,body=mask_body)
        canvas_sha = canonical_hash(image.canvas)
        reference_image = WorkingImage.model_validate_json(scope.get('reference_body',scope['source_body']))
        transform = ResolvedTransform(schema_version=1,from_canvas_hash=canonical_hash(reference_image.canvas),to_canvas_hash=canvas_sha,
            matrix=[1.,0.,0.,0.,1.,0.,0.,0.,1.],kind='homography')
        transform_body = canonical_bytes(transform)
        transform_sha = sha256(transform_body).hexdigest()
        transform_id = publish(transform_sha,task=producer,body=transform_body)
        for aid,role in ((binary,'mask_pixels'),(mask_id,'mask'),(transform_id,'transform')):
            db.execute('UPDATE photo.artifacts SET output_role=%s WHERE id=%s',(role,aid))
        scope['candidate_mask_body'] = mask_body
        scope['candidate_transform_body'] = transform_body
        scope['candidate_mask_ref'] = dict(artifact_id=str(mask_id),sha256=mask_sha)
        scope['candidate_transform_ref'] = dict(artifact_id=str(transform_id),sha256=transform_sha)
        candidate = TransferCandidate(schema_version=1,target=params.target,
            mask=ArtifactRef(artifact_id=mask_id,sha256=mask_sha),transform=ArtifactRef(artifact_id=transform_id,sha256=transform_sha),
            confidence=1.,state='proposed',reason=None)
        return canonical_bytes(candidate)
    return build


def review_calibration_body():
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.session.contracts import Calibration
    return canonical_bytes(Calibration(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.7,scene_log_luma_max=1.,cluster_diameter_max=.3,sharpness_scale=1.,
        clipping_penalty=1.,match_inlier_min=.8,match_error_px_max=3.,identity_similarity_min=.7,
        landmark_drift_max=.1,normalization_scales=[1.]*6,loss_weights=[1/6]*6,
        focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.2,
        clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=32,
        depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))


def review_transfer_params(scope):
    """Ordered actual transfer proposals; a plural list never falls back to one."""
    from photo_worker.contracts.semantic import SemanticTransferParams
    bodies=scope.get('transfer_params_bodies')
    if bodies is None:bodies=[scope['transfer_params_body']]
    if not isinstance(bodies,list) or not 1<=len(bodies)<=100:
        raise ValueError('review transfer list requires 1..100 items')
    params=[SemanticTransferParams.model_validate_json(body) for body in bodies]
    first=params[0]
    if len({item.target.asset_version_id for item in params})!=len(params):
        raise ValueError('duplicate review target version')
    if any((item.intent,item.model_lock_sha,item.calibration)!=(first.intent,first.model_lock_sha,first.calibration) for item in params):
        raise ValueError('mixed review transfer authority')
    return params


def review_snapshot_builder(scope,publish):
    from photo_worker.contracts.base import canonical_bytes,canonical_hash
    from photo_worker.operations.session.contracts import InputSnapshot,SnapshotItem
    params=review_transfer_params(scope)
    snapshot=InputSnapshot(schema_version=1,snapshot_id=scope['snapshot'],snapshot_digest='0'*64,
        items=[SnapshotItem(ordinal=index,binding=item.target,working_image=item.target.source,
            captured_at=None,camera_hash=None,recipe=item.intent) for index,item in enumerate(params)])
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    return canonical_bytes(snapshot)


def review_plan_builder(scope,publish,producer):
    from photo_worker.contracts.semantic import SemanticPlan,TransferCandidate
    from photo_worker.operations.session.contracts import InputSnapshot
    from photo_worker.contracts.base import canonical_bytes
    params=review_transfer_params(scope)
    bodies=scope.get('candidate_bodies')
    if bodies is None:bodies=[scope['candidate_body']]
    if not isinstance(bodies,list) or len(bodies)!=len(params):
        raise ValueError('incomplete review candidates')
    candidates=[TransferCandidate.model_validate_json(body) for body in bodies]
    snapshot=InputSnapshot.model_validate_json(scope['snapshot_body'])
    if len(snapshot.items)!=len(params) or any(
        candidate.target!=proposal.target or item.binding!=proposal.target or item.ordinal!=index
        or item.recipe!=proposal.intent or item.working_image!=proposal.target.source
        for index,(candidate,proposal,item) in enumerate(zip(candidates,params,snapshot.items))):
        raise ValueError('review snapshot/candidate target mismatch')
    first=params[0]
    return canonical_bytes(SemanticPlan(schema_version=1,intent_sha=first.intent.sha256,
        snapshot_digest=snapshot.snapshot_digest,candidates=candidates,
        model_lock_sha=first.model_lock_sha,calibration=first.calibration))
