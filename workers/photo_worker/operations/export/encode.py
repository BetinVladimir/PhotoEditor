"""Color-managed deterministic raster encoding from a validated WorkingImage."""
from __future__ import annotations

from contextlib import ExitStack
import hashlib

import cv2
import numpy as np
import pyvips
import tifffile
from PIL import Image
from pydantic import ValidationError

from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodedImage, EncodeParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _dimensions(width: int, height: int, params: EncodeParams) -> tuple[int, int]:
    if params.size is None:
        return width, height
    size = params.size
    if size.long_edge is not None:
        scale = size.long_edge / max(width, height)
    else:
        scale = min(size.width / width, size.height / height)
    if not size.allow_upscale:
        scale = min(1.0, scale)
    return max(1, round(width * scale)), max(1, round(height * scale))


def _resize(array: np.ndarray, width: int, height: int) -> np.ndarray:
    if (array.shape[1], array.shape[0]) == (width, height):
        return np.asarray(array)
    interpolation = cv2.INTER_AREA if width < array.shape[1] or height < array.shape[0] else cv2.INTER_LANCZOS4
    result = cv2.resize(array, (width, height), interpolation=interpolation)
    if array.ndim == 3 and result.ndim == 2:
        result = result[:, :, None]
    return np.clip(result, 0, 1)


def _resize_rgba(rgb,alpha,width,height,*,encoding='gamma24'):
    """Preserve existing opaque resize; translucent geometry uses linear premult."""
    if alpha is None:return _resize(rgb,width,height),None
    if rgb.shape[:2]!=alpha.shape or alpha.dtype!=np.float32 or not np.isfinite(alpha).all() or np.any((alpha<0)|(alpha>1)):
        raise ValueError('RESIZE_ALPHA_INVALID')
    if (rgb.shape[1],rgb.shape[0])==(width,height):return np.asarray(rgb),np.asarray(alpha)
    if np.all(alpha==1):return _resize(rgb,width,height),_resize(alpha,width,height)
    source=np.clip(rgb,0,1).astype(np.float32,copy=False)
    if encoding=='linear':linear=source
    elif encoding=='gamma24':linear=np.copysign(np.abs(source)**2.4,source)
    elif encoding=='srgb':
        absolute=np.abs(source)
        linear=np.copysign(np.where(absolute<=.04045,absolute/12.92,((absolute+.055)/1.055)**2.4),source)
    else:raise ValueError('RESIZE_COLOR_INVALID')
    interpolation=cv2.INTER_AREA if width<rgb.shape[1] or height<rgb.shape[0] else cv2.INTER_LANCZOS4
    premult=cv2.resize(linear*alpha[...,None],(width,height),interpolation=interpolation)
    opacity=np.clip(cv2.resize(alpha,(width,height),interpolation=interpolation),0,1).astype(np.float32)
    output=np.zeros_like(premult)
    np.divide(premult,opacity[...,None],out=output,where=opacity[...,None]>1e-6)
    absolute=np.abs(output)
    if encoding=='linear':encoded=output
    elif encoding=='gamma24':encoded=np.copysign(absolute**(1/2.4),output)
    else:encoded=np.copysign(np.where(absolute<=.0031308,absolute*12.92,1.055*absolute**(1/2.4)-.055),output)
    return np.clip(encoded,0,1).astype(np.float32),opacity


def _resize_working_image(rgb, alpha, width, height, image: WorkingImage):
    return _resize_rgba(rgb, alpha, width, height, encoding=image.color.encoding)


def _composite_matte(rgb: np.ndarray, alpha: np.ndarray,
                     matte_rgb: list[float], icc_id: str) -> np.ndarray:
    """Composite straight-alpha colors in the target profile's linear light."""
    if rgb.shape[:2] != alpha.shape or rgb.shape[2] != 3:
        raise ValueError('MATTE_SHAPE_INVALID')
    if not np.isfinite(rgb).all() or not np.isfinite(alpha).all() or np.any((alpha < 0) | (alpha > 1)):
        raise ValueError('MATTE_PIXELS_INVALID')
    matte = np.asarray(matte_rgb, dtype=np.float32)
    if matte.shape != (3,) or not np.isfinite(matte).all() or np.any((matte < 0) | (matte > 1)):
        raise ValueError('MATTE_COLOR_INVALID')
    if np.all(alpha == 1):
        return rgb
    if icc_id == 'srgb_v1':
        decode = lambda value: np.where(value <= .04045, value / 12.92,
                                         ((value + .055) / 1.055) ** 2.4)
        encode = lambda value: np.where(value <= .0031308, value * 12.92,
                                         1.055 * value ** (1 / 2.4) - .055)
    elif icc_id == 'work_rec2020_gamma24_v1':
        decode = lambda value: value ** 2.4
        encode = lambda value: value ** (1 / 2.4)
    else:
        raise ValueError('MATTE_ICC_INVALID')
    linear = decode(np.clip(rgb, 0, 1)) * alpha[:, :, None]
    linear += decode(matte)[None, None, :] * (1 - alpha[:, :, None])
    output = np.clip(encode(np.clip(linear, 0, 1)), 0, 1).astype(np.float32)
    # Mixed transparency must not gamma-roundtrip fully retained foreground
    # or the exact matte. Fractional opacity keeps the linear-light blend.
    np.copyto(output, np.clip(rgb, 0, 1), where=(alpha == 1)[:, :, None])
    np.copyto(output, matte[None, None, :], where=(alpha == 0)[:, :, None])
    return output


def _write(path, pixels: np.ndarray, profile: bytes, params: EncodeParams):
    height, width, bands = pixels.shape
    rgb16 = np.round(np.clip(pixels[:, :, :3], 0, 1) * 65535).astype('<u2')
    alpha16 = np.round(np.clip(pixels[:, :, 3], 0, 1) * 65535).astype('<u2') if bands == 4 else None
    if params.format == 'tiff':
        value = rgb16 if alpha16 is None else np.dstack((rgb16, alpha16))
        tifffile.imwrite(path, value, photometric='rgb', compression='deflate', predictor=True, metadata=None, software=False,
            extratags=[(34675, 'B', len(profile), profile, False)])
    else:
        value = rgb16 if alpha16 is None else np.dstack((rgb16, alpha16))
        image = pyvips.Image.new_from_memory(value.data, width, height, value.shape[2], 'ushort').copy(interpretation='rgb16')
        image.set_type(pyvips.GValue.blob_type, 'icc-profile-data', profile)
        if params.format == 'png':
            image.pngsave(str(path), compression=6, bitdepth=16, keep='icc')
        else:
            image.jpegsave(str(path), Q=params.quality, subsample_mode='off', optimize_coding=True, keep='icc')


def _verify_png_layout(path, width, height, alpha):
    with open(path, 'rb') as stream:
        header = stream.read(33)
    if (len(header) != 33 or header[:8] != b'\x89PNG\r\n\x1a\n' or
            int.from_bytes(header[8:12], 'big') != 13 or header[12:16] != b'IHDR' or
            int.from_bytes(header[16:20], 'big') != width or
            int.from_bytes(header[20:24], 'big') != height or
            header[24] != 16 or header[25] != (6 if alpha else 2) or
            header[26] != 0 or header[27] != 0 or header[28] not in (0, 1)):
        raise ValueError('PNG_LAYOUT_MISMATCH')


def _verify_lossless_pixels(decoded: np.ndarray, pixels: np.ndarray,
                            channel_order: tuple[int, ...], check_active) -> None:
    if (decoded.dtype != np.uint16 or
            decoded.shape != pixels.shape or pixels.ndim != 3 or
            len(channel_order) != pixels.shape[2]):
        raise ValueError('ENCODE_PIXEL_MISMATCH')
    height, width = pixels.shape[:2]
    rows = max(1, min(256, 1_048_576 // width))
    for start in range(0, height, rows):
        check_active()
        stop = min(height, start + rows)
        expected = np.round(np.clip(pixels[start:stop], 0, 1) * 65535).astype(np.uint16)
        for channel, observed in enumerate(channel_order):
            if not np.array_equal(decoded[start:stop, :, observed],
                                  expected[:, :, channel]):
                raise ValueError('ENCODE_PIXEL_MISMATCH')


def _verify(path, params: EncodeParams, width: int, height: int, alpha: bool,
            *, expected_pixels: np.ndarray | None = None,
            check_active=lambda: None):
    if params.format == 'tiff':
        with tifffile.TiffFile(path) as document:
            if len(document.pages) != 1:
                raise ValueError('TIFF_VERIFY_FAILED')
            page = document.pages[0]
            if page.shape != (height, width, 4 if alpha else 3) or page.dtype != np.uint16 or 34675 not in page.tags:
                raise ValueError('TIFF_VERIFY_FAILED')
            if page.subifds or int(page.photometric) != 2:
                raise ValueError('TIFF_VERIFY_FAILED')
            if alpha and tuple(int(value) for value in page.extrasamples) != (2,):
                raise ValueError('TIFF_ALPHA_MISMATCH')
            profile = bytes(page.tags[34675].value)
            if hashlib.sha256(profile).hexdigest() != params.icc_sha256:
                raise ValueError('ENCODE_ICC_MISMATCH')
            # Pixel-derived files must not retain author/source/camera/GPS/XMP,
            # embedded previews or Photoshop/IPTC blocks under strip policy.
            forbidden = {270, 315, 33432, 34665, 34853, 700, 33723, 34377, 513, 514}
            if any(tag in page.tags for tag in forbidden):
                raise ValueError('ENCODE_METADATA_NOT_STRIPPED')
            decoded = page.asarray()
            if decoded.shape != page.shape or decoded.dtype != np.uint16:
                raise ValueError('TIFF_VERIFY_FAILED')
            if expected_pixels is not None:
                _verify_lossless_pixels(decoded, expected_pixels,
                                        tuple(range(4 if alpha else 3)),
                                        check_active)
    else:
        if params.format == 'png':
            _verify_png_layout(path, width, height, alpha)
        with Image.open(path) as image:
            image.verify()
        with Image.open(path) as image:
            if image.size != (width, height) or (image.mode in ('RGBA', 'LA')) != alpha or image.info.get('icc_profile') is None:
                raise ValueError('ENCODE_VERIFY_FAILED')
            if hashlib.sha256(image.info['icc_profile']).hexdigest() != params.icc_sha256:
                raise ValueError('ENCODE_ICC_MISMATCH')
            if getattr(image, 'text', {}) or image.getexif() or any(key in image.info for key in ('exif', 'xmp', 'XML:com.adobe.xmp', 'iptc', 'photoshop', 'comment')):
                raise ValueError('ENCODE_METADATA_NOT_STRIPPED')
            image.load()
        if params.format == 'png' and expected_pixels is not None:
            decoded = cv2.imread(str(path), cv2.IMREAD_UNCHANGED)
            if decoded is None:
                raise ValueError('ENCODE_PIXEL_MISMATCH')
            _verify_lossless_pixels(decoded, expected_pixels,
                                    (2, 1, 0, 3) if alpha else (2, 1, 0),
                                    check_active)


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, registry: ICCRegistry) -> OperationResult:
    if ctx.lease.execution_profile.get('backend') != 'cpu':
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(EncodeParams)
        image = artifacts.contract(params.image, WorkingImage)
        target_profile = registry.resolve(params.icc_id)
        source_profile = registry.resolve(image.color.profile_id)
        if target_profile.sha256 != params.icc_sha256 or image.color.icc_sha256 != source_profile.sha256:
            raise ValueError('ICC_BINDING_MISMATCH')
        required = [params.image, image.pixels, *([image.alpha] if image.alpha else [])]
        if list(ctx.lease.command.inputs) != required or image.dtype != 'float32':
            raise ValueError('ENCODE_INPUT_BINDING')
        if params.alpha == 'preserve' and params.format == 'jpeg':
            raise ValueError('JPEG_ALPHA_UNSUPPORTED')
    except (ValueError, ValidationError) as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    width, height = _dimensions(image.canvas.width, image.canvas.height, params)
    from .memory import admit_presentation
    admit_presentation(ctx.lease.execution_profile,image.canvas.width,image.canvas.height,width,height,alpha=image.alpha is not None)
    ctx.progress(0, 5)
    with ExitStack() as stack:
        rgb_local = stack.enter_context(artifacts.input(image.pixels, suffix='.npy'))
        rgb = stack.enter_context(read_array(rgb_local.path, (image.canvas.height, image.canvas.width, 3), 'float32', ctx.check_active, unit_interval=False))
        alpha = None
        if image.alpha is not None:
            alpha_local = stack.enter_context(artifacts.input(image.alpha, suffix='.npy'))
            alpha16 = stack.enter_context(read_alpha_array(alpha_local.path, (image.canvas.height, image.canvas.width), ctx.check_active))
            alpha = alpha16.astype(np.float32) / 65535 if alpha16.dtype==np.uint16 else alpha16
        ctx.progress(1, 5)
        resized,resized_alpha = _resize_working_image(rgb,alpha,width,height,image)
        source16 = np.round(np.clip(resized,0,1) * 65535).astype(np.uint16)
        converted16 = LittleCMS().convert(source16, source_profile.data, target_profile.data)
        converted = converted16.astype(np.float32) / 65535
        if resized_alpha is not None and params.alpha == 'matte':
            converted = _composite_matte(converted, resized_alpha, params.matte_rgb,
                                         params.icc_id)
            resized_alpha = None
        ctx.progress(2, 5)
        bands = converted if resized_alpha is None else np.dstack((converted, resized_alpha))
        suffix = {'jpeg': '.jpg', 'png': '.png', 'tiff': '.tiff'}[params.format]
        estimate = width * height * (8 if bands.shape[2] == 4 else 6) + 2 * 1024 * 1024
        with artifacts.scratch.reserve(estimate) as reservation:
            path = reservation.planned_path('encoded', suffix)
            try:
                _write(path, bands, target_profile.data, params)
                reservation.validate_file(path)
                _verify(path, params, width, height, resized_alpha is not None,
                        expected_pixels=bands, check_active=ctx.check_active)
            except (ValueError, OSError, pyvips.Error, tifffile.TiffFileError) as exc:
                raise OperationFailure('INPUT_INVALID') from exc
            ctx.progress(3, 5)
            output = artifacts.stage(path, reservation, 'encoded_image', {'jpeg': 'image/jpeg', 'png': 'image/png', 'tiff': 'image/tiff'}[params.format])
            written = path.stat().st_size
    ctx.progress(4, 5)
    manifest = EncodedImage(artifact=output, format=params.format, width=width, height=height,
        icc_id=params.icc_id, icc_sha256=target_profile.sha256, alpha=resized_alpha is not None,
        metadata_policy=params.metadata_policy)
    manifest_ref = artifacts.stage_contract(manifest, 'encoded_manifest')
    ctx.progress(5, 5)
    return OperationResult(outputs=[output, manifest_ref], bytes_written=written + len(canonical_bytes(manifest)))


def export_operations(config: ArtifactConfig, icc_root=None) -> list[Operation]:
    if icc_root is None:
        return []
    registry = ICCRegistry(icc_root)

    def encode(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'encoded_image', 'encoded_manifest'})) as artifacts:
            return execute(ctx, artifacts, registry)
    return [Operation('export.encode', 'cpu', encode)]
