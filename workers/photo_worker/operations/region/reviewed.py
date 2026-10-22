"""Reviewed repairs replay only exact live SQL-bound human selections."""
from contextlib import contextmanager,ExitStack
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.contracts.defects import DefectRepairParams,DefectCandidates,DefectReview,accepted_candidates
from photo_worker.contracts.dust import DustRepairParams,DustCandidates,accepted_dust_candidates
from photo_worker.runtime.arrays import read_array


def reviewed_base(effective,verified):
    entries=[entry for entry in verified if getattr(entry[0],'review_authority',None) is not None and entry[0].review_authority.get('route')=='base']
    requested=effective.defects
    if not requested.enabled or requested.policy!='review':
        if entries:raise ValueError('REGION_REVIEWED_BASE_UNEXPECTED')
        return None
    repairs=[entry for entry in verified if entry[0].operation=='defects.repair']
    if repairs:
        if entries:raise ValueError('REGION_REVIEWED_BASE_CONFLICT')
        return None
    if len(entries)!=1:raise ValueError('REGION_REVIEWED_BASE_AUTHORITY_REQUIRED')
    stage,_,_,image,_=entries[0];authority=stage.review_authority
    if set(authority)!={'route','review_id','decision_hash','selection','producer_task_id'}:raise ValueError('REGION_REVIEWED_BASE_AUTHORITY_INVALID')
    from uuid import UUID
    import re
    if (UUID(authority['review_id']).int==0 or UUID(authority['producer_task_id']).int==0
            or not isinstance(authority['decision_hash'],str) or re.fullmatch('[0-9a-f]{64}',authority['decision_hash']) is None):
        raise ValueError('REGION_REVIEWED_BASE_AUTHORITY_INVALID')
    selection=DefectReview.model_validate_json(canonical_bytes(authority['selection']))
    if selection.image!=stage.output:raise ValueError('REGION_REVIEWED_BASE_SOURCE_INVALID')
    return image


def reviewed_entry(effective,verified,operation):
    defects=operation=='defects.repair'
    requested=effective.defects if defects else effective.extensions.restoration
    enabled=requested.enabled if defects else requested is not None and requested.sensor_dust!='off'
    policy=requested.policy if defects and enabled else requested.sensor_dust if enabled else None
    entries=[entry for entry in verified if entry[0].operation==operation]
    if policy!='review':
        if entries:raise ValueError('REGION_REVIEWED_STAGE_UNEXPECTED')
        return None
    if defects and not entries:
        reviewed_base(effective,verified);return None
    if len(entries)!=1:raise ValueError('REGION_REVIEWED_STAGE_COVERAGE_MISMATCH')
    stage,raw,source,target,evidence=entries[0]
    params=(DefectRepairParams if defects else DustRepairParams).model_validate_json(canonical_bytes(raw))
    authority=stage.review_authority
    allowed={'review_id','decision_hash','selection','selection_ref'} if defects else {'review_id','decision_hash','selection'}
    if (not isinstance(authority,dict) or set(authority)-allowed or 'selection' not in authority
            or ('review_id' in authority)!=('decision_hash' in authority)
            or source.canvas!=target.canvas or source.transform_chain!=target.transform_chain or source.alpha!=target.alpha):
        raise ValueError('REGION_REVIEWED_AUTHORITY_INVALID')
    if 'review_id' in authority:
        from uuid import UUID
        import re
        if UUID(authority['review_id']).int==0 or not isinstance(authority['decision_hash'],str) or re.fullmatch('[0-9a-f]{64}',authority['decision_hash']) is None:
            raise ValueError('REGION_REVIEWED_AUTHORITY_INVALID')
    if defects:
        resolved=evidence.get('resolved')
        if (set(authority)!=allowed or ArtifactRef.model_validate_json(canonical_bytes(authority['selection_ref']))!=params.review
                or resolved is None or resolved.strength!=effective.strength or resolved.modules.defects!=effective.defects
                or params.protected.canvas!=source.canvas or params.protected.transform_chain!=source.transform_chain):
            raise ValueError('REGION_REVIEWED_POLICY_MISMATCH')
    elif params.review is None or canonical_bytes(params.review)!=canonical_bytes(authority['selection']):
        raise ValueError('REGION_REVIEWED_POLICY_MISMATCH')
    elif 'review_id' not in authority and (params.review.accepted_candidate_ids or params.review.rejected_candidate_ids):
        raise ValueError('REGION_REVIEWED_EMPTY_CONTINUATION_REQUIRED')
    return stage,params,source,evidence


def reviewed_documents(effective,verified,document):
    def owned(ref,model):
        raw=document(ref);value=model.model_validate_json(raw)
        if canonical_bytes(value)!=raw or canonical_hash(value)!=ref.sha256:raise ValueError('REGION_REVIEWED_DOCUMENT_INVALID')
        return value
    result=[]
    base=reviewed_base(effective,verified)
    if base is not None:
        stage=next(entry[0] for entry in verified if (getattr(entry[0],'review_authority',None) or {}).get('route')=='base')
        review=DefectReview.model_validate_json(canonical_bytes(stage.review_authority['selection']))
        proposal=owned(review.proposal,DefectCandidates)
        accepted_candidates(review.proposal,proposal,review,stage.output,base)
        result.append((base,[],proposal.label_map))
    for operation in ('sensor.dust_repair','defects.repair'):
        entry=reviewed_entry(effective,verified,operation)
        if entry is None:continue
        stage,params,image,_=entry
        if operation=='defects.repair':
            proposal=owned(params.proposal,DefectCandidates);review=owned(params.review,DefectReview)
            if canonical_bytes(review)!=canonical_bytes(stage.review_authority['selection']):raise ValueError('REGION_REVIEWED_SELECTION_CHANGED')
            accepted_candidates(params.proposal,proposal,review,params.image,image)
            result.append((image,[params.protected],proposal.label_map))
        else:
            proposal=None if params.proposal is None else owned(params.proposal,DustCandidates)
            accepted_dust_candidates(params,proposal)
            if image.alpha is not None or image.canvas.orientation!=1:raise ValueError('REGION_REVIEWED_DUST_SOURCE_INVALID')
            result.append((image,[],None))
    return result


@contextmanager
def prepare_region_reviewed(artifacts,effective,verified,operation,rgb,alpha,*,profile,verify_runtime,check_active,private_sequence=None):
    entry=reviewed_entry(effective,verified,operation)
    base=reviewed_base(effective,verified) if operation=='defects.repair' and entry is None else None
    if entry is None and base is None:yield rgb;return
    if entry is None:stage=params=policy=None;image=base
    else:stage,params,image,policy=entry
    h,w=image.canvas.height,image.canvas.width
    ram=profile.get('resource_estimate',{}).get('ram_bytes')
    if type(ram) is not int or ram<1 or entry is not None and ram<max(1<<30,164*h*w+256*1048576):raise ValueError('REGION_REVIEWED_RESOURCE_INVALID')
    def document(ref):
        artifacts.require_declared_input(ref)
        with artifacts.input(ref,suffix='.json') as local:
            if local.path.stat().st_size>1048576:raise ValueError('REGION_REVIEWED_DOCUMENT_TOO_LARGE')
            check_active();raw=local.path.read_bytes();check_active();return raw
    reviewed_documents(effective,verified,document)
    verify_runtime();check_active()
    with ExitStack() as scopes:
        def mapped(ref,shape,dtype='float32'):
            artifacts.require_declared_input(ref);local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
            return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=dtype=='float32'))
        source=mapped(image.pixels,(h,w,3))
        from .private_sources import require_stage_source
        original_alpha=None if image.alpha is None else mapped(image.alpha,(h,w),'uint16')
        if private_sequence is not None:
            from .private_frames import RegionPrivateSequence
            if type(private_sequence) is not RegionPrivateSequence:raise ValueError('REGION_PRIVATE_STAGE_INPUT_INVALID')
            if entry is None:
                base_stage=next(value[0] for value in verified if (getattr(value[0],'review_authority',None) or {}).get('route')=='base')
                private_sequence.require_base(base_stage.output,rgb,alpha)
            else:
                token=private_sequence.input(operation,rgb,alpha)
                require_stage_source(operation,params.image,rgb,alpha,source,original_alpha,
                    private_input=token,check_active=check_active)
        else:
            require_stage_source(operation,None if params is None else params.image,rgb,alpha,source,original_alpha,
                check_active=check_active)
        if entry is None:
            check_active();verify_runtime();yield rgb;check_active();verify_runtime();return
        if operation=='defects.repair':
            from photo_worker.algorithms.effective import resolve_effective_params
            from photo_worker.operations.defects.prepare import prepare_repair
            proposal=artifacts.contract(params.proposal,DefectCandidates);review=artifacts.contract(params.review,DefectReview)
            selected=accepted_candidates(params.proposal,proposal,review,params.image,image)
            strength=resolve_effective_params(policy['resolved']).modules.defects.strength
            files=scopes.enter_context(prepare_repair(rgb,mapped(proposal.label_map,(h,w),'uint16'),mapped(params.protected.artifact,(h,w)),proposal,selected,artifacts.scratch,strength=strength,max_roi_pixels=min(4194304,ram//256),check_active=check_active))
            path=files.image
        else:
            from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair
            proposal=None if params.proposal is None else artifacts.contract(params.proposal,DustCandidates)
            selected=accepted_dust_candidates(params,proposal)
            if not selected:yield rgb;check_active();verify_runtime();return
            files=scopes.enter_context(prepare_dust_repair(rgb,proposal.raw_crop,selected,artifacts.scratch,check_active=check_active));path=files.path
        output=scopes.enter_context(read_array(path,(h,w,3),'float32',check_active,unit_interval=True))
        verify_runtime();check_active();yield output;check_active();verify_runtime()
