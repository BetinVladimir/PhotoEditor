"""Physical raster verification for delivery adoption, independent of the writer.

The caller supplies SQL-authorized Params/source/manifest and a private staged
file. This proves the encoded container, not photographic pixel equivalence or
SQL adoption authority. No production adoption path is enabled by this module.
"""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
import stat

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodeParams, EncodedImage
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.metadata import MetadataDocument


@dataclass(frozen=True)
class VerifiedEncodedRaster:
    sha256: str
    bytes: int
    width: int
    height: int
    alpha: bool


def verify_delivery_encoded_file(path: Path, params: EncodeParams,
        source: WorkingImage, manifest: EncodedImage, *, max_bytes: int,
        max_pixels: int, check_active=lambda: None, metadata_document=None, metadata_policy='copyright_only') -> VerifiedEncodedRaster:
    """Hash first; reject allocation bounds and metadata before raster decoding."""
    check_active()
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or type(max_pixels) is not int or not 1<=max_pixels<=200000000:
        raise ValueError('DELIVERY_ENCODE_LIMIT_INVALID')
    # This is an explicit post-metadata verification input supplied by the
    # authenticated adoption coordinator, never inferred from manifest policy.
    if metadata_document is not None:
        if (not isinstance(metadata_document,MetadataDocument)
                or metadata_policy not in ('copyright_only','safe_camera')
                or manifest.metadata_policy!=metadata_policy):
            raise ValueError('DELIVERY_ENCODE_METADATA_AUTHORITY')
    elif manifest.metadata_policy!='strip':
        raise ValueError('DELIVERY_ENCODE_METADATA_AUTHORITY')
    width, height = source.canvas.width, source.canvas.height
    if params.size is not None:
        size = params.size
        scale = (size.long_edge / max(width, height) if size.long_edge is not None
                 else min(size.width / width, size.height / height))
        if not size.allow_upscale:
            scale = min(1., scale)
        width, height = max(1, round(width * scale)), max(1, round(height * scale))
    alpha = source.alpha is not None and params.alpha == 'preserve'
    if width * height > max_pixels:
        raise ValueError('DELIVERY_ENCODE_PIXEL_LIMIT')
    if (manifest.format != params.format or manifest.width != width or manifest.height != height
            or manifest.icc_id != params.icc_id or manifest.icc_sha256 != params.icc_sha256
            or manifest.alpha != alpha
            or params.metadata_policy != 'strip' or params.format == 'jpeg' and alpha):
        raise ValueError('DELIVERY_ENCODE_MANIFEST_MISMATCH')
    return _verify_manifest_file(path,manifest,max_bytes=max_bytes,max_pixels=max_pixels,
        check_active=check_active,metadata_document=metadata_document,metadata_policy=metadata_policy)


def _verify_manifest_file(path,manifest,*,max_bytes,max_pixels,check_active=lambda:None,
        metadata_document=None,metadata_policy='copyright_only'):
    check_active()
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or type(max_pixels) is not int or not 1<=max_pixels<=200000000:
        raise ValueError('DELIVERY_ENCODE_LIMIT_INVALID')
    manifest=EncodedImage.model_validate_json(canonical_bytes(manifest))
    if metadata_document is not None:
        metadata_document=MetadataDocument.model_validate_json(canonical_bytes(metadata_document))
        if metadata_policy not in ('copyright_only','safe_camera') or manifest.metadata_policy!=metadata_policy:
            raise ValueError('DELIVERY_ENCODE_METADATA_AUTHORITY')
    elif manifest.metadata_policy!='strip':
        raise ValueError('DELIVERY_ENCODE_METADATA_AUTHORITY')
    width,height,alpha=manifest.width,manifest.height,manifest.alpha
    if width*height>max_pixels:raise ValueError('DELIVERY_ENCODE_PIXEL_LIMIT')
    if manifest.artifact.artifact_id.int==0 or manifest.format=='jpeg' and alpha:
        raise ValueError('DELIVERY_ENCODE_MANIFEST_MISMATCH')
    path = Path(path)
    before = path.stat(follow_symlinks=False)
    if (not path.is_absolute() or path.resolve(strict=True)!=path or not stat.S_ISREG(before.st_mode)
            or before.st_nlink!=1 or not 1 <= before.st_size <= max_bytes):
        raise ValueError('DELIVERY_ENCODE_BYTE_LIMIT')
    digest = sha256()
    count = 0
    with path.open('rb') as stream:
        while block := stream.read(1024 * 1024):
            check_active()
            count += len(block)
            if count > max_bytes:
                raise ValueError('DELIVERY_ENCODE_BYTE_LIMIT')
            digest.update(block)
    if count != before.st_size or digest.hexdigest() != manifest.artifact.sha256:
        raise ValueError('DELIVERY_ENCODE_SHA_MISMATCH')
    check_active()
    if manifest.format == 'tiff':
        _verify_tiff(path, width, height, alpha, manifest.icc_sha256, check_active,metadata_document,metadata_policy)
    else:
        _verify_pillow(path, manifest.format, width, height, alpha, manifest.icc_sha256, check_active,metadata_document,metadata_policy)
    check_active()
    after = path.stat(follow_symlinks=False)
    identity=lambda item:(item.st_dev,item.st_ino,item.st_size,item.st_mtime_ns,item.st_ctime_ns,item.st_mode,item.st_nlink)
    if identity(before)!=identity(after):
        raise ValueError('DELIVERY_ENCODE_FILE_CHANGED')
    return VerifiedEncodedRaster(digest.hexdigest(), count, width, height, alpha)


def _verify_pillow(path, format, width, height, alpha, icc_sha, check_active,metadata_document=None,metadata_policy='copyright_only'):
    from PIL import Image
    if format == 'png':
        with path.open('rb') as stream:
            header = stream.read(33)
        if (len(header) != 33 or header[:8] != b'\x89PNG\r\n\x1a\n'
                or header[8:16] != b'\x00\x00\x00\rIHDR'
                or int.from_bytes(header[16:20], 'big') != width
                or int.from_bytes(header[20:24], 'big') != height
                or header[24] != 16 or header[25] != (6 if alpha else 2)
                or header[26:28] != b'\x00\x00' or header[28] not in (0, 1)):
            raise ValueError('DELIVERY_ENCODE_PNG_LAYOUT')
    with Image.open(path) as image:
        if (image.format != {'jpeg': 'JPEG', 'png': 'PNG'}[format]
                or image.size != (width, height) or getattr(image, 'n_frames', 1) != 1
                or image.mode != ('RGBA' if alpha else 'RGB')):
            raise ValueError('DELIVERY_ENCODE_CONTAINER_MISMATCH')
        profile = image.info.get('icc_profile')
        if not isinstance(profile, bytes) or sha256(profile).hexdigest() != icc_sha:
            raise ValueError('DELIVERY_ENCODE_ICC_MISMATCH')
        if metadata_document is None:
            if (getattr(image, 'text', {}) or image.getexif()
                    or any(key in image.info for key in ('exif', 'xmp', 'XML:com.adobe.xmp', 'iptc', 'photoshop', 'comment'))):
                raise ValueError('DELIVERY_ENCODE_METADATA_NOT_STRIPPED')
        else:
            _verify_metadata(image,metadata_document,metadata_policy,check_active)
        image.verify()
    check_active()
    with Image.open(path) as image:
        image.load()


def _verify_tiff(path, width, height, alpha, icc_sha, check_active,metadata_document=None,metadata_policy='copyright_only'):
    import numpy as np
    import tifffile
    with tifffile.TiffFile(path) as document:
        if len(document.pages) != 1:
            raise ValueError('DELIVERY_ENCODE_TIFF_LAYOUT')
        page = document.pages[0]
        if (page.shape != (height, width, 4 if alpha else 3) or page.dtype != np.uint16
                or int(page.photometric) != 2 or page.subifds
                or tuple(int(value) for value in page.extrasamples) != ((2,) if alpha else ())):
            raise ValueError('DELIVERY_ENCODE_TIFF_LAYOUT')
        profile = page.tags.get(34675)
        if profile is None or sha256(bytes(profile.value)).hexdigest() != icc_sha:
            raise ValueError('DELIVERY_ENCODE_ICC_MISMATCH')
        if metadata_document is None:
            if any(tag in page.tags for tag in (270, 315, 33432, 34665, 34853, 700, 33723, 34377, 513, 514)):
                raise ValueError('DELIVERY_ENCODE_METADATA_NOT_STRIPPED')
        else:
            from PIL import Image
            # Read headers/EXIF only; tifffile remains the 16bit raster decoder.
            with Image.open(path) as metadata_image:
                if metadata_image.format!='TIFF' or metadata_image.size!=(width,height):
                    raise ValueError('DELIVERY_ENCODE_CONTAINER_MISMATCH')
                _verify_metadata(metadata_image,metadata_document,metadata_policy,check_active)
        check_active()
        decoded = page.asarray()
        if decoded.shape != page.shape or decoded.dtype != np.uint16:
            raise ValueError('DELIVERY_ENCODE_TIFF_LAYOUT')


def _verify_metadata(image,projection,policy,check_active):
    if policy=='copyright_only':
        from .delivery_copyright_verify import verify_delivery_copyright_image
        verify_delivery_copyright_image(image,projection,check_active=check_active)
    elif policy=='safe_camera':
        from types import SimpleNamespace
        from photo_worker.operations.metadata.verify import verify_written_metadata
        # Projection is externally authenticated; presence alone never approves GPS.
        # The coordinator must derive it from the externally pinned frozen task.
        verify_written_metadata(image,projection,
            SimpleNamespace(policy='safe_camera',copy_gps=projection.gps is not None),
            check_active=check_active)
    else:
        raise ValueError('DELIVERY_ENCODE_METADATA_AUTHORITY')
