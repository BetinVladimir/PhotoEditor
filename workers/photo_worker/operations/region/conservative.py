"""Automatic repairs reuse the one pinned task/replay engine at Apply positions."""
from contextlib import contextmanager,ExitStack
from types import SimpleNamespace
from photo_worker.contracts.conservative import DefectAutoRepairParams,DustAutoRepairParams
from photo_worker.operations.conservative import prepare_conservative_repair
from photo_worker.runtime.arrays import read_array
from photo_worker.contracts.base import canonical_bytes,canonical_hash


def conservative_entry(effective,verified,operation):
    defects=operation=='defects.auto_repair'
    requested=effective.defects if defects else effective.extensions.restoration
    enabled=(requested.enabled if defects else requested is not None and requested.sensor_dust!='off')
    policy=(requested.policy if defects and enabled else requested.sensor_dust if not defects and enabled else None)
    entries=[entry for entry in verified if entry[0].operation==operation]
    if not enabled:
        if entries:raise ValueError('REGION_CONSERVATIVE_STAGE_UNEXPECTED')
        return None
    if policy=='review':
        if entries:raise ValueError('REGION_CONSERVATIVE_STAGE_UNEXPECTED')
        return None
    if policy!='conservative_auto':raise ValueError('REGION_REPAIR_POLICY_UNSUPPORTED')
    if len(entries)!=1:raise ValueError('REGION_CONSERVATIVE_STAGE_COVERAGE_MISMATCH')
    _,raw,source,target,evidence=entries[0]
    model=DefectAutoRepairParams if defects else DustAutoRepairParams
    params=model.model_validate_json(canonical_bytes(raw));resolved=evidence.get('resolved')
    if (resolved is None or resolved.strength!=effective.strength
            or defects and resolved.modules.defects!=effective.defects
            or not defects and resolved.modules.extensions.restoration!=effective.extensions.restoration
            or source.canvas!=target.canvas or source.transform_chain!=target.transform_chain
            or source.alpha!=target.alpha):
        raise ValueError('REGION_CONSERVATIVE_EFFECTIVE_POLICY_MISMATCH')
    return params,source


def conservative_documents(effective,verified,document):
    """Independent content bindings; the live SQL scope still owns provenance."""
    from photo_worker.contracts.defects import DefectCandidates,DefectProposeReport
    from photo_worker.contracts.dust import DustCandidates
    result=[]
    def owned(ref,model):
        raw=document(ref);value=model.model_validate_json(raw)
        if canonical_bytes(value)!=raw or canonical_hash(value)!=ref.sha256:
            raise ValueError('REGION_CONSERVATIVE_DOCUMENT_INVALID')
        return value
    for operation in ('sensor.dust_auto_repair','defects.auto_repair'):
        entry=conservative_entry(effective,verified,operation)
        if entry is None:continue
        params,image=entry
        if operation=='defects.auto_repair':
            proposal=owned(params.proposal,DefectCandidates);report=owned(params.proposal_report,DefectProposeReport)
            if (proposal.image!=params.image or proposal.source_pixels!=image.pixels
                    or proposal.canvas!=image.canvas or proposal.transform_chain!=image.transform_chain
                    or report.proposal!=params.proposal or report.params.image!=params.image
                    or report.algorithm!='defect_dog_v1' or any(face.object_id is None for face in report.params.faces)
                    or not report.protection.protected or report.protection.canvas!=image.canvas
                    or report.protection.transform_chain!=image.transform_chain):
                raise ValueError('REGION_CONSERVATIVE_PROPOSAL_INVALID')
            masks=[report.protection];labels=proposal.label_map
        else:
            proposal=None if params.proposal is None else owned(params.proposal,DustCandidates)
            if proposal is not None:
                from photo_worker.contracts.dust import verify_dust_candidate_ids
                verify_dust_candidate_ids(proposal)
                if params.image not in proposal.frames:raise ValueError('REGION_CONSERVATIVE_DUST_SOURCE_INVALID')
            if image.alpha is not None or image.canvas.orientation!=1:raise ValueError('REGION_CONSERVATIVE_DUST_SOURCE_INVALID')
            masks=params.protected;labels=None
        for mask in masks:
            if not mask.protected or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:
                raise ValueError('REGION_CONSERVATIVE_PROTECTION_INVALID')
        result.append((image,masks,labels))
    return result


@contextmanager
def prepare_region_conservative(artifacts,effective,verified,operation,rgb,alpha,*,profile,verify_runtime,check_active,private_input=None,fresh_faces=()):
    entry=conservative_entry(effective,verified,operation)
    if entry is None:
        yield rgb;return
    params,source=entry
    if private_input is not None:
        from .private_frames import PrivateStageInput
        if type(private_input) is not PrivateStageInput:raise ValueError('REGION_PRIVATE_STAGE_INPUT_INVALID')
        private_input.require(operation,params.image,rgb,alpha)
        verify_runtime();check_active()
        with ExitStack() as scopes:
            def mapped(ref,shape,dtype='float32'):
                artifacts.require_declared_input(ref)
                local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
                return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=dtype=='float32'))
            def document(ref):
                artifacts.require_declared_input(ref)
                with artifacts.input(ref,suffix='.json') as local:
                    if local.path.stat().st_size>1048576:raise ValueError('REGION_CONSERVATIVE_DOCUMENT_TOO_LARGE')
                    check_active();raw=local.path.read_bytes();check_active();return raw
            # Re-read the independently admitted original policy/proposal. Its
            # refs are never rewritten to pretend to name these fresh pixels.
            conservative_documents(effective,verified,document)
            h,w=source.canvas.height,source.canvas.width
            ram=profile.get('resource_estimate',{}).get('ram_bytes')
            if type(ram) is not int or ram<max(1<<30,164*h*w+256*1048576):raise ValueError('REGION_CONSERVATIVE_RESOURCE_INVALID')
            if operation=='defects.auto_repair':
                from photo_worker.contracts.defects import DefectProposeReport
                from photo_worker.algorithms.effective import resolve_effective_params
                from .private_repairs import prepare_private_defects
                report=artifacts.contract(params.proposal_report,DefectProposeReport)
                protection=mapped(report.protection.artifact,(h,w))
                from photo_worker.contracts.resolved import ResolvedStyle
                resolved=artifacts.contract(params.resolved,ResolvedStyle)
                strength=resolve_effective_params(resolved).modules.defects.strength
                output,_,_=scopes.enter_context(prepare_private_defects(rgb,alpha,private_input.metadata,fresh_faces,
                    (protection,),artifacts.scratch,ledger=private_input.ledger,owner=private_input.owner,
                    stage=private_input.stage,original_source=params.image,strength=strength,ram_bytes=ram,check_active=check_active))
            else:
                from photo_worker.contracts.dust import DustCandidates
                from photo_worker.algorithms.conservative import select_dust
                from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair,dust_repair_ram_bytes
                if ram<max(1<<30,dust_repair_ram_bytes(w,h)):raise ValueError('REGION_CONSERVATIVE_RESOURCE_INVALID')
                proposal=None if params.proposal is None else artifacts.contract(params.proposal,DustCandidates)
                protected=[mapped(mask.artifact,(h,w)) for mask in params.protected]
                selected=() if proposal is None else select_dust(proposal,params.image,source,protected,check_active=check_active)
                if selected:
                    repair=scopes.enter_context(prepare_dust_repair(rgb,proposal.raw_crop,selected,artifacts.scratch,check_active=check_active))
                    output=scopes.enter_context(read_array(repair.path,(h,w,3),'float32',check_active,unit_interval=True))
                else:output=rgb
            private_input.require(operation,params.image,rgb,alpha)
            verify_runtime();check_active();yield output;check_active();verify_runtime()
        return
    # The shared engine verifies exact RGB/alpha against the original admitted
    # predecessor, not only canvas identity. No copying an adopted after-image.
    with prepare_conservative_repair(artifacts,params,execution_profile=profile,
            current=SimpleNamespace(rgb=rgb,alpha=alpha),verify_runtime=verify_runtime,
            check_active=check_active) as stage:
        if stage.path is None:yield rgb
        else:
            with read_array(stage.path,(source.canvas.height,source.canvas.width,3),'float32',check_active,unit_interval=True) as output:
                yield output
        check_active()
