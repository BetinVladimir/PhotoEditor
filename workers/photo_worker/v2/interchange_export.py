"""Deterministic streaming PhotoEditor ZIP64 bundle writer."""
from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO
from typing import BinaryIO, Callable, ContextManager
import zipfile

from photo_worker.contracts.base import canonical_bytes
from .interchange_archive import ArchiveLimits, VerifiedArchiveFile, interchange_path
from .interchange_manifest import BundleManifest, verify_bundle_manifest


@dataclass(frozen=True)
class ExportFile:
    path: str
    bytes: int
    sha256: str
    sidecar: bool


class _QuotaWriter:
    def __init__(self, output, maximum):
        self.output, self.maximum = output, maximum

    def write(self, value):
        if self.tell() + len(value) > self.maximum:
            raise ValueError("interchange archive quota exceeded")
        written = self.output.write(value)
        if written != len(value):
            raise OSError("short archive write")
        return written

    def tell(self):
        return self.output.tell()

    def seek(self, *args):
        return self.output.seek(*args)

    def flush(self):
        self.output.flush()


def export_bundle(output: BinaryIO, manifest: BundleManifest, files: tuple[ExportFile, ...],
                  open_source: Callable[[str], ContextManager[BinaryIO]], limits: ArchiveLimits,
                  active: Callable[[], None] = lambda: None) -> int:
    """Write to an empty caller-owned seekable scratch file, never user paths.

    open_source must resolve paths against the already authorized frozen export
    plan. This byte writer verifies length/SHA, not tenant/recipe authority. Any
    failure invalidates the entire output; caller must delete it and not stage it.
    Returns final archive bytes, including ZIP metadata, after successful close.
    """
    limits.validate()
    active()
    output.seek(0, 2)
    if output.tell() != 0:
        raise ValueError("archive destination must be empty")
    raw = canonical_bytes(manifest)
    records = []
    for item in files:
        interchange_path(item.path)
        if type(item.bytes) is not int or item.bytes < 0 or type(item.sidecar) is not bool:
            raise ValueError("invalid frozen archive file")
        records.append(VerifiedArchiveFile(item.path, item.bytes, item.sha256))
    records.append(VerifiedArchiveFile("manifest.json", len(raw), sha256(raw).hexdigest()))
    # Reject stale inventory and collisions before opening a single source.
    verify_bundle_manifest(raw, tuple(records))
    if len(records) > limits.files or sum(item.bytes for item in records) > limits.expanded_bytes:
        raise ValueError("interchange expansion quota exceeded")
    by_path = {item.path: item for item in files}
    paths = sorted([*by_path, "manifest.json"])
    sink = _QuotaWriter(output, limits.archive_bytes)
    with zipfile.ZipFile(sink, "w", allowZip64=True, compression=zipfile.ZIP_STORED) as archive:
        for path in paths:
            active()
            is_manifest = path == "manifest.json"
            item = ExportFile(path, len(raw), sha256(raw).hexdigest(), True) if is_manifest else by_path[path]
            info = zipfile.ZipInfo(path, date_time=(1980, 1, 1, 0, 0, 0))
            info.create_system = 0
            info.external_attr = 0x20  # regular DOS archive file; no UID/GID
            info.compress_type = zipfile.ZIP_DEFLATED if item.sidecar else zipfile.ZIP_STORED
            info._compresslevel = 6
            info.file_size = item.bytes
            digest = sha256()
            count = 0

            def copy(source):
                nonlocal count
                with archive.open(info, "w", force_zip64=True) as target:
                    while True:
                        active()
                        chunk = source.read(min(1 << 20, item.bytes - count + 1))
                        if not chunk:
                            break
                        if not isinstance(chunk, bytes):
                            raise ValueError("invalid frozen source stream")
                        count += len(chunk)
                        if count > item.bytes:
                            raise ValueError("frozen source size mismatch")
                        digest.update(chunk)
                        if target.write(chunk) != len(chunk):
                            raise OSError("short ZIP entry write")
                if count != item.bytes or digest.hexdigest() != item.sha256:
                    raise ValueError("frozen source hash/size mismatch")

            if is_manifest:
                copy(BytesIO(raw))
            else:
                with open_source(path) as source:
                    copy(source)
            if info.file_size > 100 * info.compress_size:
                raise ValueError("export compression ratio exceeded")
        if sum(info.file_size for info in archive.filelist) > 100 * sum(info.compress_size for info in archive.filelist):
            raise ValueError("export compression ratio exceeded")
    active()
    return output.tell()
