"""Offline integrity gate for the SDK wheel selected by execution contract 15.

This receipt authenticates distribution bytes only. It does not attest an
installed/loaded SDK, transitive dependencies, native handles or HSM qualification.
No archive extraction, import, installation or network access occurs here.
"""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import stat

SIGNER_SDK_VERSION = '0.32.4'
SIGNER_SDK_WHEEL_SHA256 = 'a98a594cf79655e3f29987f40bcc5a601de382d352a567bba6601cd4a5322c56'
SIGNER_SDK_WHEEL_MAX_BYTES = 512 << 20


@dataclass(frozen=True)
class SignerSDKWheelReceipt:
    schema_version: str
    distribution: str
    version: str
    target: str
    sha256: str
    size_bytes: int
    qualification: str = 'not_measured'


def _require_active(check_active):
    if check_active() is False:
        raise RuntimeError('SIGNER_TASK_INACTIVE')


def verify_signer_sdk_wheel(path, *, check_active=lambda: None):
    """Stream and recheck a deployment-owned official wheel before installation.

    The expected version/digest are constants from the approved project plan;
    neither command arguments nor a colocated checksum file choose the pin.
    Verification is portable; installation targets Linux x86_64 exclusively.
    The installer must independently enforce the same hash on its own read.
    """
    if not callable(check_active) or not isinstance(path, (str, Path)):
        raise ValueError('SIGNER_SDK_WHEEL_INPUT')
    _require_active(check_active)
    path = Path(path)
    if (not path.is_absolute() or path.suffix != '.whl'
            or path.resolve(strict=True) != path):
        raise ValueError('SIGNER_SDK_WHEEL_PATH')
    # Resolve each ancestor through a pinned directory descriptor. O_NOFOLLOW
    # on a full filename protects only the final component.
    directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in path.parts[1:-1]:
            _require_active(check_active)
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=directory)
            os.close(directory)
            directory = child
        _require_active(check_active)
        descriptor = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=directory)
        try:
            return _verify_open_wheel(descriptor, directory, path, check_active)
        finally:
            os.close(descriptor)
    finally:
        os.close(directory)


def _verify_open_wheel(descriptor, directory, path, check_active):
    _require_active(check_active)
    before = os.fstat(descriptor)
    if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
            or before.st_mode & 0o022
            or not 1 <= before.st_size <= SIGNER_SDK_WHEEL_MAX_BYTES):
        raise ValueError('SIGNER_SDK_WHEEL_FILE')
    checksum = sha256()
    remaining = before.st_size
    while remaining:
        _require_active(check_active)
        block = os.read(descriptor, min(1 << 20, remaining))
        _require_active(check_active)
        if not block:
            raise ValueError('SIGNER_SDK_WHEEL_TRUNCATED')
        checksum.update(block)
        remaining -= len(block)
    _require_active(check_active)
    trailing = os.read(descriptor, 1)
    _require_active(check_active)
    if trailing:
        raise ValueError('SIGNER_SDK_WHEEL_CHANGED')
    after = os.fstat(descriptor)
    _require_active(check_active)
    current = os.stat(path.name, dir_fd=directory, follow_symlinks=False)
    _require_active(check_active)
    def identity(info):
        return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
                info.st_ctime_ns, info.st_mode, info.st_nlink)
    if (identity(before) != identity(after) or identity(after) != identity(current)
            or path.resolve(strict=True) != path):
        raise ValueError('SIGNER_SDK_WHEEL_CHANGED')
    if checksum.hexdigest() != SIGNER_SDK_WHEEL_SHA256:
        raise ValueError('SIGNER_SDK_WHEEL_SHA256')
    _require_active(check_active)
    return SignerSDKWheelReceipt('signer-sdk-wheel/v1', 'c2pa-python',
        SIGNER_SDK_VERSION, 'manylinux_2_28_x86_64', checksum.hexdigest(), before.st_size)
