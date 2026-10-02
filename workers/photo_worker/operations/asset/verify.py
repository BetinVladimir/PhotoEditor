from __future__ import annotations
from dataclasses import dataclass
import hashlib
from pathlib import Path
from typing import Callable

MAX_FILE_BYTES = 536870912
MAX_PIXELS = 200000000
RAW_EXTENSIONS = {'.nef','.nrw','.crw','.cr2','.cr3','.arw','.srf','.sr2','.mrw','.dng','.rw2','.orf','.raf','.pef','.srw','.3fr','.fff','.iiq','.mos','.kdc','.mef','.x3f'}

class AssetError(ValueError):
    pass

@dataclass(frozen=True)
class VerifiedSource:
    path: Path
    format: str
    bytes: int
    sha256: str


def verify_magic(path: Path) -> str:
    with path.open('rb') as stream:
        header = stream.read(32)
    if header.startswith(b'\xff\xd8\xff'):
        return 'jpeg'
    if header.startswith(b'\x89PNG\r\n\x1a\n'):
        return 'png'
    if header[:4] in (b'II*\0',b'MM\0*',b'II+\0',b'MM\0+'):
        return 'raw' if path.suffix.lower() in RAW_EXTENSIONS else 'tiff'
    if path.suffix.lower() in RAW_EXTENSIONS and (header[:4] in (b'IIRO',b'IIRS',b'MMOR',b'IIU\0',b'\0MRM') or header[6:14]==b'HEAPCCDR' or header.startswith(b'FUJIFILMCCD-RAW') or header.startswith(b'FOVb') or header[4:8] == b'ftyp'):
        return 'raw'
    raise AssetError('FORMAT_UNSUPPORTED')


def verify_suffix(kind: str, suffix: str) -> None:
    suffix = suffix.lower()
    allowed = {
        'jpeg': {'.jpg', '.jpeg'},
        'png': {'.png'},
        'tiff': {'.tif', '.tiff'},
        'raw': RAW_EXTENSIONS,
    }
    if kind not in allowed or suffix not in allowed[kind]:
        raise AssetError('FORMAT_SUFFIX_MISMATCH')


def verify_size(path: Path, expected_size: int | None = None, expected_sha256: str | None = None, *, check_active: Callable[[],None] | None = None) -> tuple[int,str]:
    if check_active is not None:check_active()
    if path.is_symlink() or not path.is_file():
        raise AssetError('INPUT_NOT_REGULAR_FILE')
    size = path.stat().st_size
    if not 0 < size <= MAX_FILE_BYTES or expected_size is not None and size != expected_size:
        raise AssetError('INPUT_SIZE_INVALID')
    digest = hashlib.sha256()
    read = 0
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1048576),b''):
            if check_active is not None:check_active()
            read += len(block)
            if read > MAX_FILE_BYTES:
                raise AssetError('INPUT_SIZE_INVALID')
            digest.update(block)
    if read != size:
        raise AssetError('INPUT_CHANGED')
    sha = digest.hexdigest()
    if expected_sha256 is not None and sha != expected_sha256:
        raise AssetError('INPUT_HASH_MISMATCH')
    return size,sha


def verify_decode(path: Path, expected_size: int | None = None, expected_sha256: str | None = None, *, check_active: Callable[[],None] | None = None) -> VerifiedSource:
    size,sha = verify_size(path,expected_size,expected_sha256,check_active=check_active)
    kind = verify_magic(path)
    verify_suffix(kind,path.suffix)
    source = VerifiedSource(path,kind,size,sha)
    from .inspect import read_metadata
    read_metadata(source)
    if kind != 'raw':
        from .inspect import open_raster
        import pyvips
        image = open_raster(path)
        try:
            for y in range(0,image.height,512):
                for x in range(0,image.width,512):
                    if check_active is not None:check_active()
                    image.crop(x,y,min(512,image.width-x),min(512,image.height-y)).write_to_memory()
        except pyvips.Error as exc:
            raise AssetError('IMAGE_DECODE_FAILED') from exc
    return source
