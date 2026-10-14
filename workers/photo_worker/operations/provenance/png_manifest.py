"""Exact PNG caBX manifest-store binding (C2PA specification appendix A).

This checks container bytes only. It does not validate JUMBF, COSE certificates,
trust or signatures; SDK and independent certificate verification still apply.
"""
from struct import unpack
from zlib import crc32


PNG_SIGNATURE = b'\x89PNG\r\n\x1a\n'


def verify_png_manifest(stream, manifest, *, check_active=lambda: None):
    """Compare exactly one CRC-valid caBX with the returned binary store.

    Scan the entire bounded container without allocating image data. Restore
    caller position on success/failure; reject trailing data and duplicate stores.
    """
    if (not isinstance(manifest, bytes) or not 1 <= len(manifest) <= 8 << 20
            or not callable(check_active)):
        raise ValueError('SIGNER_PNG_MANIFEST_INPUT')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    position = stream.tell()
    consumed, count, found = 0, 0, False
    def read_exact(size):
        nonlocal consumed
        guard()
        if consumed + size > 512 << 20:
            raise ValueError('SIGNER_PNG_FILE_LIMIT')
        data = stream.read(size)
        guard()
        if not isinstance(data, bytes) or len(data) != size:
            raise ValueError('SIGNER_PNG_TRUNCATED')
        consumed += size
        return data
    try:
        stream.seek(0)
        if read_exact(8) != PNG_SIGNATURE:
            raise ValueError('SIGNER_PNG_SIGNATURE')
        while True:
            count += 1
            if count > 100000:
                raise ValueError('SIGNER_PNG_CHUNK_LIMIT')
            length, kind = unpack('>I4s', read_exact(8))
            if length > (1 << 31)-1 or any(not (65 <= c <= 90 or 97 <= c <= 122) for c in kind):
                raise ValueError('SIGNER_PNG_CHUNK')
            if count == 1 and (kind != b'IHDR' or length != 13):
                raise ValueError('SIGNER_PNG_HEADER')
            if count > 1 and kind == b'IHDR':
                raise ValueError('SIGNER_PNG_HEADER')
            is_manifest = kind == b'caBX'
            if is_manifest and (found or length != len(manifest)):
                raise ValueError('SIGNER_PNG_MANIFEST_BINDING')
            if kind == b'IEND' and length != 0:
                raise ValueError('SIGNER_PNG_END')
            checksum, offset = crc32(kind), 0
            while offset < length:
                block = read_exact(min(1 << 20, length-offset))
                checksum = crc32(block, checksum)
                if is_manifest and block != manifest[offset:offset+len(block)]:
                    raise ValueError('SIGNER_PNG_MANIFEST_BINDING')
                offset += len(block)
            if unpack('>I', read_exact(4))[0] != checksum & 0xffffffff:
                raise ValueError('SIGNER_PNG_CRC')
            if is_manifest:
                found = True
            if kind == b'IEND':
                guard()
                trailing=stream.read(1)
                guard()
                if trailing != b'':
                    raise ValueError('SIGNER_PNG_TRAILING_DATA')
                if not found:
                    raise ValueError('SIGNER_PNG_MANIFEST_MISSING')
                return
    finally:
        stream.seek(position)
