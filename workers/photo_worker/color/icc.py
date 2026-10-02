from __future__ import annotations

import ctypes as C
import ctypes.util
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import struct

import numpy as np


class ICCError(ValueError):
    pass


class xyY(C.Structure):
    _fields_ = [('x', C.c_double), ('y', C.c_double), ('Y', C.c_double)]


class Primaries(C.Structure):
    _fields_ = [('Red', xyY), ('Green', xyY), ('Blue', xyY)]


class LittleCMS:
    """Narrow LCMS2 API, with explicit RGB16 formats and owned handles."""
    RGB16 = (4 << 16) | (3 << 3) | 2

    def __init__(self, path: str | None = None):
        if path is None:
            path = os.environ.get('LCMS2_LIBRARY')
        if not path:
            import PIL
            bundled = sorted((Path(PIL.__file__).parent / '.dylibs').glob('liblcms2*.dylib'))
            path = str(bundled[0]) if bundled else ctypes.util.find_library('lcms2')
        if not path:
            raise ICCError('LITTLECMS_UNAVAILABLE')
        self.lib = C.CDLL(path)
        self.library_path = path
        signatures = {
            'cmsGetEncodedCMMversion': (C.c_uint32, []),
            'cmsBuildGamma': (C.c_void_p, [C.c_void_p, C.c_double]),
            'cmsFreeToneCurve': (None, [C.c_void_p]),
            'cmsCreateRGBProfile': (C.c_void_p, [C.POINTER(xyY), C.POINTER(Primaries), C.POINTER(C.c_void_p)]),
            'cmsCreate_sRGBProfile': (C.c_void_p, []),
            'cmsCloseProfile': (C.c_int, [C.c_void_p]),
            'cmsSaveProfileToMem': (C.c_int, [C.c_void_p, C.c_void_p, C.POINTER(C.c_uint32)]),
            'cmsOpenProfileFromMem': (C.c_void_p, [C.c_void_p, C.c_uint32]),
            'cmsGetColorSpace': (C.c_uint32, [C.c_void_p]),
            'cmsSetProfileVersion': (None, [C.c_void_p, C.c_double]),
            'cmsMD5computeID': (C.c_int, [C.c_void_p]),
            'cmsMLUalloc': (C.c_void_p, [C.c_void_p, C.c_uint32]),
            'cmsMLUsetASCII': (C.c_int, [C.c_void_p, C.c_char_p, C.c_char_p, C.c_char_p]),
            'cmsMLUfree': (None, [C.c_void_p]),
            'cmsWriteTag': (C.c_int, [C.c_void_p, C.c_uint32, C.c_void_p]),
            'cmsCreateTransform': (C.c_void_p, [C.c_void_p, C.c_uint32, C.c_void_p, C.c_uint32, C.c_uint32, C.c_uint32]),
            'cmsDoTransform': (None, [C.c_void_p, C.c_void_p, C.c_void_p, C.c_uint32]),
            'cmsDeleteTransform': (None, [C.c_void_p]),
        }
        for name, (restype, argtypes) in signatures.items():
            function = getattr(self.lib, name)
            function.restype, function.argtypes = restype, argtypes

    @property
    def version(self) -> int:
        return int(self.lib.cmsGetEncodedCMMversion())

    def serialize(self, profile) -> bytes:
        size = C.c_uint32()
        if not profile or not self.lib.cmsSaveProfileToMem(profile, None, C.byref(size)) or not 128 <= size.value <= 1048576:
            raise ICCError('ICC_SERIALIZATION_FAILED')
        buffer = C.create_string_buffer(size.value)
        if not self.lib.cmsSaveProfileToMem(profile, buffer, C.byref(size)):
            raise ICCError('ICC_SERIALIZATION_FAILED')
        return buffer.raw[:size.value]

    def open(self, data: bytes):
        validate_icc(data)
        buffer = C.create_string_buffer(data)
        handle = self.lib.cmsOpenProfileFromMem(buffer, len(data))
        if not handle:
            raise ICCError('ICC_INVALID')
        if self.lib.cmsGetColorSpace(handle) != int.from_bytes(b'RGB ', 'big'):
            self.lib.cmsCloseProfile(handle)
            raise ICCError('ICC_RGB_REQUIRED')
        # LCMS memory IO owns a copy; keep buffer until the handle is closed too.
        return handle, buffer

    def _tag(self, handle, signature: bytes, value: str):
        mlu = self.lib.cmsMLUalloc(None, 1)
        if not mlu:
            raise ICCError('ICC_ALLOCATION_FAILED')
        try:
            if not self.lib.cmsMLUsetASCII(mlu, b'en', b'US', value.encode('ascii')) or not self.lib.cmsWriteTag(handle, int.from_bytes(signature, 'big'), mlu):
                raise ICCError('ICC_TAG_FAILED')
        finally:
            self.lib.cmsMLUfree(mlu)

    def generate(self, profile_id: str) -> bytes:
        definitions = {
            'work_rec2020_gamma24_v1': (2.4, ((.708, .292), (.170, .797), (.131, .046))),
            'linear_srgb_v1': (1., ((.640, .330), (.300, .600), (.150, .060))),
        }
        if profile_id == 'srgb_v1':
            handle = self.lib.cmsCreate_sRGBProfile()
        elif profile_id in definitions:
            gamma, primaries = definitions[profile_id]
            curve = self.lib.cmsBuildGamma(None, gamma)
            if not curve:
                raise ICCError('ICC_TRC_FAILED')
            try:
                curves = (C.c_void_p * 3)(curve, curve, curve)
                white = xyY(.3127, .3290, 1.)
                colors = Primaries(*(xyY(x, y, 1.) for x, y in primaries))
                handle = self.lib.cmsCreateRGBProfile(C.byref(white), C.byref(colors), curves)
            finally:
                self.lib.cmsFreeToneCurve(curve)
        else:
            raise ICCError('ICC_PROFILE_UNKNOWN')
        if not handle:
            raise ICCError('ICC_CREATION_FAILED')
        try:
            self.lib.cmsSetProfileVersion(handle, 4.3)
            self._tag(handle, b'desc', profile_id)
            self._tag(handle, b'cprt', 'PhotoEditor generated numeric color profile; CC0-1.0')
            data = bytearray(self.serialize(handle))
        finally:
            self.lib.cmsCloseProfile(handle)
        # Normalize creation date/platform before LCMS recomputes the ICC ID.
        data[24:36] = struct.pack('>6H', 2026, 1, 1, 0, 0, 0)
        data[40:44] = b'\0' * 4
        data[84:100] = b'\0' * 16
        handle, buffer = self.open(bytes(data))
        try:
            if not self.lib.cmsMD5computeID(handle):
                raise ICCError('ICC_ID_FAILED')
            return self.serialize(handle)
        finally:
            self.lib.cmsCloseProfile(handle)

    def convert(self, image: np.ndarray, source: bytes, destination: bytes) -> np.ndarray:
        if image.dtype != np.uint16 or image.ndim < 2 or image.shape[-1] != 3:
            raise ICCError('ICC_RGB16_REQUIRED')
        pixels = int(np.prod(image.shape[:-1]))
        if not 0 < pixels <= 200_000_000:
            raise ICCError('PIXEL_LIMIT_EXCEEDED')
        a, buffer_a = self.open(source)
        b = transform = None
        try:
            b, buffer_b = self.open(destination)
            # Relative colorimetric intent=1; BPC off, NOOPTIMIZE prevents a coarse
            # precalculated CLUT from reducing RGB16 accuracy.
            transform = self.lib.cmsCreateTransform(a, self.RGB16, b, self.RGB16, 1, 0x0100)
            if not transform:
                raise ICCError('ICC_TRANSFORM_FAILED')
            contiguous = np.ascontiguousarray(image)
            output = np.empty_like(contiguous)
            self.lib.cmsDoTransform(transform, contiguous.ctypes.data, output.ctypes.data, pixels)
            return output
        finally:
            if transform:
                self.lib.cmsDeleteTransform(transform)
            if b:
                self.lib.cmsCloseProfile(b)
            self.lib.cmsCloseProfile(a)


def validate_icc(data: bytes) -> None:
    if not isinstance(data, bytes) or not 132 <= len(data) <= 1048576:
        raise ICCError('ICC_SIZE_INVALID')
    size = int.from_bytes(data[:4], 'big')
    if size != len(data) or data[36:40] != b'acsp' or data[16:20] != b'RGB ' or data[8] not in (2, 4):
        raise ICCError('ICC_HEADER_INVALID')
    count = int.from_bytes(data[128:132], 'big')
    if count > 4096 or 132 + count * 12 > size:
        raise ICCError('ICC_TAG_TABLE_INVALID')
    for index in range(count):
        _, offset, length = struct.unpack_from('>III', data, 132 + index * 12)
        if offset < 132 + count * 12 or length < 8 or offset + length > size:
            raise ICCError('ICC_TAG_RANGE_INVALID')


@dataclass(frozen=True)
class Profile:
    id: str
    sha256: str
    data: bytes


class ICCRegistry:
    def __init__(self, root: Path):
        self.root = root.resolve()
        self.manifest = json.loads((self.root / 'profiles.lock.json').read_text())

    def resolve(self, profile_id: str) -> Profile:
        entries = [p for p in self.manifest['profiles'] if p['id'] == profile_id]
        if len(entries) != 1:
            raise ICCError('ICC_PROFILE_UNKNOWN')
        entry = entries[0]
        candidate = self.root / entry['path']
        path = candidate.resolve()
        if not path.is_relative_to(self.root) or candidate.is_symlink():
            raise ICCError('ICC_PATH_INVALID')
        data = path.read_bytes()
        if len(data) != entry['bytes'] or hashlib.sha256(data).hexdigest() != entry['sha256']:
            raise ICCError('ICC_HASH_MISMATCH')
        validate_icc(data)
        return Profile(profile_id, entry['sha256'], data)
