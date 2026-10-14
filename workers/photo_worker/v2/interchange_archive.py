"""Bounded ZIP/ZIP64 byte verification for untrusted interchange bundles.

No filesystem paths are opened from archive names. Optional staging is supplied
by the caller and must be discarded if verification fails. Returned inventory
is byte evidence, not canonical image/recipe authority or an adoption receipt.
"""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import BinaryIO, Callable, ContextManager
import stat
import struct
import unicodedata
import zipfile


class InvalidInterchangeArchive(ValueError):
    pass


@dataclass(frozen=True)
class ArchiveLimits:
    archive_bytes: int
    expanded_bytes: int
    files: int

    def validate(self) -> None:
        for value, maximum in ((self.archive_bytes, 64 << 30),
                               (self.expanded_bytes, 128 << 30),
                               (self.files, 30000)):
            if type(value) is not int or not 1 <= value <= maximum:
                raise InvalidInterchangeArchive("invalid archive reservation")


@dataclass(frozen=True)
class VerifiedArchiveFile:
    path: str
    bytes: int
    sha256: str


def interchange_path(value: str) -> str:
    if (not isinstance(value, str) or not 1 <= len(value) <= 240
            or unicodedata.normalize("NFC", value) != value
            or any(c in value for c in ("\\", ":", "\x00"))
            or any(ord(c) < 32 or ord(c) == 127 for c in value)
            or any(part in ("", ".", "..") for part in value.split("/"))):
        raise InvalidInterchangeArchive("invalid archive path")
    try:
        value.encode("utf-8", errors="strict")
    except UnicodeError as exc:
        raise InvalidInterchangeArchive("invalid archive path encoding") from exc
    return value


def _metadata_budget(source: BinaryIO, physical: int, max_files: int) -> None:
    # ZipFile eagerly reads the central directory. Bound it before constructing
    # that parser so attacker-controlled metadata cannot exhaust worker RAM.
    source.seek(max(0, physical - 65557))
    tail = source.read(65557)
    pos = tail.rfind(b"PK\x05\x06")
    if pos < 0 or pos + 22 > len(tail):
        raise InvalidInterchangeArchive("missing ZIP directory")
    _, disk, directory_disk, disk_files, files, size, offset, comment = struct.unpack_from("<4s4H2IH", tail, pos)
    end = physical - len(tail) + pos
    if pos + 22 + comment != len(tail) or disk or directory_disk or disk_files != files:
        raise InvalidInterchangeArchive("unsupported ZIP directory")
    if files == 65535 or size == 0xffffffff or offset == 0xffffffff:
        if end < 20:
            raise InvalidInterchangeArchive("missing ZIP64 directory")
        source.seek(end - 20)
        locator = source.read(20)
        sig, locator_disk, location, disks = struct.unpack("<4sIQI", locator)
        if sig != b"PK\x06\x07" or locator_disk or disks != 1 or location > end - 76:
            raise InvalidInterchangeArchive("invalid ZIP64 locator")
        source.seek(location)
        record = source.read(56)
        sig, length, _, _, disk, directory_disk, disk_files, files, size, offset = struct.unpack("<4sQ2H2I4Q", record)
        if sig != b"PK\x06\x06" or length < 44 or location + 12 + length != end - 20 or disk or directory_disk or disk_files != files:
            raise InvalidInterchangeArchive("invalid ZIP64 directory")
        end = location
    if not 1 <= files <= max_files or size > 64 << 20 or offset + size != end:
        raise InvalidInterchangeArchive("ZIP directory budget exceeded")


def verify_archive(source: BinaryIO, limits: ArchiveLimits, *,
                   stage: Callable[[str], ContextManager[BinaryIO]] | None = None,
                   cancelled: Callable[[], bool] = lambda: False,
                   ) -> tuple[VerifiedArchiveFile, ...]:
    """Verify every file through bounded reads; never trust declared totals.

    source must be seekable and remains caller-owned. stage receives a validated
    name only and owns temporary bytes, never a destination selected by a ZIP.
    Its context must provide a binary writer. Cancellation/CRC/hash/limit errors
    invalidate the entire staged batch, including previously finished files.
    """
    limits.validate()
    source.seek(0, 2)
    physical_bytes = source.tell()
    if not 1 <= physical_bytes <= limits.archive_bytes:
        raise InvalidInterchangeArchive("archive byte quota exceeded")
    try:
        _metadata_budget(source, physical_bytes, limits.files)
    except struct.error as exc:
        raise InvalidInterchangeArchive("truncated ZIP metadata") from exc
    source.seek(0)
    result: list[VerifiedArchiveFile] = []
    expanded = compressed = 0
    try:
        with zipfile.ZipFile(source, "r", allowZip64=True) as archive:
            entries = archive.infolist()
            if not 1 <= len(entries) <= limits.files:
                raise InvalidInterchangeArchive("archive file quota exceeded")
            names: set[str] = set()
            for info in entries:
                path = interchange_path(info.orig_filename)
                if info.filename != info.orig_filename or path.casefold() in names:
                    raise InvalidInterchangeArchive("duplicate archive path")
                names.add(path.casefold())
                mode = info.external_attr >> 16
                kind = stat.S_IFMT(mode)
                if (info.is_dir() or info.external_attr & 0x10
                        or kind not in (0, stat.S_IFREG)
                        or info.flag_bits & (1 | 0x40 | 0x2000)
                        or info.compress_type not in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED)):
                    raise InvalidInterchangeArchive("unsupported archive file")
                if (info.file_size < 0 or info.compress_size < 0
                        or info.compress_size > physical_bytes
                        or info.file_size > limits.expanded_bytes - expanded
                        or info.file_size > 100 * info.compress_size):
                    raise InvalidInterchangeArchive("archive expansion quota exceeded")
                digest = sha256()
                count = 0

                def consume(output: BinaryIO | None) -> None:
                    nonlocal count, expanded
                    with archive.open(info, "r") as content:
                        while True:
                            if cancelled():
                                raise InterruptedError("interchange cancelled")
                            chunk = content.read(1 << 20)
                            if not chunk:
                                break
                            count += len(chunk)
                            expanded += len(chunk)
                            if (expanded > limits.expanded_bytes
                                    or count > 100 * info.compress_size
                                    or count > info.file_size):
                                raise InvalidInterchangeArchive("archive expansion quota exceeded")
                            digest.update(chunk)
                            if output is not None:
                                # A partial write must not be silently hashed as stored.
                                if output.write(chunk) != len(chunk):
                                    raise OSError("short interchange staging write")
                    if count != info.file_size:
                        raise InvalidInterchangeArchive("archive size mismatch")

                if stage is None:
                    consume(None)
                else:
                    with stage(path) as output:
                        consume(output)
                compressed += info.compress_size
                if expanded > 100 * compressed:
                    raise InvalidInterchangeArchive("archive compression ratio exceeded")
                result.append(VerifiedArchiveFile(path, count, digest.hexdigest()))
    except (zipfile.BadZipFile, NotImplementedError, RuntimeError, EOFError) as exc:
        raise InvalidInterchangeArchive("invalid ZIP archive") from exc
    return tuple(sorted(result, key=lambda item: item.path))
