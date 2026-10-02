from contextlib import ExitStack
from dataclasses import asdict
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.algorithms.beauty_roi import plan_roi
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.beauty import BeautyParams,BeautyReport,BeautyFaceReport
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.images import WorkingImage,Face68
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .prepare import FaceInput,prepare_beauty


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None;tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824 or type(tile) is not int or tile not in (256,512,1024):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(BeautyParams);image=artifacts.contract(params.image,WorkingImage)
        resolved=artifacts.contract(params.resolved,ResolvedStyle)
        if resolved.mode=='skip':raise OperationFailure('STYLE_OOD_SKIP')
        source=image if resolved.params.image==params.image else artifacts.contract(resolved.params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256:
            raise ValueError('Beauty source mismatch')
        h,w=image.canvas.height,image.canvas.width
        if h*w>200000000:raise ValueError('Beauty pixel limit')
        if image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('Undeclared alpha')
        effective=resolve_effective_params(resolved).modules.beauty
        faces=[]
        def validate_mask(mask,object_id=None):
            if mask is None:return
            if mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:raise ValueError('Beauty mask geometry mismatch')
            if object_id is not None and mask.object_id is not None and mask.object_id!=object_id:raise ValueError('Beauty mask object mismatch')
        validate_mask(params.permanent)
        if effective.enabled:
            for binding in params.faces:
                if binding.object_id is None:
                    face=artifacts.contract(binding.landmarks,Face68)
                else:
                    collection=artifacts.contract(binding.landmarks,FaceLandmarks)
                    if collection.image!=params.image:raise ValueError('Beauty landmarks image mismatch')
                    matches=[candidate for candidate in collection.landmarks if candidate.object_id==binding.object_id]
                    if len(matches)!=1:raise ValueError('Beauty face object missing or ambiguous')
                    face=matches[0]
                if face.canvas!=image.canvas or face.transform_chain!=image.transform_chain:raise ValueError('Beauty face geometry mismatch')
                validate_mask(binding.skin,face.object_id);validate_mask(binding.manual,face.object_id)
                confidence=min([p.confidence for p in face.points]+[m.confidence for m in (binding.skin,binding.manual) if m is not None])
                face=face.model_copy(update={'points':[p.model_copy(update={'confidence':min(p.confidence,confidence)}) for p in face.points]})
                faces.append((binding,face))
            if len({f.object_id for _,f in faces})!=len(faces):raise ValueError('Duplicate face object')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    roi_limit=min(4194304,ram//256)
    if effective.enabled:
        for _,face in faces:
            try:plan_roi(face,max_work_pixels=roi_limit)
            except ValueError as exc:
                if str(exc)=='BEAUTY_ROI_BUDGET_EXCEEDED':raise OperationFailure('RESOURCE_OOM',False) from exc
                raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,3);outputs=[];written=0;reports=[];warnings=[]
    output_image=image
    if effective.enabled:
        with ExitStack() as inputs:
            def mapped(ref,shape):
                local=inputs.enter_context(artifacts.input(ref,suffix='.npy'))
                return inputs.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=True))
            rgb=mapped(image.pixels,(h,w,3))
            permanent=None if params.permanent is None else mapped(params.permanent.artifact,(h,w))
            prepared=[FaceInput(face,None if b.skin is None else mapped(b.skin.artifact,(h,w)),
                None if b.manual is None else mapped(b.manual.artifact,(h,w)),b.manual_mode) for b,face in faces]
            with prepare_beauty(rgb,image,prepared,effective,artifacts.scratch,permanent=permanent,tile=tile,
                                max_roi_pixels=roi_limit,check_active=ctx.check_active) as files:
                pixel_ref=artifacts.stage(files.image,files.reservation,'working_rgb','application/octet-stream')
                written=files.image.stat().st_size;outputs.append(pixel_ref)
                reports=[BeautyFaceReport(**asdict(e)) for e in files.faces]
                output_image=image.model_copy(update={'pixels':pixel_ref})
        if not faces:warnings.append('no_faces')
        if any(not r.applied and r.reason!='zero_strength' for r in reports):warnings.append('face_skipped')
    else:warnings.append('module_disabled')
    ctx.progress(1,3)
    image_ref=artifacts.stage_contract(output_image,'working_image');outputs.append(image_ref);ctx.progress(2,3)
    report=BeautyReport(image=params.image,resolved=params.resolved,requested=resolved.modules.beauty,effective=effective,faces=reports,warnings=warnings)
    report_ref=artifacts.stage_contract(report,'beauty_report');outputs.append(report_ref);ctx.progress(3,3)
    return OperationResult(outputs=outputs,review_required=resolved.needs_review or any(w!='module_disabled' for w in warnings),
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def beauty_operations(config:ArtifactConfig)->list[Operation]:
    def retouched(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','beauty_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('portrait.beauty','cpu',retouched)]
