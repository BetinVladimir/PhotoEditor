from contextlib import ExitStack
from dataclasses import asdict
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.defects import DefectProposeParams,DefectProposeReport,DefectProposalFaceReport,DefectCandidates,DefectCandidate,candidate_id
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.images import WorkingImage,Face68,MaskRef
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from photo_worker.operations.portrait.prepare import FaceInput
from .propose_prepare import prepare_proposals
from .protection import verify_proposal_protection


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(DefectProposeParams);image=artifacts.contract(params.image,WorkingImage)
        h,w=image.canvas.height,image.canvas.width
        if h*w>200000000 or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('Invalid proposal image')
        def validate_mask(mask,object_id=None):
            if mask is None:return
            if mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:raise ValueError('Proposal mask geometry mismatch')
            if object_id is not None and mask.object_id is not None and mask.object_id!=object_id:raise ValueError('Proposal mask object mismatch')
        validate_mask(params.permanent)
        for protection in params.protections:
            validate_mask(protection)
            artifacts.require_declared_input(protection.artifact)
        faces=[]
        for binding in params.faces:
            if binding.object_id is None:
                face=artifacts.contract(binding.landmarks,Face68)
            else:
                collection=artifacts.contract(binding.landmarks,FaceLandmarks)
                if collection.image!=params.image:raise ValueError('Proposal landmarks image mismatch')
                matches=[candidate for candidate in collection.landmarks if candidate.object_id==binding.object_id]
                if len(matches)!=1:raise ValueError('Proposal face object missing or ambiguous')
                face=matches[0]
            if face.canvas!=image.canvas or face.transform_chain!=image.transform_chain:raise ValueError('Proposal face geometry mismatch')
            validate_mask(binding.skin,face.object_id);validate_mask(binding.manual,face.object_id)
            confidence=min([p.confidence for p in face.points]+[m.confidence for m in (binding.skin,binding.manual) if m is not None])
            face=face.model_copy(update={'points':[p.model_copy(update={'confidence':min(p.confidence,confidence)}) for p in face.points]})
            faces.append((binding,face))
        if len({f.object_id for _,f in faces})!=len(faces):raise ValueError('Duplicate face object')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,3)
    try:
        with ExitStack() as inputs:
            def mapped(ref,shape):
                path=inputs.enter_context(artifacts.input(ref,suffix='.npy')).path
                return inputs.enter_context(read_array(path,shape,'float32',ctx.check_active,unit_interval=True))
            rgb=mapped(image.pixels,(h,w,3))
            permanent=None if params.permanent is None else mapped(params.permanent.artifact,(h,w))
            protections=tuple(mapped(mask.artifact,(h,w)) for mask in params.protections)
            prepared=[FaceInput(face,None if b.skin is None else mapped(b.skin.artifact,(h,w)),None if b.manual is None else mapped(b.manual.artifact,(h,w)),b.manual_mode) for b,face in faces]
            with prepare_proposals(rgb,image,prepared,artifacts.scratch,permanent=permanent,protections=protections,
                diameter_fraction=tuple(params.diameter_face_fraction),max_roi_pixels=min(4194304,ram//256),check_active=ctx.check_active) as files:
                with read_array(files.protection,(h,w),'float32',ctx.check_active,unit_interval=True) as output_protection, read_array(files.labels,(h,w),'uint16',ctx.check_active) as output_labels:
                    verify_proposal_protection(image,prepared,output_protection,output_labels,
                        permanent=permanent,protections=protections,max_roi_pixels=min(4194304,ram//256),check_active=ctx.check_active)
                labels=artifacts.stage(files.labels,files.reservation,'defect_labels','application/octet-stream')
                protected=artifacts.stage(files.protection,files.reservation,'defect_protection','application/octet-stream')
                png=artifacts.stage(files.png,files.reservation,'defect_labels_png','image/png')
                written=sum(p.stat().st_size for p in (files.labels,files.protection,files.png))
                candidates=[DefectCandidate(id=candidate_id(image.pixels,labels,c.label,c.bbox,c.diameter),label=c.label,bbox=c.bbox,diameter=c.diameter,score=c.score) for c in files.candidates]
                evidence=[DefectProposalFaceReport(**asdict(f)) for f in files.faces]
    except ValueError as exc:
        code='RESOURCE_OOM' if str(exc)=='BEAUTY_ROI_BUDGET_EXCEEDED' else 'INPUT_INVALID'
        raise OperationFailure(code) from exc
    ctx.progress(1,3)
    proposal=DefectCandidates(image=params.image,source_pixels=image.pixels,label_map=labels,canvas=image.canvas,transform_chain=image.transform_chain,candidates=candidates)
    proposal_ref=artifacts.stage_contract(proposal,'defect_candidates');ctx.progress(2,3)
    warnings=[]
    if not faces:warnings.append('no_faces')
    if any(f.reason is not None for f in evidence):warnings.append('face_skipped')
    if any(f.overlapping_candidates for f in evidence):warnings.append('overlapping_candidates')
    protection=MaskRef(artifact=protected,canvas=image.canvas,transform_chain=image.transform_chain,selector='manual',
        confidence=min((p.confidence for _,f in faces for p in f.points),default=1.),model_lock_id=None,protected=True)
    report=DefectProposeReport(params=params,proposal=proposal_ref,protection=protection,label_png=png,faces=evidence,warnings=warnings)
    report_ref=artifacts.stage_contract(report,'defect_propose_report');ctx.progress(3,3)
    return OperationResult(outputs=[labels,protected,png,proposal_ref,report_ref],review_required=bool(candidates or warnings),
        bytes_written=written+len(canonical_bytes(proposal))+len(canonical_bytes(report)))


def defect_propose_operations(config:ArtifactConfig)->list[Operation]:
    def proposed(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('defects.propose','cpu',proposed)]
