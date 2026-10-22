"""Private qualified-retouch context; content does not grant execution authority."""
from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256
from .beauty import BeautyFace
from .style import Beauty
from photo_worker.operations.session.contracts import VersionBinding


class RetouchPreviewAdapterContext(Contract):
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    algorithm: Literal['beautyfilter_v1']
    source: VersionBinding
    candidate: ArtifactRef
    transferred_mask: ArtifactRef
    protection: ArtifactRef
    faces: Annotated[list[BeautyFace],Field(min_length=1,max_length=64)]
    beauty: Beauty
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def complete_manual_face_inputs(self):
        refs=(self.source.source,self.candidate,self.transferred_mask,self.protection)
        if any(ref.artifact_id.int==0 for ref in refs) or len({ref.artifact_id for ref in refs})!=len(refs):
            raise ValueError('SEMANTIC_RETOUCH_CONTEXT_ALIAS')
        keys=[(face.landmarks.artifact_id,face.object_id) for face in self.faces]
        if len(set(keys))!=len(keys) or any(not face.manual_mode or face.manual is None
                or face.landmarks.artifact_id.int==0 for face in self.faces):
            raise ValueError('SEMANTIC_RETOUCH_FACE_CONTEXT_INVALID')
        return self
