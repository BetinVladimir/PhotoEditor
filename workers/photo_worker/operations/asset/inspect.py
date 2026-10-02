from __future__ import annotations
from dataclasses import dataclass
import hashlib
import re
from pathlib import Path

from photo_worker.color.icc import validate_icc
from .verify import AssetError,MAX_PIXELS,VerifiedSource

@dataclass(frozen=True)
class ImageMetadata:
    width: int
    height: int
    bit_depth: int
    has_alpha: bool
    orientation: int
    icc: bytes | None
    format: str
    sensor_alignment: dict | None = None

    @property
    def icc_sha256(self) -> str | None:
        return hashlib.sha256(self.icc).hexdigest() if self.icc else None


def _pixels(width: int, height: int) -> None:
    if width < 1 or height < 1 or width*height > MAX_PIXELS:
        raise AssetError('PIXEL_LIMIT_EXCEEDED')


def detect_orientation(image) -> int:
    orientation = int(image.get('orientation')) if image.get_typeof('orientation') else 1
    if orientation not in range(1,9):
        raise AssetError('ORIENTATION_INVALID')
    return orientation


def open_raster(path: Path):
    import pyvips
    pyvips.cache_set_max_mem(512*1024*1024)
    try:
        image = pyvips.Image.new_from_file(str(path), access='random', fail_on='error')
    except pyvips.Error as exc:
        raise AssetError('IMAGE_HEADER_INVALID') from exc
    _pixels(image.width,image.height)
    pages = int(image.get('n-pages')) if image.get_typeof('n-pages') else 1
    if pages != 1:
        raise AssetError('MULTIPAGE_UNSUPPORTED')
    if image.interpretation not in ('srgb','rgb','rgb16') or image.bands not in (3,4) or image.format not in ('uchar','ushort'):
        raise AssetError('RGB_8_OR_16_REQUIRED')
    return image


def read_metadata(source: VerifiedSource) -> ImageMetadata:
    if source.format == 'raw':
        import rawpy
        try:
            with rawpy.imread(str(source.path)) as raw:
                _pixels(raw.sizes.raw_width,raw.sizes.raw_height)
                width,height = raw.sizes.width,raw.sizes.height
                _pixels(width,height)
                sizes=raw.sizes;alignment=None
                # A physical-sensor mapping is published only for unrotated RAW
                # and only when the file carries a body serial. Model alone is
                # insufficient because dust belongs to one physical sensor.
                if int(getattr(sizes,'flip',0))==0:
                    camera=_raw_camera_id(source.path)
                    if camera is not None:
                        alignment=dict(camera_id=camera,sensor_width=int(sizes.raw_width),sensor_height=int(sizes.raw_height),crop_x=int(sizes.left_margin),crop_y=int(sizes.top_margin),crop_width=int(sizes.width),crop_height=int(sizes.height),orientation=1)
                return ImageMetadata(width,height,16,False,1,None,'raw',alignment)
        except rawpy.LibRawError as exc:
            raise AssetError('RAW_UNSUPPORTED') from exc
    image = open_raster(source.path)
    icc = bytes(image.get('icc-profile-data')) if image.get_typeof('icc-profile-data') else None
    if icc is not None:
        validate_icc(icc)
    return ImageMetadata(image.width,image.height,8 if image.format=='uchar' else 16,
                         bool(image.hasalpha()),detect_orientation(image),icc,source.format)


def _exif_value(image,names):
    fields={name.lower():name for name in image.get_fields()}
    for wanted in names:
        key=fields.get(wanted.lower())
        if key:
            value=str(image.get(key)).split(' (',1)[0].strip()
            if value:return re.sub(r'\s+',' ',value)
    return None


def _raw_camera_id(path:Path)->str|None:
    import pyvips
    try:image=pyvips.Image.new_from_file(str(path),access='sequential',fail_on='error')
    except pyvips.Error:return None
    make=_exif_value(image,['exif-ifd0-Make','exif-ifd0-make'])
    model=_exif_value(image,['exif-ifd0-Model','exif-ifd0-model'])
    serial=_exif_value(image,['exif-ifd0-BodySerialNumber','exif-exif-BodySerialNumber','exif-ifd0-CameraSerialNumber'])
    if not make or not model or not serial:return None
    return 'raw-'+hashlib.sha256(('camera-v1\0'+make+'\0'+model+'\0'+serial).encode()).hexdigest()
