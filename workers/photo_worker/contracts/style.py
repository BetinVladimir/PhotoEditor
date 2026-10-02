from __future__ import annotations

from typing import Annotated, Literal

from pydantic import Field, model_validator

from .base import Contract, SHA256, UInt32, Unit, canonical_bytes, canonical_hash, parse_json

Curve = Annotated[list[Unit], Field(min_length=7, max_length=7)]
RGB = Annotated[list[float], Field(min_length=3, max_length=3)]
Features = Annotated[list[float], Field(min_length=20, max_length=20)]


class Disabled(Contract):
    enabled: Literal[False] = False


class LUT(Contract):
    kind: Literal["residual", "absolute"]
    size: Literal[17, 33, 65]
    order: Literal["RGB_C"] = "RGB_C"
    domain: list[float] = Field(default_factory=lambda: [0., 1.])
    encoding: Literal["dense", "constant"]
    values: list[float]

    @model_validator(mode="after")
    def validate_lut(self):
        if self.domain != [0, 1]:
            raise ValueError("LUT domain must be [0,1]")
        count = 3 if self.encoding == "constant" else self.size**3 * 3
        if len(self.values) != count:
            raise ValueError(f"LUT requires {count} values")
        low, high = (-.25, .25) if self.kind == "residual" else (0, 1)
        if any(not low <= value <= high for value in self.values):
            raise ValueError("LUT value outside declared range")
        if self.kind == "residual" and self.size != 17:
            raise ValueError("ColorProgram v1 residual grid must be 17")
        if self.kind == "absolute" and self.size not in (33, 65):
            raise ValueError("Adaptive LUT grid must be 33 or 65")
        return self


class GlobalColor(Contract):
    gains_rgb: Annotated[list[Annotated[float, Field(ge=.25, le=4)]], Field(min_length=3, max_length=3)]
    master_curve: Curve
    rgb_curves: Annotated[list[Curve], Field(min_length=3, max_length=3)]
    saturation: Annotated[float, Field(ge=0, le=2)]
    residual_lut: LUT

    @model_validator(mode="after")
    def monotonic(self):
        for curve in [self.master_curve, *self.rgb_curves]:
            if any(a > b for a, b in zip(curve, curve[1:])):
                raise ValueError("Curves must be nondecreasing")
        if self.residual_lut.kind != "residual":
            raise ValueError("global.residual_lut must be residual")
        return self


class LocalColor(Contract):
    enabled: Literal[True]
    person_ev: Annotated[float, Field(ge=-1, le=1)]
    background_ev: Annotated[float, Field(ge=-1, le=1)]
    person_saturation: Annotated[float, Field(ge=.7, le=1.3)]
    background_saturation: Annotated[float, Field(ge=.7, le=1.3)]
    mask_policy_id: Literal["person_guided_v1"]


LocalModule = Annotated[Disabled | LocalColor, Field(discriminator="enabled")]


class Beauty(Contract):
    enabled: Literal[True]
    algorithm: Literal["beautyfilter_v1"]
    smoothing: Unit
    tone_evenness: Unit
    texture_keep: Unit
    skin_mask: Literal["face68_conservative_v1"]
    protect_features: Literal[True]


class Defects(Contract):
    enabled: Literal[True]
    algorithm: Literal["defectrepair_telea_v1"]
    policy: Literal["review", "conservative_auto"]
    strength: Unit
    diameter_face_fraction: Annotated[list[float], Field(min_length=2, max_length=2)]
    preserve_permanent_marks: Literal[True]

    @model_validator(mode="after")
    def valid_diameter(self):
        lo, hi = self.diameter_face_fraction
        if not .002 <= lo <= hi <= .012:
            raise ValueError("Defect diameter outside supported range")
        return self


class Geometry(Contract):
    enabled: Literal[True]
    algorithm: Literal["bodywarp_v1"]
    body_strength: Unit
    face_slim: Annotated[float, Field(ge=0, le=.05)]
    waist_slim: Annotated[float, Field(ge=0, le=.08)]
    hip_slim: Annotated[float, Field(ge=0, le=.08)]
    torso_slim: Annotated[float, Field(ge=0, le=.08)]
    leg_length: Annotated[float, Field(ge=0, le=.03)]
    protect_background: Literal[True]
    max_displacement_image_fraction: Annotated[float, Field(gt=0, le=.02)]
    on_unsafe: Literal["skip_module"]


class Culling(Contract):
    enabled: Literal[True]
    algorithm: Literal["cullrank_v1"]
    target_count: Annotated[int, Field(ge=1)] | None = None
    target_fraction: Annotated[float, Field(gt=0, le=1)] | None = None
    never_delete: Literal[True]
    eye_policy: Literal["review"]

    @model_validator(mode="after")
    def one_target(self):
        if self.target_count is not None and self.target_fraction is not None:
            raise ValueError("Select count or fraction, not both")
        return self


class Series(Contract):
    enabled: Literal[True]
    algorithm: Literal["seriesmatch_v1"]
    reference: str
    ev_limit: Annotated[float, Field(ge=0, le=.35)]
    rgb_gain_limits: Annotated[list[float], Field(min_length=2, max_length=2)]

    @model_validator(mode="after")
    def bounds(self):
        lo, hi = self.rgb_gain_limits
        if not .95 <= lo <= 1 <= hi <= 1.05:
            raise ValueError("Series gain bounds exceed safe range")
        return self


class Composition(Contract):
    enabled: Literal[True]
    aspect: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=2, max_length=2)]
    headroom: Unit
    rotate_limit_deg: Annotated[float, Field(ge=0, le=5)]
    outpaint_policy: Literal["off", "review"] = "off"
    outpaint_prompt: Annotated[str, Field(max_length=1000)] = ""
    outpaint_negative_prompt: Annotated[str, Field(max_length=1000)] = ""
    outpaint_seed: UInt32 = 0
    outpaint_guidance_scale: Annotated[float, Field(ge=1, le=20)] = 7.5

    @model_validator(mode="after")
    def explicit_outpaint(self):
        if self.outpaint_policy == "review" and not self.outpaint_prompt:
            raise ValueError("Review outpaint requires a prompt")
        if self.outpaint_policy == "off" and (self.outpaint_prompt or self.outpaint_negative_prompt):
            raise ValueError("Disabled outpaint cannot carry prompts")
        return self


class PortraitDetail(Contract):
    enabled: Literal[True]
    teeth_strength: Unit
    shine_strength: Unit
    under_eye_strength: Unit
    glasses_policy: Literal["review"]


class Masking(Contract):
    scene: Literal["segformer_b0_ade"]
    face: Literal["bisenet_face19"]
    interactive: Literal["mobilesam"]


class Restoration(Contract):
    denoise: Literal["off", "scunet_color_real_psnr"]
    denoise_strength: Unit
    upscale: Literal["off", "2x", "4x"]
    sensor_dust: Literal["off", "review", "conservative_auto"]


class Background(Contract):
    mode: Literal["keep", "transparent", "solid", "asset", "blur", "generate"]
    asset_id: str | None
    solid_rgb: Annotated[list[Unit], Field(min_length=3, max_length=3)] | None = None
    blur_strength: Unit
    protect_subject: Literal[True]

    @model_validator(mode="after")
    def required_asset(self):
        if self.mode == "asset" and not self.asset_id:
            raise ValueError("Asset background requires portable asset_id")
        if self.mode == "solid" and self.solid_rgb is None:
            raise ValueError("Solid background requires solid_rgb")
        return self


class Inpainting(Contract):
    enabled: Literal[True]
    model: Literal["big_lama"]
    policy: Literal["review"]
    mask_source: Literal["per_image"]


class Generation(Contract):
    enabled: Literal[True]
    model: Literal["sd15_inpaint"]
    prompt: Annotated[str, Field(max_length=2000)]
    negative_prompt: Annotated[str, Field(max_length=2000)]
    seed: UInt32
    steps: Annotated[int, Field(ge=1, le=50)]
    guidance_scale: Annotated[float, Field(ge=0, le=20)]
    strength: Unit
    canvas: Annotated[list[int], Field(min_length=2, max_length=2)]
    require_acceptance: Literal[True]

    @model_validator(mode="after")
    def canvas_size(self):
        if self.canvas != [512, 512]:
            raise ValueError("SD15 canvas must be 512x512")
        return self


class RegionAdjustment(Contract):
    region_id: str
    selector: Literal["person", "background", "skin", "sky", "clothing", "manual"]
    ev: Annotated[float, Field(ge=-1, le=1)]
    saturation: Annotated[float, Field(ge=.7, le=1.3)]
    hue_deg: Annotated[float, Field(ge=-15, le=15)]
    strength: Unit
    priority: int


class Extensions(Contract):
    culling: Annotated[Disabled | Culling, Field(discriminator="enabled")] | None = None
    series: Annotated[Disabled | Series, Field(discriminator="enabled")] | None = None
    masking: Masking | None = None
    composition: Annotated[Disabled | Composition, Field(discriminator="enabled")] | None = None
    portrait_detail: Annotated[Disabled | PortraitDetail, Field(discriminator="enabled")] | None = None
    restoration: Restoration | None = None
    background: Background | None = None
    inpainting: Annotated[Disabled | Inpainting, Field(discriminator="enabled")] | None = None
    generation: Annotated[Disabled | Generation, Field(discriminator="enabled")] | None = None
    region_adjustments: list[RegionAdjustment] = Field(default_factory=list, max_length=128)


class ModelReference(Contract):
    id: str
    weight_id: str


class ColorSpace(Contract):
    profile_id: Literal["work_rec2020_gamma24_v1"]
    intent: Literal["relative"]
    bpc: Literal[False]


class Prototype(Contract):
    id: str
    features: Features
    absolute_lut: LUT
    local: LocalModule

    @model_validator(mode="after")
    def absolute(self):
        if self.absolute_lut.kind != "absolute":
            raise ValueError("Prototype must contain absolute LUT")
        return self


class Adaptation(Contract):
    feature_version: Literal["stats20_v1"]
    mean: Features
    scale: Annotated[list[Annotated[float, Field(ge=.05)]], Field(min_length=20, max_length=20)]
    k: Annotated[int, Field(ge=1, le=4)]
    temperature: Literal[1]
    ood_threshold: Annotated[float, Field(ge=1)]
    prototypes: Annotated[list[Prototype], Field(min_length=1, max_length=32)]
    coverage_bins: Annotated[list[bool], Field(min_length=512, max_length=512)]

    @model_validator(mode="after")
    def consistent_grids(self):
        sizes = {p.absolute_lut.size for p in self.prototypes}
        if len(sizes) != 1 or (65 in sizes and len(self.prototypes) > 8):
            raise ValueError("Prototype grids inconsistent or memory limit exceeded")
        if self.k > len(self.prototypes):
            raise ValueError("k exceeds number of prototypes")
        ids = [p.id for p in self.prototypes]
        if len(ids) != len(set(ids)):
            raise ValueError("Duplicate prototype id")
        return self


class MetricBlock(Contract):
    median_delta_e: Annotated[float, Field(ge=0)]
    p95_delta_e: Annotated[float, Field(ge=0)]
    ssim: Annotated[float, Field(ge=-1, le=1)]
    sample_count: Annotated[int, Field(ge=1)]


class Provenance(Contract):
    pair_count: Annotated[int, Field(ge=0)]
    session_count: Annotated[int, Field(ge=0)]
    seed: UInt32
    manifest_sha256: SHA256 | None
    runtime_lock_id: SHA256 | None


class Quality(Contract):
    status: Literal["example", "measured", "needs_review"]
    reconstruction: MetricBlock | None
    validation: MetricBlock | None
    beauty_validation: MetricBlock | None = None
    geometry_validation: MetricBlock | None = None
    transfer_confidence: Literal["unvalidated", "experimental", "validated"]
    warnings: list[str]

    @model_validator(mode="after")
    def measured_validation(self):
        if self.transfer_confidence == "validated" and self.validation is None:
            raise ValueError("Validated transfer requires measured validation")
        return self


class StyleV2(Contract):
    schema_version: Literal["2.0.0"]
    renderer_version: Literal["photo_renderer_v2"]
    style_id: Annotated[str, Field(min_length=1, max_length=120)]
    mode: Literal["fixed", "adaptive"]
    strength: Unit
    color: ColorSpace
    raw_recipe: Literal["raw_camera_wb_linear_srgb_ahd_v1"]
    models: list[ModelReference]
    global_: GlobalColor = Field(alias="global")
    local: LocalModule
    adaptation: Adaptation | None
    beauty: Annotated[Disabled | Beauty, Field(discriminator="enabled")]
    defects: Annotated[Disabled | Defects, Field(discriminator="enabled")]
    geometry: Annotated[Disabled | Geometry, Field(discriminator="enabled")]
    extensions: Extensions
    provenance: Provenance
    quality: Quality

    @model_validator(mode="after")
    def dependencies(self):
        if (self.mode == "adaptive") != (self.adaptation is not None):
            raise ValueError("mode/adaptation mismatch")
        ids = {m.id for m in self.models}
        if len(ids) != len(self.models):
            raise ValueError("Duplicate model IDs")
        required = set()
        if self.mode == "adaptive" or self.local.enabled:
            required.add("lraspp_mobilenet_v3_large")
        if self.beauty.enabled or self.defects.enabled or self.geometry.enabled:
            required.update(["sfd", "fan2d"])
        if self.geometry.enabled:
            required.update(["keypointrcnn_resnet50_fpn", "lraspp_mobilenet_v3_large"])
        ext = self.extensions
        if ext.masking:
            required.update([ext.masking.scene, ext.masking.face, ext.masking.interactive])
        if ext.inpainting and ext.inpainting.enabled:
            required.add("big_lama")
        if ext.generation and ext.generation.enabled:
            required.add("sd15_inpaint")
        if ext.restoration:
            if ext.restoration.denoise != "off":
                required.add(ext.restoration.denoise)
            if ext.restoration.upscale != "off":
                required.add({"2x": "realesrgan_x2", "4x": "realesrgan_x4"}[ext.restoration.upscale])
        if ext.background and ext.background.mode == "generate" and not (ext.generation and ext.generation.enabled):
            raise ValueError("Generated background needs enabled generation module")
        if not required <= ids:
            raise ValueError(f"Missing model dependencies: {sorted(required-ids)}")
        return self

    def canonical(self) -> bytes:
        return canonical_bytes(self)

    @property
    def config_hash(self) -> str:
        return canonical_hash(self)

    @classmethod
    def from_bytes(cls, raw: bytes, *, allow_example: bool = False) -> "StyleV2":
        result = cls.model_validate(parse_json(raw, max_bytes=100_000_000))
        if result.quality.status == "example" and not allow_example:
            raise ValueError("Example profile is not permitted for production apply")
        return result
