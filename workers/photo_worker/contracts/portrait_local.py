"""V28 local portrait parameters matching implementation-v2 schemas."""
from typing import Annotated, Literal

from pydantic import Field

from photo_worker.contracts.base import ArtifactRef, Contract, Unit
from photo_worker.operations.session.contracts import VersionBinding


class PortraitHairParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    mask: ArtifactRef
    hue_delta_degrees: Annotated[float, Field(ge=-30, le=30)]
    saturation_factor: Annotated[float, Field(ge=.7, le=1.3)]
    detail: Unit
    protected: ArtifactRef


class PortraitClothingParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    mask: ArtifactRef
    protected: ArtifactRef
    hue_delta_degrees: Annotated[float, Field(ge=-180, le=180)]
    saturation_factor: Annotated[float, Field(ge=0, le=2)]
    wrinkle_strength: Unit
