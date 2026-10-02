"""Strict, versioned public configuration. No silently ignored settings."""
import json
import os
from pathlib import Path
from typing import Annotated, Literal

import yaml
from pydantic import BaseModel, ConfigDict, Field, model_validator

Unit = Annotated[float, Field(ge=0, le=1)]
EV = Annotated[float, Field(ge=-3, le=3)]
Rect = tuple[Unit, Unit, Unit, Unit]


class Model(BaseModel):
    model_config = ConfigDict(extra="forbid", allow_inf_nan=False)


class Input(Model):
    recursive: bool = True
    formats: list[Literal['jpg', 'jpeg', 'png', 'tif', 'tiff']] = ['jpg', 'jpeg', 'tif', 'tiff']
    missing_icc: Literal['assume_srgb', 'reject'] = 'assume_srgb'


class Runtime(Model):
    analysis_long_edge: int = Field(1600, ge=640, le=2400)
    tile_size: int = Field(768, ge=128, le=2048)
    threads: int = Field(4, ge=1, le=16)
    max_megapixels: int = Field(80, ge=1, le=200)


class Color(Model):
    enabled: bool = True
    exposure_ev: EV = 0
    wb_gains: tuple[Annotated[float, Field(ge=.5, le=2)], ...] = (1., 1., 1.)
    contrast: float = Field(.05, ge=-.5, le=.5)
    saturation: float = Field(.02, ge=-1, le=1)
    gamma: float = Field(1., ge=.5, le=2)
    @model_validator(mode='after')
    def channels(self):
        if len(self.wb_gains) != 3:
            raise ValueError('wb_gains must have three RGB channels')
        return self


class Collection(Model):
    enabled: bool = True
    mode: Literal['median', 'target'] = 'median'
    target_face_luma: float = Field(.52, ge=.2, le=.7)  # encoded sRGB luma
    strength: Unit = .75
    max_correction_ev: float = Field(2.2, ge=0, le=3)
    expected_faces: dict[str, int] = {}


class Effect(Model):
    enabled: bool = True
    strength: Unit = .25


class Light(Effect):
    strength: Unit = .75
    max_local_ev: float = Field(1.0, ge=0, le=1.5)


class Skin(Model):
    enabled: bool = True
    face_strength: Unit = .4
    body_strength: Unit = .2
    texture_retention: Unit = .85


class HighlightCleanup(Model):
    enabled: bool = True
    strength: Unit = .35
    threshold: Unit = .66
    local_contrast: float = Field(.035, ge=.005, le=.15)
    max_reduction: float = Field(.12, ge=0, le=.25)


class FaceColorFinish(Model):
    enabled: bool = True
    strength: Unit = .40
    saturation: float = Field(.10, ge=0, le=.35)
    warmth: float = Field(.012, ge=-.03, le=.05)
    max_chroma_shift: float = Field(.035, ge=0, le=.08)


class PostWhiteBalance(Model):
    enabled: bool = True
    strength: Unit = .45
    max_gain: float = Field(.12, ge=0, le=.30)
    neutral_saturation: float = Field(.20, ge=.05, le=.45)
    highlight_start: float = Field(.82, ge=.65, le=.95)
    highlight_headroom: float = Field(.975, ge=.90, le=.995)


class Blemishes(Model):
    enabled: bool = False
    auto_redness: bool = True
    auto_strength: Unit = .35
    # No automatic mole/freckle removal. Normalized circles x,y,radius relative to width.
    circles: list[tuple[Unit, Unit, Annotated[float, Field(gt=0, le=.02)]]] = []


class Beauty(Model):
    enabled: bool = True
    preserve_geometry: bool = True
    skin: Skin = Field(default_factory=Skin)
    even_light: Light = Field(default_factory=Light)
    specular: Effect = Field(default_factory=Effect)
    highlight_cleanup: HighlightCleanup = Field(default_factory=HighlightCleanup)
    face_color_finish: FaceColorFinish = Field(default_factory=FaceColorFinish)
    post_white_balance: PostWhiteBalance = Field(default_factory=PostWhiteBalance)
    redness: Effect = Field(default_factory=lambda: Effect(strength=.2))
    contour_light: Effect = Field(default_factory=lambda: Effect(enabled=False, strength=.15))
    blemishes: Blemishes = Field(default_factory=Blemishes)


class Geometry(Model):
    enabled: bool = False
    face_strength: Unit = .25
    body_strength: Unit = .2
    max_face_displacement: float = Field(.015, ge=0, le=.02)
    max_body_displacement: float = Field(.02, ge=0, le=.03)
    body_regions: list[Rect] = []  # explicit normalized visible torso boxes
    @model_validator(mode='after')
    def rectangles(self):
        for x0, y0, x1, y1 in self.body_regions:
            if x1 <= x0 or y1 <= y0:
                raise ValueError('body_regions require x1>x0 and y1>y0')
        return self


class Detail(Model):
    sharpening: Effect = Field(default_factory=lambda: Effect(strength=.25))
    blur_detection: 'BlurDetection' = Field(default_factory=lambda: BlurDetection())
    face_recovery: 'FaceRecovery' = Field(default_factory=lambda: FaceRecovery())


class BlurDetection(Model):
    enabled: bool = True
    # Variance of a normalized 512 px face crop Laplacian.
    threshold: float = Field(.002, ge=0, le=.1)
    restoration_model: Literal['codeformer'] = 'codeformer'
    fidelity: float = Field(.9, ge=0, le=1)
    min_identity_similarity: float = Field(.75, ge=0, le=1)
    auto_restore: bool = False
    fidelity_candidates: list[Annotated[float, Field(ge=0, le=1)]] = [.85, .92, .97]
    blend: Unit = .65

    @model_validator(mode='after')
    def candidates(self):
        if not self.fidelity_candidates:
            raise ValueError('fidelity_candidates must not be empty')
        if len(self.fidelity_candidates) > 5:
            raise ValueError('fidelity_candidates supports at most five values')
        return self


class FaceRecovery(Model):
    enabled: bool = True
    strength: Unit = .55
    # Never amplify a high-pass component beyond this encoded-sRGB amount.
    max_detail_gain: float = Field(.045, ge=0, le=.1)


class Export(Model):
    master: bool = True
    delivery: bool = True
    jpeg_quality: int = Field(95, ge=80, le=100)
    preview_long_edge: int = Field(1600, ge=320, le=2400)
    @model_validator(mode='after')
    def nonempty(self):
        if not self.master and not self.delivery:
            raise ValueError('enable at least one full-resolution output')
        return self


class Config(Model):
    schema_version: Literal[1] = 1
    profile: str = 'natural_series'
    input: Input = Field(default_factory=Input)
    runtime: Runtime = Field(default_factory=Runtime)
    color: Color = Field(default_factory=Color)
    collection: Collection = Field(default_factory=Collection)
    beauty: Beauty = Field(default_factory=Beauty)
    geometry: Geometry = Field(default_factory=Geometry)
    detail: Detail = Field(default_factory=Detail)
    export: Export = Field(default_factory=Export)
    image_overrides: dict[str, dict] = {}
    @model_validator(mode='after')
    def geometry_contract(self):
        if self.geometry.enabled and self.beauty.preserve_geometry:
            raise ValueError('geometry.enabled conflicts with beauty.preserve_geometry')
        return self


def merge(a, b):
    out = dict(a)
    for k, v in b.items():
        out[k] = merge(out[k], v) if isinstance(v, dict) and isinstance(out.get(k), dict) else v
    return out


def for_image(config: Config, relative: str) -> Config:
    patch = config.image_overrides.get(relative, {})
    if set(patch) - {'color', 'beauty', 'geometry', 'detail'}:
        raise ValueError(f'{relative}: overrides may only change color/beauty/geometry/detail')
    return Config.model_validate(merge(config.model_dump(), patch))


def load(path: Path) -> Config:
    text = path.read_text()
    if len(text) > 1_000_000:
        raise ValueError('configuration exceeds 1 MB')
    if path.suffix.lower() == '.json':
        raw = json.loads(text)
    else:
        # Alias expansion can be recursive or unexpectedly large; no aliases in configs.
        if any(isinstance(t, (yaml.tokens.AliasToken, yaml.tokens.AnchorToken)) for t in yaml.scan(text)):
            raise ValueError('YAML anchors/aliases are not supported')
        class UniqueLoader(yaml.SafeLoader):
            pass
        def mapping(loader, node, deep=False):
            result = {}
            for key, val in node.value:
                k = loader.construct_object(key, deep=deep)
                if k in result:
                    raise ValueError(f'duplicate YAML key: {k}')
                result[k] = loader.construct_object(val, deep=deep)
            return result
        UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, mapping)
        raw = yaml.load(text, Loader=UniqueLoader)
    config = Config.model_validate(raw)
    for name in config.image_overrides:
        for_image(config, name)
    return config


def env_paths():
    keys = ['PHOTO_INPUT_DIR', 'PHOTO_OUTPUT_DIR', 'PHOTO_CONFIG_FILE']
    missing = [k for k in keys if not os.environ.get(k)]
    if missing:
        raise ValueError('Missing environment variables: ' + ', '.join(missing))
    source, dest, cfg = [Path(os.environ[k]).expanduser().resolve() for k in keys]
    if source == dest or source in dest.parents or dest in source.parents:
        raise ValueError('Input and output must be disjoint directories')
    if not source.is_dir() or not cfg.is_file():
        raise ValueError('Input directory/config file does not exist')
    return source, dest, cfg
