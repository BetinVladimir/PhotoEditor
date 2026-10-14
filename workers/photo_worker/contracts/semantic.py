from uuid import UUID
from photo_worker.operations.session.contracts import VersionBinding
from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Canvas,MaskRef,TransformChain


class SceneParseParams(Contract):
    image:ArtifactRef
    model_id:Literal['segformer_b0_ade']='segformer_b0_ade'
    model_lock_id:SHA256
    preprocessing_id:Literal['segformer_ade512_official_v1']='segformer_ade512_official_v1'


class SceneLabels(Contract):
    image:ArtifactRef
    labels:ArtifactRef
    canvas:Canvas
    transform_chain:TransformChain
    classes:Literal[150]=150
    model_id:Literal['segformer_b0_ade']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['segformer_ade512_official_v1']
    confidence:Unit
    warnings:Annotated[list[Literal['low_confidence','source_srgb_clipped']],Field(max_length=2)]

    @model_validator(mode='after')
    def geometry(self):self.transform_chain.validate_target(self.canvas);return self


class AlphaParams(Contract):
    image:ArtifactRef
    model_id:Literal['u2net']='u2net'
    model_lock_id:SHA256
    preprocessing_id:Literal['u2net_320_v1']='u2net_320_v1'


class AlphaPrediction(Contract):
    image:ArtifactRef
    alpha:MaskRef
    preview_width:Literal[320]=320
    preview_height:Literal[320]=320
    model_id:Literal['u2net']
    model_lock_id:SHA256
    backend:Literal['cpu']
    preprocessing_id:Literal['u2net_320_v1']
    foreground_fraction:Unit
    source_clip_fraction:Unit
    warnings:Annotated[list[Literal['empty_foreground','low_confidence','source_srgb_clipped']],Field(max_length=3)]


class SemanticTransferParams(Contract):
    """V27 pinned intent/calibration and exact reference/target versions."""
    schema_version:Literal[1]
    intent:ArtifactRef
    reference:VersionBinding
    target:VersionBinding
    model_lock_sha:SHA256
    calibration:ArtifactRef


class TransferCandidate(Contract):
    """Proposal only; neither proposed nor confidence implies human approval."""
    schema_version:Literal[1]
    target:VersionBinding
    mask:ArtifactRef|None
    transform:ArtifactRef|None
    confidence:Unit
    state:Literal['proposed','unsupported','rejected']
    reason:Annotated[str,Field(min_length=1,max_length=80)]|None


class EditIntent(Contract):
    schema_version:Literal[1]
    kind:Literal['remove_object','local_exposure','background_cleanup','retouch_region']
    reference:VersionBinding
    reference_mask:ArtifactRef
    target_snapshot_id:UUID
    strength:Unit
    ev:Annotated[float,Field(ge=-1,le=1,allow_inf_nan=False)]|None
    preserve_persons:Literal[True]
    prompt:Annotated[str,Field(min_length=1,max_length=1024)]|None

    @model_validator(mode='after')
    def exposure_kind(self):
        if (self.kind=='local_exposure')!=(self.ev is not None):
            raise ValueError('SEMANTIC_INTENT_EV_INVALID')
        return self


def bind_semantic_transfer_documents(params,intent,calibration):
    """Owned content binding only; scoped provenance/qualification is upstream."""
    from .base import canonical_bytes
    from photo_worker.operations.session.contracts import Calibration
    params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
    intent=EditIntent.model_validate_json(canonical_bytes(intent))
    calibration=Calibration.model_validate_json(canonical_bytes(calibration))
    if intent.reference!=params.reference:raise ValueError('SEMANTIC_REFERENCE_CHANGED')
    if not calibration.qualified or calibration.model_lock_sha!=params.model_lock_sha:
        raise ValueError('SEMANTIC_CALIBRATION_INVALID')
    if calibration.match_error_px_max<=0:raise ValueError('SEMANTIC_CALIBRATION_INVALID')
    return params,intent,calibration


class V2SemanticAssembleParams(Contract):
    schema_version:Literal[1]
    intent:ArtifactRef
    candidates:Annotated[list[ArtifactRef],Field(min_length=1,max_length=5000)]
    model_lock_sha:SHA256
    calibration:ArtifactRef


class SemanticPlan(Contract):
    schema_version:Literal[1]
    intent_sha:SHA256
    snapshot_digest:SHA256
    candidates:Annotated[list[TransferCandidate],Field(min_length=1,max_length=5000)]
    model_lock_sha:SHA256
    calibration:ArtifactRef


class ApprovedSemanticEdit(Contract):
    """Downstream compilation input; does not prove human/SQL admission."""
    schema_version:Annotated[int,Field(strict=True,ge=1,le=1)]
    target_id:UUID
    source:VersionBinding
    intent:ArtifactRef
    candidate:ArtifactRef
    mask:ArtifactRef
    transform:ArtifactRef
    kind:Literal['remove_object','local_exposure','background_cleanup','retouch_region']
    strength:Unit
    ev:Annotated[float,Field(ge=-1,le=1)]|None
    prompt:Annotated[str,Field(min_length=1,max_length=1024)]|None
    preserve_persons:bool

    @model_validator(mode='after')
    def bindings(self):
        if not self.preserve_persons:
            raise ValueError('SEMANTIC_EDIT_PERSON_PROTECTION_REQUIRED')
        if (self.kind=='local_exposure')!=(self.ev is not None):
            raise ValueError('SEMANTIC_EDIT_EV_INVALID')
        if (self.target_id.int==0 or self.source.asset_version_id.int==0
                or any(ref.artifact_id.int==0 for ref in (self.source.source,self.intent,self.candidate,self.mask,self.transform))):
            raise ValueError('SEMANTIC_EDIT_BINDING_INVALID')
        return self
