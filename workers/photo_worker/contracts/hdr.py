from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, Unit
from .images import Canvas, MaskRef, Matrix3


Exposure = Annotated[float, Field(gt=0, le=3600)]


class WindowParams(Contract):
    image: ArtifactRef
    dark: ArtifactRef
    mask: MaskRef
    reference_exposure_seconds: Exposure
    dark_exposure_seconds: Exposure
    confirmed: Literal[True]
    tile: Literal[256, 512, 1024] = 512

    @model_validator(mode="after")
    def mask_role(self):
        if self.mask.protected or self.mask.selector not in ("window", "manual"):
            raise ValueError("Window mask is invalid")
        ratio = self.reference_exposure_seconds / self.dark_exposure_seconds
        if not 1 <= ratio <= 64:
            raise ValueError("Dark exposure must be one to six stops below reference")
        return self


class WindowReport(Contract):
    image: ArtifactRef
    dark: ArtifactRef
    mask: ArtifactRef
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    changed_pixels: Annotated[int, Field(ge=0, le=200_000_000)]
    exposure_ratio: Annotated[float, Field(ge=1, le=64)]
    unrecoverable_fraction: Unit
    linear_light: Literal[True] = True
    warnings: list[Literal["all_exposures_clipped"]] = Field(max_length=1)


class BracketFrame(Contract):
    image: ArtifactRef
    exposure_seconds: Exposure


class HDRRegisterParams(Contract):
    frames: Annotated[list[BracketFrame], Field(min_length=3, max_length=9)]
    reference_index: Annotated[int, Field(ge=0, le=8)]
    wb_recipe: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$")]
    seed: Annotated[int, Field(ge=0, le=2**32 - 1)] = 42

    @model_validator(mode="after")
    def reference_exists(self):
        if self.reference_index >= len(self.frames):
            raise ValueError("Reference index exceeds bracket")
        if len({frame.image.artifact_id for frame in self.frames}) != len(self.frames):
            raise ValueError("Bracket images must be unique")
        return self


class BracketTransform(Contract):
    frame_index: Annotated[int, Field(ge=0, le=8)]
    image: ArtifactRef
    to_reference: Matrix3
    overlap: Unit
    median_reprojection: Annotated[float, Field(ge=0, le=1)] | None
    inlier_count: Annotated[int, Field(ge=0, le=4000)]
    motion_fraction: Unit
    reference: bool


class BracketTransforms(Contract):
    frames: Annotated[list[BracketFrame], Field(min_length=3, max_length=9)]
    reference_index: Annotated[int, Field(ge=0, le=8)]
    reference_canvas: Canvas
    wb_recipe: str
    transforms: Annotated[list[BracketTransform], Field(min_length=3, max_length=9)]
    motion_threshold: Literal[0.05] = .05
    motion_review_fraction: Literal[0.01] = .01
    warnings: list[Literal["motion_detected", "ecc_not_improved", "ecc_not_converged", "mtb_fallback"]] = Field(max_length=4)

    @model_validator(mode="after")
    def complete(self):
        if self.reference_index >= len(self.frames) or len(self.transforms) != len(self.frames):
            raise ValueError("Incomplete bracket transforms")
        if [item.frame_index for item in self.transforms] != list(range(len(self.frames))):
            raise ValueError("Transforms must follow bracket order")
        if sum(item.reference for item in self.transforms) != 1 or not self.transforms[self.reference_index].reference:
            raise ValueError("Reference transform mismatch")
        return self


class HDRMergeParams(Contract):
    registration: ArtifactRef
    mode: Literal["debevec", "mertens"]
    tile: Literal[256, 512, 1024] = 512


class HDRReport(Contract):
    registration: ArtifactRef
    mode: Literal["debevec", "mertens"]
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    response_calibrated: bool
    radiometric: bool
    deghost_fraction: Unit
    unrecoverable_fraction: Unit
    tonemap: Literal["reinhard_intensity0_light1_color0_gamma1", "mertens_default_v1"]
    warnings: list[Literal["motion_deghosted", "all_exposures_clipped", "non_radiometric_mertens", "registration_review"]] = Field(max_length=4)
