from typing import Literal

from .base import ArtifactRef, Contract


class FaceSkinParams(Contract):
    labels: ArtifactRef
    algorithm: Literal['bisenet_face19_skin_v1'] = 'bisenet_face19_skin_v1'

