from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef,Unit
from .images import MaskRef
from .style import Beauty,Disabled

class BeautyFace(Contract):
    landmarks:ArtifactRef
    object_id:UUID|None=None
    skin:MaskRef|None=None
    manual:MaskRef|None=None
    manual_mode:bool=False

    @model_validator(mode='after')
    def mode_has_mask(self):
        if self.manual_mode and self.manual is None:raise ValueError('Manual mode requires mask')
        if not self.manual_mode and self.skin is None:raise ValueError('Automatic skin parsing required')
        if self.skin is not None and (self.skin.selector!='skin' or self.skin.protected):raise ValueError('Invalid skin mask selector')
        if self.manual is not None and (self.manual.selector!='manual' or self.manual.protected):raise ValueError('Invalid manual mask selector')
        return self

class BeautyParams(Contract):
    image:ArtifactRef
    resolved:ArtifactRef
    faces:Annotated[list[BeautyFace],Field(max_length=64)]
    permanent:MaskRef|None=None

    @model_validator(mode='after')
    def coherent(self):
        keys=[(f.landmarks.artifact_id,f.object_id) for f in self.faces]
        if len(set(keys))!=len(keys):raise ValueError('Duplicate landmarks')
        if self.permanent is not None and not self.permanent.protected:raise ValueError('Expected protected permanent mask')
        return self

class BeautyFaceReport(Contract):
    object_id:UUID
    applied:bool
    reason:Literal['low_confidence','face_too_small','zero_strength','empty_skin','degenerate_landmarks','incomplete_face_roi']|None
    texture_energy_ratio:Annotated[float,Field(ge=0)]|None
    below_zero:Unit
    above_one:Unit

class BeautyReport(Contract):
    image:ArtifactRef
    resolved:ArtifactRef
    requested:Annotated[Beauty|Disabled,Field(discriminator='enabled')]
    effective:Annotated[Beauty|Disabled,Field(discriminator='enabled')]
    faces:Annotated[list[BeautyFaceReport],Field(max_length=64)]
    warnings:Annotated[list[Literal['module_disabled','no_faces','face_skipped']],Field(max_length=3)]
