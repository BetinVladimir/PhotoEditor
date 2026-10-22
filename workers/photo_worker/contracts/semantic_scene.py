"""Additive frozen V27 scene-proof contract; publication requires SQL authority."""
from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256
from photo_worker.operations.session.contracts import VersionBinding


class SemanticSceneSource(Contract):
    binding:VersionBinding
    labels:ArtifactRef
    labels_params:ArtifactRef
    labels_pixels:ArtifactRef
    depth:ArtifactRef
    depth_params:ArtifactRef
    depth_pixels:ArtifactRef
    depth_invalid:ArtifactRef|None


class SemanticScenePolicy(Contract):
    algorithm:Literal['farneback_depth_plane_v1']
    background_labels:Annotated[list[Annotated[int,Field(ge=0,le=149)]],Field(min_length=1,max_length=150)]
    proxy_edge:Annotated[int,Field(ge=32,le=1024)]
    min_support:Annotated[int,Field(ge=32,le=1024**2)]
    flow_error_px:Annotated[float,Field(gt=0,le=2,allow_inf_nan=False)]
    homography_error_px:Annotated[float,Field(gt=0,le=2,allow_inf_nan=False)]
    depth_plane_error:Annotated[float,Field(gt=0,le=.05,allow_inf_nan=False)]

    @model_validator(mode='after')
    def unique_labels(self):
        if self.background_labels!=sorted(set(self.background_labels)):
            raise ValueError('SEMANTIC_SCENE_LABEL_POLICY_INVALID')
        if self.min_support>self.proxy_edge**2:
            raise ValueError('SEMANTIC_SCENE_SUPPORT_POLICY_INVALID')
        return self


class SemanticSceneEvidence(Contract):
    schema_version:Literal[1]
    reference:SemanticSceneSource
    target:SemanticSceneSource
    intent:ArtifactRef
    calibration:ArtifactRef
    policy:SemanticScenePolicy
    policy_sha:SHA256
    model_lock_sha:SHA256
    runtime_lock_sha:SHA256
    device_profile_sha:SHA256
    dataset_sha:SHA256

    @model_validator(mode='after')
    def exact_policy(self):
        from .base import canonical_hash
        if canonical_hash(self.policy)!=self.policy_sha:
            raise ValueError('SEMANTIC_SCENE_POLICY_HASH_INVALID')
        a,b=self.reference,self.target
        if (a.binding.asset_version_id==b.binding.asset_version_id and a.binding!=b.binding):
            raise ValueError('SEMANTIC_SCENE_SOURCE_PAIR_INVALID')
        same_pixels=(a.binding.source==b.binding.source and a.binding.canvas_hash==b.binding.canvas_hash)
        if len({a.labels.artifact_id,a.depth.artifact_id})!=2 or len({b.labels.artifact_id,b.depth.artifact_id})!=2:
            raise ValueError('SEMANTIC_SCENE_EVIDENCE_ALIAS')
        if ({a.labels.artifact_id,a.depth.artifact_id}&{b.labels.artifact_id,b.depth.artifact_id}
                and (not same_pixels or a.labels!=b.labels or a.depth!=b.depth
                     or a.labels_params!=b.labels_params or a.depth_params!=b.depth_params)):
            raise ValueError('SEMANTIC_SCENE_EVIDENCE_ALIAS')
        return self
