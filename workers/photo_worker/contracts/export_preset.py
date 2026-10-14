"""Frozen exportpreset.Config wire shape; existing encoder size/options reused."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import Contract, Unit, canonical_bytes, parse_json
from photo_worker.contracts.export import ExportSize, validate_format_options


class PresetWatermark(Contract):
    asset_id: UUID
    opacity: Unit
    anchor: Literal['top_left', 'top_right', 'bottom_left', 'bottom_right', 'center']
    margin_x: Annotated[int, Field(ge=0, le=9007199254740991)]
    margin_y: Annotated[int, Field(ge=0, le=9007199254740991)]

    @model_validator(mode='after')
    def nonzero_asset(self):
        if self.asset_id.int == 0:
            raise ValueError('PRESET_WATERMARK_INVALID')
        return self


class PresetSharpen(Contract):
    radius: Annotated[float, Field(ge=.1, le=5)]
    strength: Unit
    threshold: Annotated[float, Field(ge=0, le=.1)]


class FrozenExportPresetConfig(Contract):
    format: Literal['jpeg', 'png', 'tiff']
    icc_id: Literal['srgb_v1', 'work_rec2020_gamma24_v1']
    size: ExportSize | None = None
    quality: Annotated[int, Field(ge=1, le=100)] | None = None
    subsampling: Literal['444']
    alpha: Literal['preserve', 'matte']
    matte_rgb: Annotated[list[Unit], Field(min_length=3, max_length=3)] | None = None
    metadata_policy: Literal['strip', 'copyright', 'approved_exif']
    watermark: PresetWatermark | None = None
    sharpen: PresetSharpen | None = None
    naming_template: str

    @model_validator(mode='after')
    def options(self):
        validate_format_options(self.format, self.quality, self.alpha, self.matte_rgb)
        if not self.naming_template.strip() or len(self.naming_template.encode('utf-8')) > 200:
            raise ValueError('PRESET_NAMING_INVALID')
        return self


def validate_frozen_export_preset(raw: bytes) -> None:
    """Reject normalization/default insertion: preserve exact Go canonical bytes.

    This validates settings only. Watermark ACL/closure and ICC bytes still need
    authoritative admission; model validation never grants access to them.
    """
    value = parse_json(raw, max_bytes=1_048_576)
    config = FrozenExportPresetConfig.model_validate_json(raw)
    if canonical_bytes(value) != raw or canonical_bytes(config.model_dump(mode='json', exclude_none=True)) != raw:
        raise ValueError('PRESET_NONCANONICAL')
