"""Read-only isolated-restore object verification; never authorizes writers.

The operator adapter must open the exact immutable bucket/key/version supplied
by restored metadata. It must not fall back to the current object or repair a
missing reference. Database, deletion-journal and lease reconciliation are
separate mandatory restore stages.
"""
from dataclasses import dataclass
from hashlib import sha256
import re
from typing import BinaryIO, Callable, ContextManager, Iterable

from photo_worker.contracts.base import canonical_bytes
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreObject:
    bucket: str
    key: str
    version: str
    sha256: str
    size: int


@dataclass(frozen=True)
class RestoreInventoryEvidence:
    schema_version: str
    objects: int
    bytes_verified: int
    inventory_sha256: str


def _validate(ref):
    if (not isinstance(ref, RestoreObject)
            or any(not isinstance(value, str) or not value or len(value.encode()) > limit
                   or any(ord(char) < 32 for char in value)
                   for value, limit in ((ref.bucket, 63), (ref.key, 1024), (ref.version, 1024)))
            or ref.version == 'null'
            or not isinstance(ref.sha256, str)
            or re.fullmatch(r'[0-9a-f]{64}', ref.sha256) is None
            or type(ref.size) is not int or not 0 <= ref.size <= 2**53 - 1):
        raise ValueError('RESTORE_INVENTORY_INVALID')


def verify_restore_inventory(
        references: Iterable[RestoreObject],
        open_version: Callable[[RestoreObject], ContextManager[BinaryIO]], *,
        check_active: Callable[[], None], maximum_objects: int = 10_000_000,
        maximum_bytes: int = 2**53 - 1) -> RestoreInventoryEvidence:
    """Hash bounded chunks with constant working memory, exact sorted catalogue.

    Ordering is UTF-8 (bucket, key, version); duplicate physical versions refuse.
    Inventory digest is SHA256(domain + canonical JSON row + LF for each row).
    A complete return proves only that all supplied referenced bytes matched.
    It does not prove that the database supplied every active artifact, that
    extra objects were quarantined, or that deletion history was reconciled.
    """
    if (not callable(open_version) or not callable(check_active)
            or type(maximum_objects) is not int or not 1 <= maximum_objects <= 10_000_000
            or type(maximum_bytes) is not int or not 0 <= maximum_bytes <= 2**53 - 1):
        raise ValueError('RESTORE_INVENTORY_LIMITS')
    require_restore_active(check_active)
    inventory = sha256(b'photoeditor.restore-object-inventory/v1\n')
    count = total = 0
    previous = None
    for ref in references:
        require_restore_active(check_active)
        _validate(ref)
        identity = tuple(value.encode('utf-8') for value in (ref.bucket, ref.key, ref.version))
        if (previous is not None and identity <= previous
                or count >= maximum_objects or ref.size > maximum_bytes - total):
            raise ValueError('RESTORE_INVENTORY_INVALID')
        digest = sha256()
        received = 0
        with open_version(ref) as stream:
            while True:
                require_restore_active(check_active)
                # Read one extra byte to detect excess without unbounded reads.
                data = stream.read(min(1 << 20, ref.size - received + 1))
                require_restore_active(check_active)
                if not isinstance(data, bytes):
                    raise ValueError('RESTORE_OBJECT_STREAM_INVALID')
                if not data:
                    break
                received += len(data)
                if received > ref.size:
                    raise ValueError('RESTORE_OBJECT_SIZE_MISMATCH')
                digest.update(data)
        if received != ref.size or digest.hexdigest() != ref.sha256:
            raise ValueError('RESTORE_OBJECT_CONTENT_MISMATCH')
        require_restore_active(check_active)
        inventory.update(canonical_bytes({
            'bucket': ref.bucket, 'key': ref.key, 'version': ref.version,
            'sha256': ref.sha256, 'size': ref.size}))
        inventory.update(b'\n')
        previous = identity
        count += 1
        total += received
    require_restore_active(check_active)
    return RestoreInventoryEvidence('restore-object-inventory/v1', count, total,
                                    inventory.hexdigest())
