"""Typed Render result to existing color-managed media.preview input plan.

The caller must authorize the adopted result closure before scheduling this plan.
No task, policy activation or adoption is performed by this compiler.
"""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.media import MediaParams, Variant
from photo_worker.operations.optical.contracts import ImageResult

@dataclass(frozen=True)
class RenderDerivativePlan:
    params_bytes: bytes
    params_sha256: str
    inputs: tuple[ArtifactRef, ...]
    source_result: ArtifactRef


def compile_render_derivative(result_ref: ArtifactRef, result_bytes: bytes,
                              image_bytes: bytes, srgb_sha256: str,
                              variant: Variant = 'preview1600') -> RenderDerivativePlan:
    """Bind exact result/image bytes; preserve alpha and existing preview profile."""
    from photo_worker.operations.media.derivative import media_profile_hash
    if (not isinstance(result_ref, ArtifactRef) or not isinstance(result_bytes, bytes)
            or not isinstance(image_bytes, bytes) or not 1 <= len(result_bytes) <= 1048576
            or not 1 <= len(image_bytes) <= 1048576
            or variant not in ('preview1600', 'preview2560')
            or not isinstance(srgb_sha256, str) or len(srgb_sha256) != 64
            or any(c not in '0123456789abcdef' for c in srgb_sha256)):
        raise ValueError('RENDER_DERIVATIVE_INPUT_INVALID')
    result = ImageResult.model_validate_json(result_bytes)
    image = WorkingImage.model_validate_json(image_bytes)
    if (canonical_bytes(result) != result_bytes or canonical_bytes(image) != image_bytes
            or sha256(result_bytes).hexdigest() != result_ref.sha256
            or sha256(image_bytes).hexdigest() != result.working_image.sha256
            or image.dtype != 'float32' or image.canvas.width * image.canvas.height > 200000000):
        raise ValueError('RENDER_DERIVATIVE_INPUT_INVALID')
    params = MediaParams(image=result.working_image, variant=variant,
                         profile_hash=media_profile_hash(variant, srgb_sha256))
    raw = canonical_bytes(params)
    inputs = (result.working_image, image.pixels, *((image.alpha,) if image.alpha else ()))
    if (len({ref.artifact_id for ref in inputs}) != len(inputs)
            or result_ref.artifact_id in {ref.artifact_id for ref in inputs}):
        raise ValueError('RENDER_DERIVATIVE_INPUT_INVALID')
    return RenderDerivativePlan(raw, sha256(raw).hexdigest(), inputs, result_ref.model_copy(deep=True))


def verify_render_derivative_encoded(manifest, params: MediaParams, image: WorkingImage,
                                     body: bytes, *, srgb_sha256: str) -> None:
    """Verify encoded evidence after independent catalog/version/fence proofs."""
    from io import BytesIO
    from PIL import Image
    from photo_worker.contracts.media import DerivativeManifest
    from photo_worker.operations.media.derivative import dimensions, media_profile_hash
    if (not isinstance(manifest, DerivativeManifest) or not isinstance(params, MediaParams)
            or not isinstance(image, WorkingImage) or not isinstance(body, bytes)
            or not 2 <= len(body) <= 52 * 1048576
            or params.variant not in ('preview1600', 'preview2560')
            or params.source_kind != 'working' or params.suffix is not None
            or params.matte_rgb is not None or image.dtype != 'float32'
            or sha256(canonical_bytes(image)).hexdigest() != params.image.sha256
            or image.canvas.width * image.canvas.height > 200000000
            or params.profile_hash != media_profile_hash(params.variant, srgb_sha256)
            or manifest.profile_hash != params.profile_hash or manifest.variant != params.variant
            or manifest.source != params.image or manifest.derivative.artifact_id == params.image.artifact_id
            or manifest.color_profile != 'srgb' or manifest.metadata_policy != 'stripped'
            or manifest.alpha != (image.alpha is not None)
            or manifest.media_type != ('image/webp' if manifest.alpha else 'image/jpeg')
            or (manifest.width, manifest.height) != dimensions(image.canvas.width, image.canvas.height, params.variant)
            or sha256(body).hexdigest() != manifest.derivative.sha256):
        raise ValueError('RENDER_DERIVATIVE_ENCODING_INVALID')
    if manifest.alpha:
        if body[:4] != b'RIFF' or body[8:12] != b'WEBP' or int.from_bytes(body[4:8], 'little') + 8 != len(body):
            raise ValueError('RENDER_DERIVATIVE_ENCODING_INVALID')
    elif not body.endswith(b'\xff\xd9'):
        raise ValueError('RENDER_DERIVATIVE_ENCODING_INVALID')
    def check(encoded):
        if (encoded.format != ('WEBP' if manifest.alpha else 'JPEG')
                or encoded.size != (manifest.width, manifest.height)
                or encoded.mode != ('RGBA' if manifest.alpha else 'RGB')
                or getattr(encoded, 'n_frames', 1) != 1
                or sha256(encoded.info.get('icc_profile', b'')).hexdigest() != srgb_sha256
                or any(encoded.info.get(key) for key in ('exif', 'xmp', 'comment', 'photoshop'))):
            raise ValueError('RENDER_DERIVATIVE_ENCODING_INVALID')
    with Image.open(BytesIO(body)) as encoded:
        check(encoded)
        encoded.verify()
    # JPEG verify alone does not decode the entropy stream; load all bounded pixels.
    with Image.open(BytesIO(body)) as encoded:
        check(encoded)
        encoded.load()
