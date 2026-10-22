"""Shared pinned automatic repair engine for task execution and exact replay."""
from contextlib import ExitStack,contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.conservative import DefectAutoRepairParams,DustAutoRepairParams,ConservativeRepairReport
from photo_worker.contracts.defects import DefectCandidates,DefectProposeReport
from photo_worker.contracts.dust import DustCandidates
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.algorithms.conservative import select_defects,select_dust
from photo_worker.operations.defects.prepare import prepare_repair
from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair,dust_repair_ram_bytes
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationResult,OperationFailure

@dataclass(frozen=True)
class ConservativeStage:
    image: WorkingImage
    path: object
    reservation: object
    report: ConservativeRepairReport

@contextmanager
def prepare_conservative_repair(artifacts,params,*,execution_profile,check_active=lambda:None,current=None,verify_runtime=None):
    """Replay passes current RGB + verifier; task path uses lease-qualified CPU.

    This is the sole engine for both callers: no synthetic DefectReview/DustReview.
    Source, proposal, resolved policy and protections are read through declared pins.
    """
    model=DefectAutoRepairParams if isinstance(params,DefectAutoRepairParams) else DustAutoRepairParams
    params=model.model_validate_json(canonical_bytes(params))
    if current is not None:
        if not callable(verify_runtime):raise ValueError('CONSERVATIVE_RUNTIME_REQUIRED')
        check_active();verify_runtime();check_active()
    estimate=execution_profile.get('resource_estimate',{});ram=estimate.get('ram_bytes')
    if execution_profile.get('backend') not in (('cpu','mps','cuda') if current is not None else ('cpu',)) or type(ram) is not int or ram<1073741824:raise ValueError('CONSERVATIVE_RESOURCE_INVALID')
    def owned(ref,kind):
        check_active();return kind.model_validate_json(canonical_bytes(artifacts.contract(ref,kind)))
    image=owned(params.image,WorkingImage);resolved=owned(params.resolved,ResolvedStyle)
    if resolved.mode=='skip':raise OperationFailure('STYLE_OOD_SKIP')
    source=image if resolved.params.image==params.image else owned(resolved.params.image,WorkingImage)
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256:raise ValueError('CONSERVATIVE_SOURCE_INVALID')
    h,w=image.canvas.height,image.canvas.width
    if isinstance(params,DefectAutoRepairParams):
        effective=resolve_effective_params(resolved).modules.defects
        if not effective.enabled or effective.policy!='conservative_auto' or not effective.preserve_permanent_marks:raise ValueError('CONSERVATIVE_POLICY_INVALID')
        proposal=owned(params.proposal,DefectCandidates);report=owned(params.proposal_report,DefectProposeReport)
        if (proposal.image!=params.image or proposal.source_pixels!=image.pixels or proposal.canvas!=image.canvas or proposal.transform_chain!=image.transform_chain or report.proposal!=params.proposal or report.params.image!=params.image or report.algorithm!='defect_dog_v1' or any(face.object_id is None for face in report.params.faces)):raise ValueError('CONSERVATIVE_PROPOSAL_INVALID')
        masks=[report.protection]
    else:
        restoration=resolved.modules.extensions.restoration
        if restoration is None or restoration.sensor_dust!='conservative_auto':raise ValueError('CONSERVATIVE_POLICY_INVALID')
        if image.alpha is not None or image.canvas.orientation!=1:raise ValueError('CONSERVATIVE_SOURCE_INVALID')
        proposal=None if params.proposal is None else owned(params.proposal,DustCandidates);masks=params.protected
        if ram<max(1073741824,dust_repair_ram_bytes(w,h)):raise ValueError('CONSERVATIVE_RESOURCE_INVALID')
    if ram<max(1073741824,128*w*h+268435456):raise ValueError('CONSERVATIVE_RESOURCE_INVALID')
    with ExitStack() as stack:
        def mapped(ref,shape,dtype='float32'):
            path=stack.enter_context(artifacts.input(ref,suffix='.npy')).path
            return stack.enter_context(read_array(path,shape,dtype,check_active,unit_interval=dtype=='float32'))
        rgb=mapped(image.pixels,(h,w,3))
        if current is not None:
            if current.rgb.shape!=rgb.shape or current.rgb.dtype!=np.float32:raise ValueError('CONSERVATIVE_CURRENT_SOURCE_INVALID')
            for y in range(0,h,256):
                check_active()
                if not np.array_equal(current.rgb[y:y+256].view(np.uint32),rgb[y:y+256].view(np.uint32)):raise ValueError('CONSERVATIVE_CURRENT_SOURCE_INVALID')
            if image.alpha is None:
                if current.alpha is not None:raise ValueError('CONSERVATIVE_CURRENT_SOURCE_INVALID')
            else:
                from photo_worker.v2.render_defects import alpha_tile_matches
                from photo_worker.runtime.arrays import read_alpha_array
                local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
                alpha=stack.enter_context(read_alpha_array(local.path,(h,w),check_active)
                    if execution_profile.get('alpha_mode','require_float32')=='normalize_uint16'
                    else read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
                if (not isinstance(current.alpha,np.ndarray) or current.alpha.shape!=(h,w)
                        or current.alpha.dtype!=np.float32):
                    raise ValueError('CONSERVATIVE_CURRENT_SOURCE_INVALID')
                for y in range(0,h,256):
                    for x in range(0,w,256):
                        check_active()
                        if not alpha_tile_matches(current.alpha[y:y+256,x:x+256],alpha[y:y+256,x:x+256]):
                            raise ValueError('CONSERVATIVE_CURRENT_SOURCE_INVALID')
        protected=[]
        for mask in masks:
            if not mask.protected or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:raise ValueError('CONSERVATIVE_PROTECTION_INVALID')
            artifacts.require_declared_input(mask.artifact)
            protected.append(mapped(mask.artifact,(h,w)))
        if isinstance(params,DefectAutoRepairParams):
            labels=mapped(proposal.label_map,(h,w),'uint16')
            selected=select_defects(proposal,rgb,labels,protected[0],check_active=check_active)
            files=stack.enter_context(prepare_repair(rgb,labels,protected[0],proposal,selected,artifacts.scratch,strength=effective.strength,max_roi_pixels=min(4194304,ram//256),check_active=check_active))
            applied=[c.candidate_id for c in files.candidates if c.applied_pixels>0]
            path,reservation=files.image,files.reservation;operation='defects.auto_repair'
        else:
            selected=() if proposal is None else select_dust(proposal,params.image,image,protected,check_active=check_active)
            if selected:
                files=stack.enter_context(prepare_dust_repair(rgb,proposal.raw_crop,selected,artifacts.scratch,check_active=check_active))
                path,reservation=files.path,files.reservation;applied=[c.candidate_id for c in files.candidates if c.applied_pixels>0]
            else:path=reservation=None;applied=[]
            operation='sensor.dust_auto_repair'
        selected_ids=sorted(c.id for c in selected)
        output=ConservativeRepairReport(policy=params.policy,operation=operation,image=params.image,proposal=params.proposal,resolved=params.resolved,selected_candidate_ids=selected_ids,applied_candidate_ids=sorted(applied),skipped_candidate_ids=sorted(c.id for c in (proposal.candidates if proposal else ()) if c.id not in applied))
        # Re-resolve immutable metadata after array IO before exposing owned output.
        if canonical_bytes(owned(params.resolved,ResolvedStyle))!=canonical_bytes(resolved):raise ValueError('CONSERVATIVE_DOCUMENT_CHANGED')
        if canonical_bytes(owned(params.image,WorkingImage))!=canonical_bytes(image):raise ValueError('CONSERVATIVE_DOCUMENT_CHANGED')
        check_active();yield ConservativeStage(image,path,reservation,output);check_active()


def execute_conservative(ctx,artifacts,model):
    try:
        params=artifacts.params(model)
        with prepare_conservative_repair(artifacts,params,execution_profile=ctx.lease.execution_profile,check_active=ctx.check_active) as stage:
            image=stage.image;outputs=[];written=0
            if stage.path is not None:
                pixels=artifacts.stage(stage.path,stage.reservation,'working_rgb','application/octet-stream');outputs.append(pixels);written=stage.path.stat().st_size;image=image.model_copy(update={'pixels':pixels})
            image_ref=artifacts.stage_contract(image,'working_image');outputs.append(image_ref)
            role='defect_repair_report' if model is DefectAutoRepairParams else 'dust_repair_report'
            outputs.append(artifacts.stage_contract(stage.report,role));written+=len(canonical_bytes(image))+len(canonical_bytes(stage.report))
            return OperationResult(outputs=outputs,bytes_written=written)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc


def conservative_operations(config):
    def build(name,model,report):
        def execute(ctx):
            with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image',report})) as artifacts:return execute_conservative(ctx,artifacts,model)
        return Operation(name,'cpu',execute)
    return [build('defects.auto_repair',DefectAutoRepairParams,'defect_repair_report'),build('sensor.dust_auto_repair',DustAutoRepairParams,'dust_repair_report')]
