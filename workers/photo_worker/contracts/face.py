from typing import Annotated,Literal
from pydantic import Field
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Face68,FaceBox


class FaceDetectParams(Contract):
    image:ArtifactRef
    model_id:Literal['sfd']='sfd'
    model_lock_id:SHA256
    preprocessing_id:Literal['sfd_long1024_bgr_v1']='sfd_long1024_bgr_v1'
    long_side:Literal[1024]=1024
    score_threshold:Annotated[float,Field(ge=.5,le=.99)]=.8
    nms_iou:Annotated[float,Field(gt=0,le=.5)]=.3
    max_faces:Annotated[int,Field(ge=1,le=100)]=64


class FaceBoxes(Contract):
    image:ArtifactRef
    faces:Annotated[list[FaceBox],Field(max_length=100)]
    model_id:Literal['sfd']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['sfd_long1024_bgr_v1']
    preview_width:Annotated[int,Field(ge=1,le=1024)]
    preview_height:Annotated[int,Field(ge=1,le=1024)]
    candidate_count:Annotated[int,Field(ge=0,le=100000)]
    accepted_count:Annotated[int,Field(ge=0,le=100)]
    alpha_excluded_fraction:Unit
    warnings:Annotated[list[Literal['no_faces','faces_truncated','source_srgb_clipped']],Field(max_length=3)]


class LandmarkParams(Contract):
    image:ArtifactRef
    faces:ArtifactRef
    model_id:Literal['fan2d']='fan2d'
    model_lock_id:SHA256
    preprocessing_id:Literal['fan2d_crop256_v1']='fan2d_crop256_v1'
    crop_size:Literal[256]=256
    expansion:Literal[1.25]=1.25


class FaceLandmarks(Contract):
    image:ArtifactRef
    faces:ArtifactRef
    landmarks:Annotated[list[Face68],Field(max_length=100)]
    model_id:Literal['fan2d']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['fan2d_crop256_v1']
    mean_confidence:Unit
    warnings:Annotated[list[Literal['no_faces','low_confidence','source_srgb_clipped']],Field(max_length=3)]
