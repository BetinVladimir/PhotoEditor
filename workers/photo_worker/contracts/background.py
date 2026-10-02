from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, Unit
from .images import MaskRef, RelativeDepth


class ComposeParams(Contract):
    image: ArtifactRef
    subject: MaskRef
    mode: Literal["transparent", "solid", "asset"]
    solid_rgb: Annotated[list[Unit], Field(min_length=3, max_length=3)] | None = None
    background: ArtifactRef | None = None
    asset_scale: Annotated[float, Field(ge=1, le=10)] = 1
    position_x: Unit = .5
    position_y: Unit = .5
    shadow_opacity: Annotated[float, Field(ge=0, le=.3)] = 0
    shadow_blur_fraction: Annotated[float, Field(ge=0, le=.02)] = 0
    shadow_offset_x: Annotated[float, Field(ge=-.05, le=.05)] = 0
    shadow_offset_y: Annotated[float, Field(ge=-.05, le=.05)] = 0
    tile: Literal[256, 512, 1024] = 512

    @model_validator(mode="after")
    def mode_fields(self):
        if (self.mode == "solid") != (self.solid_rgb is not None):
            raise ValueError("solid_rgb is required only for solid mode")
        if (self.mode == "asset") != (self.background is not None):
            raise ValueError("background is required only for asset mode")
        if self.mode == "transparent" and self.shadow_opacity:
            raise ValueError("Transparent output cannot contain a contact shadow")
        if self.shadow_opacity and self.shadow_blur_fraction == 0:
            raise ValueError("Contact shadow requires a nonzero blur")
        if self.subject.protected:
            raise ValueError("Subject alpha must not be a protection mask")
        if self.subject.selector not in ("person", "subject"):
            raise ValueError("Subject mask selector is invalid")
        return self


class ComposeReport(Contract):
    image: ArtifactRef
    subject: ArtifactRef
    mode: Literal["transparent", "solid", "asset"]
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    subject_confidence: Unit
    source_alpha_combined: bool
    linear_light: Literal[True] = True
    transparent_rgb_zeroed: bool
    asset_scale: Annotated[float, Field(ge=1, le=10)] | None = None
    contact_shadow: bool


class BlurParams(Contract):
    image: ArtifactRef
    subject: MaskRef
    depth: RelativeDepth
    strength: Unit
    tile: Literal[256, 512, 1024] = 512

    @model_validator(mode="after")
    def masks(self):
        if self.subject.protected or self.subject.selector not in ("person", "subject"):
            raise ValueError("Subject mask is invalid")
        return self


class BlurReport(Contract):
    image: ArtifactRef
    subject: ArtifactRef
    depth: ArtifactRef
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    strength: Unit
    layers: Literal[8] = 8
    maximum_radius: Annotated[int, Field(ge=0, le=100_000)]
    subject_depth: Unit
    linear_light: Literal[True] = True


class SkyParams(Contract):
    image: ArtifactRef
    sky_mask: MaskRef
    protected: MaskRef
    sky: ArtifactRef
    match_limit: Annotated[float, Field(ge=0, le=.25)] = .25
    asset_scale: Annotated[float, Field(ge=1, le=10)] = 1
    position_x: Unit = .5
    position_y: Unit = .5
    tile: Literal[256, 512, 1024] = 512

    @model_validator(mode="after")
    def masks(self):
        if self.sky_mask.protected or self.sky_mask.selector != "sky":
            raise ValueError("Sky mask is invalid")
        if not self.protected.protected:
            raise ValueError("Protected mask is invalid")
        return self


class SkyReport(Contract):
    image: ArtifactRef
    sky_mask: ArtifactRef
    protected: ArtifactRef
    sky: ArtifactRef
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    changed_pixels: Annotated[int, Field(ge=0, le=200_000_000)]
    match_limit: Annotated[float, Field(ge=0, le=.25)]
    gains_rgb: Annotated[list[Annotated[float, Field(ge=.75, le=1.25)]], Field(min_length=3, max_length=3)]
    linear_light: Literal[True] = True
