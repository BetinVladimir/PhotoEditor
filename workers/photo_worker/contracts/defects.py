from typing import Annotated,Literal
from pydantic import Field,model_validator,model_serializer
from .base import Contract,ArtifactRef,SHA256,Unit,canonical_hash
from .images import Canvas,TransformChain,Rect,WorkingImage

Label=Annotated[int,Field(ge=1,le=65535)]


def candidate_id(source_pixels:ArtifactRef,label_map:ArtifactRef,label:int,bbox:Rect,diameter:float)->str:
    return canonical_hash(dict(version='defect_candidate_v1',source_sha256=source_pixels.sha256,
                               mask_sha256=label_map.sha256,label=label,bbox=bbox.model_dump(mode="json"),diameter=diameter))

class DefectCandidate(Contract):
    id:SHA256
    label:Label
    bbox:Rect
    diameter:Annotated[float,Field(gt=0,le=200000000)]
    score:Unit
    decision:Literal['pending']='pending'

class DefectCandidates(Contract):
    version:Literal['defect_candidates_v1']='defect_candidates_v1'
    image:ArtifactRef
    source_pixels:ArtifactRef
    label_map:ArtifactRef
    canvas:Canvas
    transform_chain:TransformChain
    candidates:Annotated[list[DefectCandidate],Field(max_length=4096)]

    @model_validator(mode='after')
    def coherent(self):
        self.transform_chain.validate_target(self.canvas)
        if self.canvas.width*self.canvas.height>200000000:raise ValueError('Candidate canvas pixel limit')
        if len({c.id for c in self.candidates})!=len(self.candidates) or len({c.label for c in self.candidates})!=len(self.candidates):
            raise ValueError('Duplicate candidate identity/label')
        for c in self.candidates:
            c.bbox.validate_canvas(self.canvas)
            if c.id!=candidate_id(self.source_pixels,self.label_map,c.label,c.bbox,c.diameter):
                raise ValueError('Candidate identity hash mismatch')
        return self

class DefectReview(Contract):
    version:Literal['defect_review_v1']='defect_review_v1'
    proposal:ArtifactRef
    image:ArtifactRef
    label_map:ArtifactRef
    revision:Annotated[int,Field(ge=1,le=2**53-1)]
    accepted_candidate_ids:Annotated[list[SHA256],Field(max_length=4096)]
    rejected_candidate_ids:Annotated[list[SHA256],Field(max_length=4096)]

    @model_validator(mode='after')
    def coherent(self):
        accepted,rejected=self.accepted_candidate_ids,self.rejected_candidate_ids
        if len(set(accepted))!=len(accepted) or len(set(rejected))!=len(rejected):raise ValueError('Duplicate review decision')
        if set(accepted)&set(rejected):raise ValueError('Conflicting review decision')
        return self


def accepted_candidates(proposal_ref:ArtifactRef,proposal:DefectCandidates,review:DefectReview,
                        image_ref:ArtifactRef,image:WorkingImage)->tuple[DefectCandidate,...]:
    """Content binding only; trusted backend must authorize the saved review.

    Omitted candidates remain pending, never implicitly approved. Exact artifact
    references prevent applying acceptance to a different image or label version.
    """
    proposal.coherent();review.coherent()
    if review.proposal!=proposal_ref or review.image!=proposal.image or image_ref!=proposal.image or review.label_map!=proposal.label_map:
        raise ValueError('STALE_DEFECT_REVIEW')
    if image.pixels!=proposal.source_pixels or image.canvas!=proposal.canvas or image.transform_chain!=proposal.transform_chain:
        raise ValueError('STALE_DEFECT_SOURCE')
    known={c.id for c in proposal.candidates}
    if not set(review.accepted_candidate_ids+review.rejected_candidate_ids)<=known:
        raise ValueError('UNKNOWN_DEFECT_CANDIDATE')
    accepted=set(review.accepted_candidate_ids)
    return tuple(sorted((c for c in proposal.candidates if c.id in accepted),key=lambda c:c.label))

from .images import MaskRef

class DefectRepairParams(Contract):
    image:ArtifactRef
    proposal:ArtifactRef
    review:ArtifactRef
    resolved:ArtifactRef
    protected:MaskRef

    @model_validator(mode='after')
    def protected_mask(self):
        if not self.protected.protected:raise ValueError('Expected explicit protection mask')
        return self

class DefectRepairEvidence(Contract):
    candidate_id:SHA256
    label:Label
    applied_pixels:Annotated[int,Field(ge=0,le=200000000)]
    reason:Literal['insufficient_context','zero_strength','protected']|None
    radius:Annotated[float,Field(ge=1,le=5)]
    below_zero:Unit
    above_one:Unit

class DefectRepairReport(Contract):
    params:DefectRepairParams
    review_revision:Annotated[int,Field(ge=1,le=2**53-1)]
    enabled:bool
    requested_strength:Unit
    effective_strength:Unit
    candidates:Annotated[list[DefectRepairEvidence],Field(max_length=4096)]
    warnings:Annotated[list[Literal['module_disabled','no_accepted_candidates','candidate_skipped']],Field(max_length=3)]

from uuid import UUID
from .beauty import BeautyFace

class DefectProposeParams(Contract):
    protections:Annotated[list[MaskRef],Field(max_length=64)]=Field(default_factory=list)
    image:ArtifactRef
    faces:Annotated[list[BeautyFace],Field(max_length=64)]
    permanent:MaskRef|None=None
    diameter_face_fraction:Annotated[list[float],Field(min_length=2,max_length=2)]=Field(default_factory=lambda:[.002,.012])

    @model_serializer(mode='wrap')
    def preserve_protection_field_presence(self,handler):
        result=handler(self)
        # Reports bind to the exact original task Params. Old tasks have no
        # protections key; an explicitly supplied empty list must remain [] .
        if 'protections' not in self.model_fields_set:result.pop('protections',None)
        return result

    @model_validator(mode='after')
    def coherent(self):
        lo,hi=self.diameter_face_fraction
        if not .002<=lo<=hi<=.012:raise ValueError('Invalid diameter fractions')
        keys=[(f.landmarks.artifact_id,f.object_id) for f in self.faces]
        if len(set(keys))!=len(keys):raise ValueError('Duplicate face refs')
        if self.permanent is not None and not self.permanent.protected:raise ValueError('Expected permanent protection')
        if any(not mask.protected for mask in self.protections):raise ValueError('Expected protection inputs')
        ids=[mask.artifact.artifact_id for mask in self.protections]
        if self.permanent is not None:ids.append(self.permanent.artifact.artifact_id)
        if len(set(ids))!=len(ids):raise ValueError('Duplicate protection inputs')
        return self

class DefectProposalFaceReport(Contract):
    object_id:UUID
    reason:Literal['low_confidence','face_too_small','empty_skin','subpixel_diameter','degenerate_landmarks','incomplete_face_roi']|None
    threshold:Annotated[float,Field(ge=0)]
    candidates:Annotated[int,Field(ge=0,le=4096)]
    overlapping_candidates:Annotated[int,Field(ge=0,le=4096)]

class DefectProposeReport(Contract):
    params:DefectProposeParams
    proposal:ArtifactRef
    protection:MaskRef
    label_png:ArtifactRef
    faces:Annotated[list[DefectProposalFaceReport],Field(max_length=64)]
    warnings:Annotated[list[Literal['no_faces','face_skipped','overlapping_candidates']],Field(max_length=3)]
    algorithm:Literal['defect_dog_v1']='defect_dog_v1'
