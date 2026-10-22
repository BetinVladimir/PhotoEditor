from __future__ import annotations
from dataclasses import dataclass
from contextlib import contextmanager
from threading import RLock
import os
from pathlib import Path
from uuid import uuid4
from typing import Callable

import numpy as np

from photo_worker.color.icc import ICCRegistry,LittleCMS
from .inspect import open_raster,read_metadata
from .verify import AssetError,MAX_PIXELS,VerifiedSource,verify_size
from photo_worker.contracts.assets import RAWDevelopment

@dataclass(frozen=True)
class WorkingImage:
    rgb_path: Path
    alpha_path: Path | None
    width: int
    height: int
    profile_id: str
    profile_sha256: str
    source_sha256: str
    orientation_applied: bool


def orient_once(image):
    # libvips autorot removes orientation metadata after applying it.
    return image.autorot()


def raw_decoder_version(rawpy) -> str:
    return f"rawpy-{rawpy.__version__}/libraw-{'.'.join(str(part) for part in rawpy.libraw_version)}"


def validate_raw_rgb_layout(rgb: object) -> np.ndarray:
    """Require a stable contiguous RGB16 layout before tiled conversion."""
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.dtype('=u2') or
            rgb.ndim != 3 or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1 or
            not rgb.flags.c_contiguous or not rgb.flags.aligned):
        raise AssetError('RAW_DECODE_CONTRACT_ERROR')
    if rgb.shape[0] * rgb.shape[1] > MAX_PIXELS:
        raise AssetError('PIXEL_LIMIT_EXCEEDED')
    return rgb


# LibRaw X-Trans reconstruction can produce different native RGB16 samples
# with multiple OpenMP threads. Serialize RAW decode scopes within this process;
# one heavy task already runs per worker process. OpenMP's per-calling-thread
# setting works for background callers too; worker_thread_budget is main-only.
class RAWThreadControlError(RuntimeError):
    """Installed native runtime cannot enforce deterministic RAW execution."""


_RAW_THREAD_LOCK = RLock()


def _raw_openmp_pools(controller):
    pools = [pool for pool in controller.info() if pool.get('user_api') == 'openmp']
    if any(not isinstance(pool.get('filepath'), str) or not pool['filepath']
           or type(pool.get('num_threads')) is not int
           or not 1 <= pool['num_threads'] <= 2147483647 for pool in pools):
        raise RAWThreadControlError('RAW_THREAD_CONTROL_UNAVAILABLE')
    return pools


@contextmanager
def deterministic_raw_threads():
    """Enforce serial LibRaw without imposing a main-thread-only API.

    Existing OpenMP pools recover their exact observed counts. A pool first
    loaded by rawpy has no prior native state: after decode it adopts the outer
    scalar OMP_NUM_THREADS if valid, otherwise stays conservatively serial.
    The environment is always restored verbatim, including unset/auto/list forms.
    """
    from threadpoolctl import ThreadpoolController
    with _RAW_THREAD_LOCK:
        previous = os.environ.get('OMP_NUM_THREADS')
        existing = {p['filepath']: p['num_threads']
                    for p in _raw_openmp_pools(ThreadpoolController())}
        outer = (int(previous) if previous is not None and previous.isascii()
                 and previous.isdecimal() and len(previous) <= 10 else 1)
        if not 1 <= outer <= 2147483647:
            outer = 1
        os.environ['OMP_NUM_THREADS'] = '1'
        try:
            import rawpy
            flags = getattr(rawpy, 'flags', None)
            if not isinstance(flags, dict) or type(flags.get('OPENMP')) is not bool:
                raise RAWThreadControlError('RAW_THREAD_CONTROL_UNAVAILABLE')
            # Refresh after import: a pre-import controller cannot control a
            # newly loaded private LibRaw OpenMP library.
            controller = ThreadpoolController()
            with controller.limit(limits=1, user_api='openmp'):
                pools = _raw_openmp_pools(controller)
                if flags['OPENMP'] and not pools:
                    raise RAWThreadControlError('RAW_THREAD_CONTROL_UNAVAILABLE')
                if any(pool['num_threads'] != 1 for pool in pools):
                    raise RAWThreadControlError('RAW_THREAD_CONTROL_NOT_APPLIED')
                yield rawpy
        finally:
            try:
                # Refresh even after partial import/native exceptions. The
                # explicit restoration also covers a limiter entry failure.
                restore = ThreadpoolController()
                for pool in _raw_openmp_pools(restore):
                    expected = existing.get(pool['filepath'], outer)
                    selected = restore.select(filepath=pool['filepath'])
                    # Deliberately apply, not a temporary with-scope: this is
                    # the outer state that must remain after returning.
                    selected.limit(limits=expected, user_api='openmp')
                    if any(p['num_threads'] != expected for p in _raw_openmp_pools(selected)):
                        raise RAWThreadControlError('RAW_THREAD_RESTORE_NOT_APPLIED')
            finally:
                if previous is None:
                    os.environ.pop('OMP_NUM_THREADS', None)
                else:
                    os.environ['OMP_NUM_THREADS'] = previous


def decode_raw(source: VerifiedSource, development: RAWDevelopment | None = None, *,
               check_active: Callable[[],None] = lambda:None) -> np.ndarray:
    check_active()
    with deterministic_raw_threads() as rawpy:
        options = dict(use_camera_wb=True,use_auto_wb=False,no_auto_bright=True,
                       gamma=(1,1),output_bps=16,output_color=rawpy.ColorSpace.sRGB,
                       demosaic_algorithm=rawpy.DemosaicAlgorithm.AHD)
        if development is not None:
            # model_copy/model_construct may bypass Pydantic validation; reparse
            # owned fields before mapping options into the native decoder.
            development = RAWDevelopment.model_validate(development.model_dump() if isinstance(development,RAWDevelopment) else development)
            if development.decoder_version != raw_decoder_version(rawpy):
                raise AssetError('RAW_DECODER_VERSION_MISMATCH')
            options.update(use_camera_wb=development.wb_source=='camera',
                           use_auto_wb=development.wb_source=='auto',
                           demosaic_algorithm={'ahd':rawpy.DemosaicAlgorithm.AHD,
                                               'dcb':rawpy.DemosaicAlgorithm.DCB}[development.demosaic],
                           exp_shift=2.0**development.exposure_ev,exp_preserve_highlights=0.0,
                           highlight_mode={'clip':rawpy.HighlightMode.Clip,
                                           'blend':rawpy.HighlightMode.Blend,
                                           'reconstruct':rawpy.HighlightMode.ReconstructDefault}[development.highlight_policy])
        try:
            check_active()
            with rawpy.imread(str(source.path)) as raw:
                check_active()
                dimensions=(raw.sizes.raw_width,raw.sizes.raw_height)
                if any(isinstance(value,(bool,np.bool_)) or not isinstance(value,(int,np.integer)) or value<1 for value in dimensions):
                    raise AssetError('RAW_DECODE_CONTRACT_ERROR')
                if int(dimensions[0])*int(dimensions[1]) > MAX_PIXELS:
                    raise AssetError('PIXEL_LIMIT_EXCEEDED')
                if development is not None and development.wb_source == 'camera':
                    wb = raw.camera_whitebalance
                    if (not isinstance(wb,(list,tuple,np.ndarray)) or np.shape(wb)!=(4,) or
                        any(not isinstance(value,(int,float,np.number)) or not np.isfinite(value) for value in wb) or
                        any(value <= 0 for value in wb[:3]) or wb[3] < 0):
                        raise AssetError('RAW_CAMERA_WB_UNAVAILABLE')
                check_active()
                rgb = raw.postprocess(**options)
                check_active()
                # LittleCMS below consumes RGB16. Never reinterpret an unexpected
                # native tensor's byte layout or publish an invalid working image.
                return validate_raw_rgb_layout(rgb)
        except rawpy.LibRawError as exc:
            raise AssetError('RAW_UNSUPPORTED') from exc


def icc_to_work(source: VerifiedSource, output: Path, registry: ICCRegistry,
                assume_profile: str | None = None, *, analyze: bool = False, tile: int = 512,
                alpha_output: Path | None = None, raw_development: RAWDevelopment | None = None, check_active: Callable[[],None] = lambda:None) -> WorkingImage:
    if tile not in (256,512,1024):
        raise AssetError('TILE_SIZE_INVALID')
    check_active()
    if raw_development is not None and source.format != 'raw':
        raise AssetError('RAW_DEVELOPMENT_ON_RASTER')
    metadata = read_metadata(source)
    if alpha_output is not None and (not metadata.has_alpha or alpha_output.parent!=output.parent or alpha_output==output):
        raise AssetError('ALPHA_OUTPUT_INVALID')
    if analyze and metadata.has_alpha:
        raise AssetError('ANALYZE_ALPHA_UNSUPPORTED')
    work = registry.resolve('work_rec2020_gamma24_v1')
    if source.format == 'raw':
        rgb = decode_raw(source, raw_development, check_active=check_active)
        check_active()
        height,width = rgb.shape[:2]
        original_profile = registry.resolve('linear_srgb_v1').data
        image = None
    else:
        if metadata.icc is None and assume_profile is None:
            raise AssetError('INPUT_PROFILE_REQUIRED')
        original_profile = metadata.icc or registry.resolve(assume_profile).data
        image = orient_once(open_raster(source.path))
        width,height = image.width,image.height
        rgb = None
    if os.path.lexists(output):
        raise AssetError('OUTPUT_ALREADY_EXISTS')
    output.parent.mkdir(parents=True,exist_ok=True)
    scratch = output.parent
    required = width*height*(12+(2 if metadata.has_alpha else 0))+1048576
    space = os.statvfs(scratch)
    if space.f_bavail*space.f_frsize < required:
        raise AssetError('SCRATCH_EXHAUSTED')
    temporary = output.with_name(output.name+'.'+uuid4().hex+'.partial.npy')
    alpha_output = (alpha_output or output.with_name(output.stem+'.alpha.npy')) if metadata.has_alpha else None
    alpha_temp = temporary.with_name(temporary.name+'.alpha') if metadata.has_alpha else None
    if alpha_output and os.path.lexists(alpha_output):
        raise AssetError('OUTPUT_ALREADY_EXISTS')
    engine = LittleCMS()
    target = alpha = None
    linked: list[Path] = []
    try:
        # Exclusive private temporary files; numpy subsequently opens only our files.
        for path in (temporary,alpha_temp):
            if path is not None:
                fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
                os.close(fd)
        target = np.lib.format.open_memmap(temporary,mode='w+',dtype=np.float32,shape=(height,width,3))
        if alpha_temp:
            alpha = np.lib.format.open_memmap(alpha_temp,mode='w+',dtype=np.uint16,shape=(height,width))
        for y in range(0,height,tile):
            for x in range(0,width,tile):
                check_active()
                h,w = min(tile,height-y),min(tile,width-x)
                if image is not None:
                    crop = image.crop(x,y,w,h)
                    pixels = np.frombuffer(crop.write_to_memory(),dtype=np.uint8 if crop.format=='uchar' else np.uint16).reshape(h,w,crop.bands)
                    if crop.format=='uchar':
                        pixels = pixels.astype(np.uint16)*257
                else:
                    pixels = rgb[y:y+h,x:x+w]
                target[y:y+h,x:x+w] = engine.convert(pixels[...,:3],original_profile,work.data).astype(np.float32)/65535
                if alpha is not None:
                    alpha[y:y+h,x:x+w] = pixels[...,3]
        target.flush()
        if alpha is not None:
            alpha.flush()
        verify_size(source.path,source.bytes,source.sha256,check_active=check_active)
        check_active()
        # link is an atomic no-overwrite publication inside the attempt scratch dir.
        os.link(temporary,output);linked.append(output)
        if alpha_temp and alpha_output:
            os.link(alpha_temp,alpha_output);linked.append(alpha_output)
        return WorkingImage(output,alpha_output,width,height,work.id,work.sha256,source.sha256,True)
    except Exception:
        for path in linked:
            path.unlink(missing_ok=True)
        raise
    finally:
        if target is not None:
            target._mmap.close()
        if alpha is not None:
            alpha._mmap.close()
        temporary.unlink(missing_ok=True)
        if alpha_temp:
            alpha_temp.unlink(missing_ok=True)
