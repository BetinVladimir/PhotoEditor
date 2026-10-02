from contextlib import ExitStack
from dataclasses import asdict
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.defects import DefectRepairParams,DefectCandidates,DefectReview,DefectRepairReport,DefectRepairEvidence,accepted_candidates
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .prepare import prepare_repair


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(DefectRepairParams)
        image=artifacts.contract(params.image,WorkingImage)
        proposal=artifacts.contract(params.proposal,DefectCandidates)
        review=artifacts.contract(params.review,DefectReview)
        selected=accepted_candidates(params.proposal,proposal,review,params.image,image)
        resolved=artifacts.contract(params.resolved,ResolvedStyle)
        if resolved.mode=='skip':raise OperationFailure('STYLE_OOD_SKIP')
        source=image if resolved.params.image==params.image else artifacts.contract(resolved.params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256:
            raise ValueError('Repair source mismatch')
        if params.protected.canvas!=image.canvas or params.protected.transform_chain!=image.transform_chain:
            raise ValueError('Repair protection geometry mismatch')
        if image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('Undeclared alpha')
        effective=resolve_effective_params(resolved).modules.defects
    except ValueError as exc:
        code=str(exc) if str(exc) in ('STALE_DEFECT_REVIEW','STALE_DEFECT_SOURCE','UNKNOWN_DEFECT_CANDIDATE') else 'INPUT_INVALID'
        raise OperationFailure(code) from exc
    ctx.progress(0,3);outputs=[];written=0;evidence=[];warnings=[];output_image=image
    if effective.enabled:
        if not selected:warnings.append('no_accepted_candidates')
        with ExitStack() as inputs:
            h,w=image.canvas.height,image.canvas.width
            def mapped(ref,shape,dtype,unit):
                path=inputs.enter_context(artifacts.input(ref,suffix='.npy')).path
                return inputs.enter_context(read_array(path,shape,dtype,ctx.check_active,unit_interval=unit))
            rgb=mapped(image.pixels,(h,w,3),'float32',True)
            labels=mapped(proposal.label_map,(h,w),'uint16',False)
            protected=mapped(params.protected.artifact,(h,w),'float32',True)
            try:
                with prepare_repair(rgb,labels,protected,proposal,selected,artifacts.scratch,strength=effective.strength,
                                    max_roi_pixels=min(4194304,ram//256),check_active=ctx.check_active) as files:
                    pixels=artifacts.stage(files.image,files.reservation,'working_rgb','application/octet-stream')
                    written=files.image.stat().st_size;outputs.append(pixels)
                    output_image=image.model_copy(update={'pixels':pixels})
                    evidence=[DefectRepairEvidence(**asdict(e)) for e in files.candidates]
            except ValueError as exc:
                code='RESOURCE_OOM' if str(exc)=='REPAIR_ROI_BUDGET_EXCEEDED' else 'INPUT_INVALID'
                raise OperationFailure(code) from exc
        if any(e.reason not in (None,'zero_strength') for e in evidence):warnings.append('candidate_skipped')
    else:warnings.append('module_disabled')
    ctx.progress(1,3)
    image_ref=artifacts.stage_contract(output_image,'working_image');outputs.append(image_ref);ctx.progress(2,3)
    requested=resolved.modules.defects
    report=DefectRepairReport(params=params,review_revision=review.revision,enabled=effective.enabled,
        requested_strength=requested.strength if requested.enabled else 0.,effective_strength=effective.strength if effective.enabled else 0.,
        candidates=evidence,warnings=warnings)
    report_ref=artifacts.stage_contract(report,'defect_repair_report');outputs.append(report_ref);ctx.progress(3,3)
    return OperationResult(outputs=outputs,review_required=resolved.needs_review or 'candidate_skipped' in warnings,
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def defect_repair_operations(config:ArtifactConfig)->list[Operation]:
    def repaired(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','defect_repair_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('defects.repair','cpu',repaired)]
