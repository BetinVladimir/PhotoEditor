"""Owned native background composition, shared by operation and render stages."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.background import (alpha_u16, asset_geometry, contact_shadow,
    effective_alpha, render_asset_tile, solid, transparent)
from photo_worker.runtime.scratch import Reservation

@dataclass(frozen=True)
class PreparedBackground:
    rgb: Path
    alpha: Path | None
    reservation: Reservation

@contextmanager
def prepare_background(rgb, subject, source_alpha, background_rgb, params, scratch, *,
                       check_active=lambda:None, progress=lambda done:None, alpha_mode="uint16"):
    """Caller verifies source/masks/params authority and admits native resources."""
    check_active()
    if alpha_mode not in ("uint16","float32") or subject.ndim!=2:
        raise ValueError("BACKGROUND_ALPHA_MODE_INVALID")
    alpha_dtype=np.uint16 if alpha_mode=="uint16" else np.float32
    height,width=subject.shape
    tile=params.tile
    if (height*width>200000000 or rgb.shape!=(height,width,3) or rgb.dtype!=np.float32
            or subject.dtype!=np.float32 or source_alpha is not None and
            (source_alpha.shape!=(height,width) or source_alpha.dtype!=alpha_dtype)
            or (params.mode=='asset')!=(background_rgb is not None)):
        raise ValueError('BACKGROUND_SOURCE_INVALID')
    geometry=None
    if background_rgb is not None:
        if background_rgb.ndim!=3 or background_rgb.shape[2]!=3 or background_rgb.dtype!=np.float32:
            raise ValueError('BACKGROUND_ASSET_INVALID')
        geometry=asset_geometry(background_rgb.shape[1],background_rgb.shape[0],width,height,
            params.asset_scale,params.position_x,params.position_y)
    shadow_radius = int(np.ceil(params.shadow_blur_fraction * max(height, width)))
    shadow_dx = int(round(params.shadow_offset_x * width)); shadow_dy = int(round(params.shadow_offset_y * height))
    shadow_halo = shadow_radius + max(abs(shadow_dx), abs(shadow_dy)) if params.shadow_opacity else 0
    output_bytes = height * width * ((14 if alpha_mode=="uint16" else 16) if params.mode == "transparent" else 12) + 16_384
    with scratch.reserve(output_bytes) as reservation:
        rgb_path = reservation.path("background-rgb", ".npy")
        alpha_path = reservation.path("background-alpha", ".npy") if params.mode == "transparent" else None
        rgb_out = np.lib.format.open_memmap(rgb_path, mode="w+", dtype="<f4", shape=(height, width, 3))
        alpha_out = np.lib.format.open_memmap(alpha_path, mode="w+", dtype="<u2" if alpha_mode=="uint16" else "<f4", shape=(height, width)) if alpha_path else None
        try:
            done = 0
            for y in range(0, height, tile):
                for x in range(0, width, tile):
                    check_active()
                    sl = (slice(y, y + tile), slice(x, x + tile))
                    if alpha_mode=="uint16":
                        alpha = effective_alpha(subject[sl], None if source_alpha is None else source_alpha[sl])
                    else:
                        alpha = effective_alpha(subject[sl])
                        if source_alpha is not None:
                            coverage=source_alpha[sl]
                            if not np.isfinite(coverage).all() or np.any(coverage<0) or np.any(coverage>1):
                                raise ValueError("SOURCE_ALPHA_INVALID")
                            alpha=alpha*coverage
                    if params.mode in ("solid", "asset"):
                        backdrop = params.solid_rgb if params.mode == "solid" else render_asset_tile(
                            background_rgb, (height, width), (y, min(y + tile, height), x, min(x + tile, width)), geometry)
                        if params.shadow_opacity:
                            y0, y1 = max(0, y - shadow_halo), min(height, y + tile + shadow_halo)
                            x0, x1 = max(0, x - shadow_halo), min(width, x + tile + shadow_halo)
                            core = (slice(y - y0, min(y + tile, height) - y0), slice(x - x0, min(x + tile, width) - x0))
                            shade = contact_shadow(subject[y0:y1, x0:x1], shadow_radius, shadow_dx, shadow_dy,
                                                   params.shadow_opacity, core)
                            backdrop = np.power(np.power(np.asarray(backdrop, np.float32), np.float32(2.4)) *
                                                  (1 - shade[..., None]), np.float32(1 / 2.4), dtype=np.float32)
                        rgb_out[sl] = solid(rgb[sl], alpha, backdrop)
                    else:
                        rgb_out[sl] = transparent(rgb[sl], alpha)
                        alpha_out[sl] = alpha_u16(alpha) if alpha_mode=="uint16" else alpha
                    done += 1
                    progress(done)
            rgb_out.flush()
            if alpha_out is not None:
                alpha_out.flush()
            reservation.validate()
        finally:
            rgb_out._mmap.close()
            if alpha_out is not None:
                alpha_out._mmap.close()
        check_active()
        yield PreparedBackground(rgb_path,alpha_path,reservation)
        check_active()
