from __future__ import annotations
from dataclasses import dataclass
import hashlib
import re
from datetime import datetime,timezone,timedelta
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
    captured_at: str | None = None
    captured_local: str | None = None

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


def require_tiff_straight_alpha(path: Path, bands: int) -> None:
    """Bind TIFF RGB samples to the declared ExtraSamples semantics.

    The generic libvips loader is not an authenticated associated-alpha
    normalization contract. Accept explicit unassociated alpha; never divide
    RGB speculatively (zero-alpha hidden RGB cannot be recovered that way).
    tifffile reads only the first IFD's metadata here, not pixel data.
    """
    with path.open('rb') as stream:
        magic=stream.read(4)
    if magic not in (b'II*\0',b'MM\0*',b'II+\0',b'MM\0+'):
        return
    import tifffile
    try:
        with tifffile.TiffFile(path) as document:
            page=document.pages[0]
            samples=int(page.samplesperpixel)
            extras=tuple(int(value) for value in page.extrasamples)
            if samples!=bands or int(page.photometric)!=2:
                raise AssetError('TIFF_RGB_LAYOUT_UNSUPPORTED')
            if bands==4 and extras!=(2,):
                raise AssetError('TIFF_ALPHA_SEMANTICS_UNSUPPORTED')
            if bands==3 and extras:
                raise AssetError('TIFF_ALPHA_SEMANTICS_UNSUPPORTED')
    except (tifffile.TiffFileError,IndexError,TypeError,ValueError) as exc:
        if isinstance(exc,AssetError):
            raise
        raise AssetError('IMAGE_HEADER_INVALID') from exc


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
    require_tiff_straight_alpha(path,image.bands)
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
                instant,local=_raw_capture_time(source.path)
                return ImageMetadata(width,height,16,False,1,None,'raw',alignment,instant,local)
        except rawpy.LibRawError as exc:
            raise AssetError('RAW_UNSUPPORTED') from exc
    image = open_raster(source.path)
    icc = bytes(image.get('icc-profile-data')) if image.get_typeof('icc-profile-data') else None
    if icc is not None:
        validate_icc(icc)
    instant,local=_capture_time(image)
    return ImageMetadata(image.width,image.height,8 if image.format=='uchar' else 16,
                         bool(image.hasalpha()),detect_orientation(image),icc,source.format,None,instant,local)


def _exif_value(image,names):
    fields={name.lower():name for name in image.get_fields()}
    for wanted in names:
        key=fields.get(wanted.lower())
        if key:
            value=str(image.get(key)).split(' (',1)[0].strip()
            if value:return re.sub(r'\s+',' ',value)
    return None


def _capture_time(image):
    """Never assign the server timezone to an EXIF floating local timestamp."""
    raw=_exif_value(image,['exif-ifd2-DateTimeOriginal','exif-exif-DateTimeOriginal'])
    if not raw or not re.fullmatch(r'\d{4}:\d{2}:\d{2} \d{2}:\d{2}:\d{2}',raw):return None,None
    try:value=datetime.strptime(raw,'%Y:%m:%d %H:%M:%S')
    except ValueError:return None,None
    offset=_exif_value(image,['exif-ifd2-OffsetTimeOriginal','exif-exif-OffsetTimeOriginal'])
    local=value.isoformat(timespec='seconds')
    if not offset or not re.fullmatch(r'[+-]\d{2}:\d{2}',offset):return None,local
    hours,minutes=int(offset[1:3]),int(offset[4:6])
    if hours>14 or minutes>59 or hours==14 and minutes!=0:return None,local
    delta=timedelta(hours=hours,minutes=minutes)*(1 if offset[0]=='+' else -1)
    try:instant=value.replace(tzinfo=timezone(delta)).astimezone(timezone.utc)
    except (OverflowError,ValueError):return None,local
    return instant.isoformat(timespec='seconds').replace('+00:00','Z'),None


def _raw_capture_time(path):
    import pyvips
    try:image=pyvips.Image.new_from_file(str(path),access='sequential',fail_on='error')
    except pyvips.Error:return None,None
    return _capture_time(image)


def _raw_camera_id(path:Path)->str|None:
    import pyvips
    try:image=pyvips.Image.new_from_file(str(path),access='sequential',fail_on='error')
    except pyvips.Error:return None
    make=_exif_value(image,['exif-ifd0-Make','exif-ifd0-make'])
    model=_exif_value(image,['exif-ifd0-Model','exif-ifd0-model'])
    serial=_exif_value(image,['exif-ifd0-BodySerialNumber','exif-exif-BodySerialNumber','exif-ifd0-CameraSerialNumber'])
    if not make or not model or not serial:return None
    return 'raw-'+hashlib.sha256(('camera-v1\0'+make+'\0'+model+'\0'+serial).encode()).hexdigest()
