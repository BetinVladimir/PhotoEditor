"""Bounded color-managed thumbnail and preview generation."""
from __future__ import annotations

from contextlib import ExitStack
import hashlib
import numpy as np
import pyvips
from PIL import Image
from pydantic import ValidationError

from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.media import DerivativeManifest, MediaParams
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult
from photo_worker.operations.export.encode import _resize, _resize_rgba


EDGES = {'thumb320': 320, 'thumb640': 640, 'preview1600': 1600, 'preview2560': 2560}
QUALITY = {'thumb320': 82, 'thumb640': 82, 'preview1600': 88, 'preview2560': 88}
MAX_PIXELS = 200_000_000


RENDERER_POLICY = 'declared_encoding_alpha8_icc_v2'


def media_profile_hash(variant: str, srgb_sha256: str, *, legacy: bool = False) -> str:
    policy={
        'schema_version': 1, 'variant': variant, 'long_edge': EDGES[variant],
        'quality': QUALITY[variant], 'icc_sha256': srgb_sha256,
        'no_upscale': True, 'metadata_policy': 'stripped',
    }
    if not legacy:policy['renderer_policy']=RENDERER_POLICY
    return hashlib.sha256(canonical_bytes(policy)).hexdigest()


def dimensions(width: int, height: int, variant: str) -> tuple[int, int]:
    edge = EDGES[variant]
    scale = min(1.0, edge / max(width, height))
    return max(1, round(width * scale)), max(1, round(height * scale))


def _alpha8(alpha):
    return np.round(np.clip(alpha,0,1)*255).astype(np.uint8)


def _write(path, rgb: np.ndarray, alpha: np.ndarray | None, profile: bytes, quality: int, *, strip_metadata: bool = False):
    height, width, _ = rgb.shape
    metadata_options = {"keep": "icc"} if strip_metadata else {}
    rgb8 = np.round(np.clip(rgb, 0, 1) * 255).astype(np.uint8)
    if alpha is None:
        image = pyvips.Image.new_from_memory(rgb8.data, width, height, 3, 'uchar').copy(interpretation='srgb')
        image.set_type(pyvips.GValue.blob_type, 'icc-profile-data', profile)
        image.jpegsave(str(path), Q=quality, subsample_mode='off', optimize_coding=True, strip=False, **metadata_options)
    else:
        alpha8 = _alpha8(alpha)
        rgba = np.dstack((rgb8, alpha8))
        image = pyvips.Image.new_from_memory(rgba.data, width, height, 4, 'uchar').copy(interpretation='srgb')
        image.set_type(pyvips.GValue.blob_type, 'icc-profile-data', profile)
        image.webpsave(str(path), Q=quality, lossless=False, strip=False, **metadata_options)


def _read_master(path, check_active, *, admit=None):
    """Decode an immutable encoded master without trusting filename metadata."""
    check_active()
    try:
        image = pyvips.Image.new_from_file(str(path), access='sequential', fail_on='error').autorot()
        if image.width < 1 or image.height < 1 or image.width * image.height > MAX_PIXELS or image.bands not in (3, 4):
            raise ValueError('MASTER_LAYOUT_INVALID')
        if image.format not in ('uchar', 'ushort'):
            raise ValueError('MASTER_DEPTH_INVALID')
        if image.get_typeof('icc-profile-data') == 0:
            raise ValueError('MASTER_ICC_MISSING')
        profile = bytes(image.get('icc-profile-data'))
        if not 128 <= len(profile) <= 4 * 1024 * 1024:
            raise ValueError('MASTER_ICC_INVALID')
        check_active()
        if admit is not None:admit(image.width,image.height,image.bands==4)
        dtype = np.uint8 if image.format == 'uchar' else np.uint16
        pixels = np.frombuffer(image.write_to_memory(), dtype=dtype).reshape(image.height, image.width, image.bands)
        rgb16 = pixels[..., :3].astype(np.uint16)
        if dtype == np.uint8:
            rgb16 *= 257
        alpha = None
        if image.bands == 4:
            alpha = pixels[..., 3].astype(np.float32) / (255 if dtype == np.uint8 else 65535)
        return rgb16, alpha, profile
    except (pyvips.Error, ValueError, MemoryError) as exc:
        raise OperationFailure('INPUT_INVALID') from exc


def _presentation_alpha_matches(alpha, width, height, check_active, image=None):
    """Match lossless WebP alpha to the writer's exact 8-bit quantization.

    RGB WebP implies 255 everywhere. RGBA must match each actual alpha sample.
    This is the presentation contract, not a change to working/source alpha.
    """
    if not isinstance(alpha,np.ndarray) or alpha.shape != (height,width):
        return False
    for y in range(0,height,256):
        for x in range(0,width,256):
            check_active()
            tile=alpha[y:y+256,x:x+256]
            if not np.isfinite(tile).all():
                return False
            expected=_alpha8(tile)
            if image is None:
                if not np.all(expected == 255):return False
            else:
                box=(x,y,min(x+256,width),min(y+256,height))
                with image.crop(box) as region:
                    with region.getchannel('A') as plane:
                        if not np.array_equal(np.asarray(plane),expected):return False
    check_active()
    return True


def _verify_derivative(path, width, height, has_alpha, profile_sha, *, expected_alpha=None, check_active=lambda:None):
    """Verify the physical presentation contract before immutable publication."""
    check_active()
    with Image.open(path) as check:
        check.verify()
    with Image.open(path) as check:
        check_active()
        if check.size != (width,height):
            raise ValueError('DERIVATIVE_VERIFY_FAILED')
        check.load()
        check_active()
        expected_format = 'WEBP' if has_alpha else 'JPEG'
        expected_mode = 'RGBA' if has_alpha else 'RGB'
        mode_valid = check.mode == expected_mode
        if has_alpha and check.format == 'WEBP' and check.mode in ('RGB','RGBA'):
            mode_valid = _presentation_alpha_matches(expected_alpha,width,height,check_active,
                check if check.mode == 'RGBA' else None)
        profile = check.info.get('icc_profile')
        if (check.size != (width, height) or check.format != expected_format or
                not mode_valid or not isinstance(profile, bytes) or
                hashlib.sha256(profile).hexdigest() != profile_sha or
                check.info.get('exif') or check.info.get('xmp') or check.getexif()):
            raise ValueError('DERIVATIVE_VERIFY_FAILED')
    check_active()


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, registry: ICCRegistry, operation: str) -> OperationResult:
    if ctx.lease.execution_profile.get('backend') != 'cpu':
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(MediaParams)
        expected_prefix = 'thumb' if operation == 'media.thumbnail' else 'preview'
        if not params.variant.startswith(expected_prefix):
            raise ValueError('VARIANT_OPERATION_MISMATCH')
        target_profile = registry.resolve('srgb_v1')
        legacy = params.profile_hash == media_profile_hash(params.variant,target_profile.sha256,legacy=True)
        if not legacy and params.profile_hash != media_profile_hash(params.variant, target_profile.sha256):
            raise ValueError('MEDIA_PROFILE_MISMATCH')
        if params.source_kind == 'working':
            image = artifacts.contract(params.image, WorkingImage)
            source_profile = registry.resolve(image.color.profile_id)
            if source_profile.sha256 != image.color.icc_sha256:
                raise ValueError('ICC_BINDING_MISMATCH')
            required = [params.image, image.pixels, *([image.alpha] if image.alpha else [])]
            if list(ctx.lease.command.inputs) != required or image.dtype != 'float32':
                raise ValueError('MEDIA_INPUT_BINDING')
        else:
            image = None
            source_profile = None
            if list(ctx.lease.command.inputs) != [params.image]:
                raise ValueError('MEDIA_INPUT_BINDING')
    except (ValueError, ValidationError) as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    if image is not None:
        from photo_worker.operations.export.memory import admit_presentation
        planned_width,planned_height=dimensions(image.canvas.width,image.canvas.height,params.variant)
        admit_presentation(ctx.lease.execution_profile,image.canvas.width,image.canvas.height,
                           planned_width,planned_height,alpha=image.alpha is not None)
    ctx.progress(0, 4)
    with ExitStack() as stack:
        if image is not None:
            rgb_local = stack.enter_context(artifacts.input(image.pixels, suffix='.npy'))
            rgb = stack.enter_context(read_array(rgb_local.path, (image.canvas.height, image.canvas.width, 3), 'float32', ctx.check_active, unit_interval=False))
            alpha = None
            if image.alpha is not None:
                alpha_local = stack.enter_context(artifacts.input(image.alpha, suffix='.npy'))
                alpha16 = stack.enter_context(read_alpha_array(alpha_local.path, (image.canvas.height, image.canvas.width), ctx.check_active))
                alpha = alpha16.astype(np.float32) / 65535 if alpha16.dtype==np.uint16 else alpha16
            source_width, source_height = image.canvas.width, image.canvas.height
            width, height = dimensions(source_width, source_height, params.variant)
            resized_source,resized_alpha = _resize_rgba(rgb,alpha,width,height,encoding='gamma24' if legacy else image.color.encoding)
            converted16 = LittleCMS().convert(np.round(np.clip(resized_source,0,1) * 65535).astype(np.uint16), source_profile.data, target_profile.data,preserve_identity=not legacy)
            resized = converted16.astype(np.float32) / 65535
        else:
            encoded = stack.enter_context(artifacts.input(params.image, suffix=params.suffix))
            from photo_worker.operations.export.memory import admit_presentation
            def admit_master(source_width,source_height,has_alpha):
                target_width,target_height=dimensions(source_width,source_height,params.variant)
                admit_presentation(ctx.lease.execution_profile,source_width,source_height,
                                   target_width,target_height,alpha=has_alpha)
            rgb16, alpha, embedded_profile = _read_master(encoded.path, ctx.check_active,admit=admit_master)
            source_height, source_width = rgb16.shape[:2]
            converted16 = LittleCMS().convert(rgb16, embedded_profile, target_profile.data,preserve_identity=not legacy)
            width, height = dimensions(source_width, source_height, params.variant)
            converted = converted16.astype(np.float32) / 65535
            resized,resized_alpha = _resize_rgba(converted,alpha,width,height,encoding='srgb')
        ctx.progress(1, 4)
        media_type = 'image/webp' if resized_alpha is not None else 'image/jpeg'
        suffix = '.webp' if resized_alpha is not None else '.jpg'
        with artifacts.scratch.reserve(width * height * 5 + 2 * 1024 * 1024) as reservation:
            path = reservation.planned_path('derivative', suffix)
            try:
                _write(path, resized, resized_alpha, target_profile.data, QUALITY[params.variant], strip_metadata=True)
                reservation.validate_file(path)
                _verify_derivative(path, width, height, resized_alpha is not None, target_profile.sha256,
                    expected_alpha=resized_alpha,check_active=ctx.check_active)
            except (OSError, ValueError, pyvips.Error) as exc:
                raise OperationFailure('INPUT_INVALID') from exc
            derivative = artifacts.stage(path, reservation, 'derivative', media_type)
            written = path.stat().st_size
    ctx.progress(2, 4)
    manifest = DerivativeManifest(source=params.image, derivative=derivative, variant=params.variant,
        profile_hash=params.profile_hash, width=width, height=height, media_type=media_type,
        color_profile='srgb', metadata_policy='stripped', alpha=resized_alpha is not None)
    manifest_ref = artifacts.stage_contract(manifest, 'derivative_manifest')
    ctx.progress(4, 4)
    return OperationResult(outputs=[derivative, manifest_ref], bytes_written=written + len(canonical_bytes(manifest)))


def media_operations(config: ArtifactConfig, icc_root=None) -> list[Operation]:
    if icc_root is None:
        return []
    registry = ICCRegistry(icc_root)
    result = []
    for name in ('media.thumbnail', 'media.preview'):
        def run(ctx, operation=name):
            with TaskArtifacts.create(ctx, config, frozenset({'derivative', 'derivative_manifest'})) as artifacts:
                return execute(ctx, artifacts, registry, operation)
        result.append(Operation(name, 'media', run))
    return result
