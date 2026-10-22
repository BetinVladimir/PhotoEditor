"""Pinned ICC presentation for a region extracted after complete rendering."""
from collections.abc import Callable
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path

import numpy as np
from PIL import Image

from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.media.derivative import _write
from .contracts import RegionPreviewParams,RegionProjection
from .pixels import extract_rendered_region


@dataclass(frozen=True)
class RegionPresentation:
    rgb: np.ndarray
    alpha: np.ndarray | None
    profile: bytes
    profile_sha256: str
    cache_key: str
    projection: RegionProjection | None = None

    @property
    def media_type(self) -> str:
        return 'image/webp' if self.alpha is not None else 'image/jpeg'


def prepare_region_presentation(params: RegionPreviewParams, source_body: bytes,
                                rendered_rgb: np.ndarray,
                                rendered_alpha: np.ndarray | None,
                                icc_root: Path, *,
                                runtime_icc_lock_sha: str,
                                target_canvas=None,target_chain=None,load_inverse_field=None,
                                check_active: Callable[[], None] = lambda: None,
                                ) -> RegionPresentation:
    """Check the qualified runtime pin without guessing its ICC-lock semantics."""
    check_active()
    lock = (icc_root / 'profiles.lock.json').read_bytes()
    if runtime_icc_lock_sha != params.frozen_recipe.icc_lock_sha:
        raise ValueError('REGION_ICC_LOCK_MISMATCH')
    # Re-read installed profiles per presentation; a cached registry cannot hide
    # replaced files. The surrounding qualified factory must verify LCMS bytes.
    registry = ICCRegistry(icc_root)
    image = params.validate_source(source_body)
    source = registry.resolve(image.color.profile_id)
    target = registry.resolve('srgb_v1')
    if (icc_root / 'profiles.lock.json').read_bytes() != lock:
        raise ValueError('REGION_ICC_LOCK_MISMATCH')
    if source.sha256 != image.color.icc_sha256:
        raise ValueError('REGION_ICC_SOURCE_MISMATCH')
    projection=None
    if target_canvas is None and target_chain is None:
        rgb, alpha = extract_rendered_region(params, source_body, rendered_rgb,
            rendered_alpha, check_active=check_active)
    else:
        if params.execution is None or target_canvas is None or target_chain is None or not callable(load_inverse_field):
            raise ValueError('REGION_PROJECTION_PROOF_REQUIRED')
        from .pixels import extract_projected_rendered_region
        rgb,alpha,geometry=extract_projected_rendered_region(params,source_body,rendered_rgb,rendered_alpha,
            target_canvas=target_canvas,target_chain=target_chain,load_inverse_field=load_inverse_field,check_active=check_active)
        projection=RegionProjection(execution_sha256=canonical_hash(params.execution),target_canvas=target_canvas,
            target_transform_chain=target_chain,geometry=geometry)
    check_active()
    rgb16 = np.round(rgb * 65535).astype(np.uint16)
    converted = LittleCMS().convert(rgb16, source.data, target.data)
    check_active()
    output = converted.astype(np.float32) / 65535
    key = region_cache_key(params, target.sha256, alpha is not None,projection=projection)
    return RegionPresentation(output, alpha, target.data, target.sha256, key,projection)


def write_region_presentation(path: Path, presentation: RegionPresentation, *,
                              check_active: Callable[[], None] = lambda: None) -> int:
    """Use existing derivative encoder and verify dimensions, alpha and ICC."""
    check_active()
    suffix = '.webp' if presentation.alpha is not None else '.jpg'
    if path.suffix != suffix:
        raise ValueError('REGION_OUTPUT_SUFFIX_INVALID')
    _write(path, presentation.rgb, presentation.alpha, presentation.profile, 88, strip_metadata=True)
    check_active()
    with Image.open(path) as encoded:
        if (encoded.size != (presentation.rgb.shape[1], presentation.rgb.shape[0])
                or encoded.mode != ('RGBA' if presentation.alpha is not None else 'RGB')
                or sha256(encoded.info.get('icc_profile', b'')).hexdigest() != presentation.profile_sha256
                or encoded.info.get('exif') or encoded.info.get('xmp')):
            raise ValueError('REGION_OUTPUT_INVALID')
        encoded.verify()
    check_active()
    return path.stat().st_size


def region_cache_key(params: RegionPreviewParams, target_sha256: str, alpha: bool, *,projection=None) -> str:
    value=dict(schema_version=1, params=canonical_hash(params),
        target_icc_sha256=target_sha256, quality=88,
        format='webp' if alpha else 'jpeg', metadata_policy='stripped',
        resize='area_linear_premult_v1')
    if projection is not None:value['projection']=projection.model_dump(mode='json')
    return canonical_hash(value)
