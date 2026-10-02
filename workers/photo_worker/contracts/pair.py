from __future__ import annotations
from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef,UInt32,Unit
from .images import Canvas,Matrix3,MaskRef

class RegistrationParams(Contract):
    before:ArtifactRef
    after:ArtifactRef
    seed:UInt32=42
    mode:Literal['similarity','crop_aware']='similarity'
    manual_transform:Matrix3|None=None
    manual_confirmed:bool=False
    background_mask:MaskRef|None=None

    @model_validator(mode='after')
    def explicit_confirmation(self):
        if self.manual_confirmed!=(self.manual_transform is not None):
            raise ValueError('Manual transform requires explicit confirmation')
        return self

class PairAlignment(Contract):
    before:ArtifactRef
    after:ArtifactRef
    before_canvas:Canvas
    after_canvas:Canvas
    before_to_after:Matrix3
    overlap:Unit
    median_reprojection:Annotated[float,Field(ge=0,le=1)]|None
    inlier_count:Annotated[int,Field(ge=0,le=4000)]
    confidence:Literal['measured','user_confirmed']
    crop_aware:bool
    warnings:list[Literal['ecc_not_improved','ecc_not_converged','background_mask_not_provided']]

    @model_validator(mode='after')
    def quality_evidence(self):
        if self.overlap<(.5 if self.crop_aware else .9):raise ValueError('Insufficient overlap')
        if self.confidence=='measured' and (self.median_reprojection is None or self.inlier_count<12):
            raise ValueError('Missing measured alignment evidence')
        if self.confidence=='user_confirmed' and (self.median_reprojection is not None or self.inlier_count!=0):
            raise ValueError('Manual alignment cannot invent measured evidence')
        from photo_worker.algorithms.registration import _similarity
        import numpy as np
        _similarity(np.array(self.before_to_after).reshape(3,3))
        return self
