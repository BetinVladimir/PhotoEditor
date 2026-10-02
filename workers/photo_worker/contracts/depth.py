from typing import Literal
from .base import ArtifactRef,Contract,SHA256


class DepthParams(Contract):
    image:ArtifactRef
    model_id:Literal['depth_anything_v2_small']='depth_anything_v2_small'
    model_lock_id:SHA256
    preprocessing_id:Literal['depth_anything_v2_small_518_v1']='depth_anything_v2_small_518_v1'
    input_size:Literal[518]=518
