from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field

from .base import ArtifactRef, Contract, SHA256, Unit
from .images import MaskRef


class DenoiseParams(Contract):
    image: ArtifactRef
    strength: Unit
    model_id: Literal["scunet_color_real_psnr"] = "scunet_color_real_psnr"
    model_lock_id: SHA256
    color_adapter_id: Literal["work_srgb_delta_v1"] = "work_srgb_delta_v1"
    tile: Literal[256, 512] = 512
    overlap: Literal[64] = 64


class DenoiseReport(Contract):
    image: ArtifactRef
    model_id: Literal["scunet_color_real_psnr"]
    model_lock_id: SHA256
    backend: Literal["cpu", "cuda", "mps"]
    color_adapter_id: Literal["work_srgb_delta_v1"]
    strength: Unit
    tile: Literal[256, 512]
    overlap: Literal[64]
    pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    disabled_delta_fraction: Unit
    source_clip_fraction: Unit
    gamut_excess_fraction: Unit
    alpha_preserved: bool
    warnings: list[Literal["source_srgb_clipped", "delta_gamut_excess"]] = Field(max_length=2)


class UpscaleParams(Contract):
    image: ArtifactRef
    scale: Literal[2, 4]
    model_id: Literal["realesrgan_x2", "realesrgan_x4"]
    model_lock_id: SHA256
    color_adapter_id: Literal["work_srgb_delta_v1"] = "work_srgb_delta_v1"
    tile: Literal[256] = 256
    overlap: Literal[32] = 32

    def model_post_init(self, __context):
        if self.model_id != f"realesrgan_x{self.scale}":
            raise ValueError("Upscale model does not match scale")


class UpscaleReport(Contract):
    image: ArtifactRef
    model_id: Literal["realesrgan_x2", "realesrgan_x4"]
    model_lock_id: SHA256
    backend: Literal["cpu", "cuda", "mps"]
    scale: Literal[2, 4]
    color_adapter_id: Literal["work_srgb_delta_v1"]
    tile: Literal[256]
    overlap: Literal[32]
    input_pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    output_pixel_count: Annotated[int, Field(ge=1, le=200_000_000)]
    disabled_delta_fraction: Unit
    source_clip_fraction: Unit
    gamut_excess_fraction: Unit
    alpha_resampled: bool
    warnings: list[Literal["source_srgb_clipped", "delta_gamut_excess"]] = Field(max_length=2)


class InpaintParams(Contract):
    image: ArtifactRef
    mask: MaskRef
    protected: Annotated[list[MaskRef], Field(max_length=32)] = Field(default_factory=list)
    confirmed: Literal[True]
    model_id: Literal["big_lama"] = "big_lama"
    model_lock_id: SHA256
    context_pixels: Annotated[int, Field(ge=32, le=512)] = 128
    feather_pixels: Annotated[int, Field(ge=1, le=64)] = 8

    def model_post_init(self, __context):
        if self.mask.protected:
            raise ValueError("Inpaint target cannot be protected")
        if any(not item.protected for item in self.protected):
            raise ValueError("Protected masks must be marked protected")


class InpaintReport(Contract):
    image: ArtifactRef
    mask: ArtifactRef
    model_id: Literal["big_lama"]
    model_lock_id: SHA256
    backend: Literal["cpu", "cuda", "mps"]
    changed_pixels: Annotated[int, Field(ge=0, le=200_000_000)]
    target_pixels: Annotated[int, Field(ge=1, le=200_000_000)]
    protected_pixels: Annotated[int, Field(ge=0, le=200_000_000)]
    global_pass: bool
    roi: Annotated[list[int], Field(min_length=4, max_length=4)]
    feather_pixels: Annotated[int, Field(ge=1, le=64)]
    alpha_preserved: bool
    candidate_requires_review: Literal[True] = True


class GenerationParams(Contract):
    image: ArtifactRef
    mask: MaskRef
    protected: Annotated[list[MaskRef], Field(max_length=32)] = Field(default_factory=list)
    confirmed: Literal[True]
    prompt: Annotated[str, Field(min_length=1, max_length=1000)]
    negative_prompt: Annotated[str, Field(max_length=1000)] = ""
    seed: Annotated[int, Field(ge=0, le=2**32-1)]
    steps: Literal[25] = 25
    guidance_scale: Annotated[float, Field(ge=1, le=20)] = 7.5
    model_id: Literal["sd15_inpaint"] = "sd15_inpaint"
    model_lock_id: SHA256
    feather_pixels: Annotated[int, Field(ge=1, le=64)] = 8

    def model_post_init(self, __context):
        if self.mask.protected or any(not item.protected for item in self.protected):
            raise ValueError("Generation mask roles invalid")


class GenerationReport(Contract):
    image: ArtifactRef
    mask: ArtifactRef | None
    model_id: Literal["sd15_inpaint"]
    model_lock_id: SHA256
    backend: Literal["cpu", "cuda", "mps"]
    seed: Annotated[int, Field(ge=0, le=2**32-1)]
    scheduler: Literal["ddim_v1"] = "ddim_v1"
    steps: Literal[25] = 25
    guidance_scale: Annotated[float, Field(ge=1, le=20)]
    generated_pixels: Annotated[int, Field(ge=1, le=200_000_000)]
    protected_pixels: Annotated[int, Field(ge=0, le=200_000_000)]
    side: Literal["left", "right", "top", "bottom"] | None
    source_rect: Annotated[list[int], Field(min_length=4, max_length=4)]
    candidate_requires_review: Literal[True] = True


class OutpaintParams(Contract):
    image: ArtifactRef
    side: Literal["left", "right", "top", "bottom"]
    pixels: Annotated[int, Field(ge=8, le=8192)]
    prompt: Annotated[str, Field(min_length=1, max_length=1000)]
    negative_prompt: Annotated[str, Field(max_length=1000)] = ""
    seed: Annotated[int, Field(ge=0, le=2**32-1)]
    steps: Literal[25] = 25
    guidance_scale: Annotated[float, Field(ge=1, le=20)] = 7.5
    model_id: Literal["sd15_inpaint"] = "sd15_inpaint"
    model_lock_id: SHA256
    feather_pixels: Annotated[int, Field(ge=1, le=64)] = 16
