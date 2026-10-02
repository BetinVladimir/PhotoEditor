from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import MaskRef
from .style import Beauty,Defects,Disabled


class BeautyFitParams(Contract):
    pair_id:UUID
    before:ArtifactRef
    after:ArtifactRef
    alignment:ArtifactRef
    before_landmarks:ArtifactRef
    after_landmarks:ArtifactRef
    skin:MaskRef
    permanent:MaskRef|None=None
    min_confidence:Literal[.75]=.75
    algorithm:Literal['beauty_fit_coordinate_v1']='beauty_fit_coordinate_v1'

    @model_validator(mode='after')
    def masks(self):
        if self.skin.selector!='skin' or self.skin.protected:raise ValueError('Beauty fit requires unprotected skin mask')
        if self.permanent is not None and not self.permanent.protected:raise ValueError('Permanent mask must be protected')
        return self


class BeautyFaceFitEvidence(Contract):
    before_object_id:UUID
    after_object_id:UUID
    baseline_loss:Annotated[float,Field(ge=0)]
    fitted_loss:Annotated[float,Field(ge=0)]
    improvement:Annotated[float,Field(ge=-1,le=1)]
    texture_energy_ratio:Annotated[float,Field(ge=0)]|None
    confidence:Unit
    accepted:bool
    reason:Literal['applied','low_confidence','face_too_small','no_improvement','invalid_roi']


class BeautyFit(Contract):
    params:BeautyFitParams
    beauty:Annotated[Beauty|Disabled,Field(discriminator='enabled')]
    confidence:Unit
    faces:Annotated[list[BeautyFaceFitEvidence],Field(max_length=64)]
    search_digest:SHA256
    transfer_confidence:Literal['unvalidated']='unvalidated'
    warnings:Annotated[list[Literal['faces_skipped','inconsistent_faces','no_measured_improvement']],Field(max_length=3)]


class DefectFitParams(Contract):
    pair_id:UUID
    before:ArtifactRef
    after:ArtifactRef
    alignment:ArtifactRef
    skin_masks:Annotated[list[MaskRef],Field(min_length=1,max_length=64)]
    permanent:MaskRef|None=None
    min_confidence:Literal[.75]=.75
    algorithm:Literal['defect_evidence_v1']='defect_evidence_v1'

    @model_validator(mode='after')
    def masks(self):
        if any(m.selector!='skin' or m.protected for m in self.skin_masks):raise ValueError('Defect fit requires skin masks')
        if self.permanent is not None and not self.permanent.protected:raise ValueError('Permanent mask must be protected')
        return self


class DefectEvidence(Contract):
    params:DefectFitParams
    defects:Annotated[Defects|Disabled,Field(discriminator='enabled')]
    confidence:Unit
    residual_components:Annotated[int,Field(ge=0,le=65535)]
    improved_components:Annotated[int,Field(ge=0,le=65535)]
    median_residual_reduction:Annotated[float,Field(ge=-1,le=1)]
    evidence_digest:SHA256
    transfer_confidence:Literal['unvalidated']='unvalidated'
    warnings:Annotated[list[Literal['low_mask_confidence','no_residual_evidence','review_policy_forced']],Field(max_length=3)]
