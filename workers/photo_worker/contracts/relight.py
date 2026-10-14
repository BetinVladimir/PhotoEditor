"""ImageRelightParams schema; this contract alone grants no execution authority."""
from typing import Annotated,Literal
from pydantic import Field
from .base import ArtifactRef,Contract,Unit
from photo_worker.operations.session.contracts import VersionBinding


class RelightLight(Contract):
    x:Annotated[float,Field(ge=-1,le=2)]
    y:Annotated[float,Field(ge=-1,le=2)]
    radius:Annotated[float,Field(ge=.05,le=2)]
    ev:Annotated[float,Field(ge=-1,le=1)]


class ImageRelightParams(Contract):
    schema_version:Literal[1]
    base:VersionBinding
    strength:Unit
    depth:ArtifactRef
    subject:ArtifactRef
    protected:ArtifactRef
    lights:Annotated[list[RelightLight],Field(min_length=1,max_length=4)]
    calibration:ArtifactRef
