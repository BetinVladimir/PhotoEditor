from typing import Annotated,Literal
from pydantic import Field
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Pose17


class PoseParams(Contract):
    image:ArtifactRef
    model_id:Literal['keypointrcnn_resnet50_fpn']='keypointrcnn_resnet50_fpn'
    model_lock_id:SHA256
    preprocessing_id:Literal['keypointrcnn_coco_v1']='keypointrcnn_coco_v1'
    min_size:Literal[640]=640
    max_size:Literal[1024]=1024
    score_threshold:Annotated[float,Field(ge=.5,le=.99)]=.75
    max_people:Annotated[int,Field(ge=1,le=100)]=32


class PoseSet(Contract):
    image:ArtifactRef
    poses:Annotated[list[Pose17],Field(max_length=100)]
    model_id:Literal['keypointrcnn_resnet50_fpn']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['keypointrcnn_coco_v1']
    detected_count:Annotated[int,Field(ge=0,le=10000)]
    accepted_count:Annotated[int,Field(ge=0,le=100)]
    alpha_excluded_fraction:Unit
    warnings:Annotated[list[Literal['no_people','people_truncated','low_keypoint_confidence','source_srgb_clipped']],Field(max_length=4)]
