"""Bounded, SHA-pinned preparation of a normalized metadata document.

Caller authenticates original artifact/version and approval. Preparation is
read-only and does not publish/adopt an artifact or reserve SQL authority.
"""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import stat
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from .delivery_metadata_extract import extract_delivery_metadata
from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES,read_raw_metadata

# Match metadata.write compressed input admission before any file or object IO.
MAX_METADATA_ORIGINAL_BYTES = 2 * 1024**3


@dataclass(frozen=True)
class PreparedMetadataSource:
    original_jcs: bytes
    original_bytes: int
    metadata_jcs: bytes
    metadata_sha256: str


def prepare_delivery_metadata_source(path,original,mime,expected_bytes,*,max_bytes,include_gps=False,
                                     raw_executable=None,raw_tool_sha=None,check_active=lambda:None):
    check_active()
    path=Path(path)
    if (not path.is_absolute() or not isinstance(original,ArtifactRef) or original.artifact_id.int==0
            or mime not in ('image/jpeg','image/png','image/tiff') and mime not in RAW_METADATA_FILETYPES
            or type(expected_bytes) is not int or type(max_bytes) is not int
            or not 1<=expected_bytes<=min(max_bytes,MAX_METADATA_ORIGINAL_BYTES) or not 1<=max_bytes<=2**53-1 or type(include_gps) is not bool):
        raise ValueError('DELIVERY_METADATA_SOURCE_INPUT')
    original=ArtifactRef.model_validate_json(canonical_bytes(original))
    if path.resolve(strict=True)!=path:
        raise ValueError('DELIVERY_METADATA_SOURCE_PATH')
    descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        before=os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode) or before.st_nlink!=1 or before.st_size!=expected_bytes:
            raise ValueError('DELIVERY_METADATA_SOURCE_FILE')
        with os.fdopen(descriptor,'rb',closefd=False) as stream:
            digest=sha256();count=0
            while True:
                check_active();block=stream.read(min(1048576,expected_bytes-count+1))
                if not block:break
                count+=len(block)
                if count>expected_bytes:raise ValueError('DELIVERY_METADATA_SOURCE_SIZE')
                digest.update(block)
            if count!=expected_bytes or digest.hexdigest()!=original.sha256:
                raise ValueError('DELIVERY_METADATA_SOURCE_SHA')
            stream.seek(0)
            if mime in RAW_METADATA_FILETYPES:
                if raw_executable is None:raise ValueError('DELIVERY_METADATA_RAW_TOOL_UNAVAILABLE')
                document=read_raw_metadata(raw_executable,path,mime,expected_tool_sha=raw_tool_sha,
                                           include_gps=include_gps,
                                           check_active=check_active)
            else:
                with Image.open(stream) as image:
                    if (image.format!={'image/jpeg':'JPEG','image/png':'PNG','image/tiff':'TIFF'}[mime]
                            or getattr(image,'n_frames',1)!=1 or image.width<1 or image.height<1
                            or image.width*image.height>200000000):
                        raise ValueError('DELIVERY_METADATA_SOURCE_FORMAT')
                    document=extract_delivery_metadata(image,include_gps=include_gps,check_active=check_active)
        identity=lambda item:(item.st_dev,item.st_ino,item.st_size,item.st_mtime_ns,item.st_ctime_ns,item.st_mode,item.st_nlink)
        if identity(before)!=identity(os.fstat(descriptor)) or identity(before)!=identity(path.stat(follow_symlinks=False)) or path.resolve(strict=True)!=path:
            raise ValueError('DELIVERY_METADATA_SOURCE_CHANGED')
        check_active()
        raw=canonical_bytes(document)
        return PreparedMetadataSource(canonical_bytes(original),expected_bytes,raw,sha256(raw).hexdigest())
    finally:
        os.close(descriptor)
