"""Deployment-pinned runtime file integrity, complementary to live authority.

This proves file bytes only, never that a shared library is loaded or trusted.
The independent inspector must attest loaded handles, trust and network policy.
"""
import os
import stat
from hashlib import sha256
from pathlib import Path

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from .cancellation import check_signing_active


def _trusted_runtime_owner(owner, effective=None):
    if effective is None:
        effective = os.geteuid()
    return owner in (0, effective)


def _open_runtime_file(path):
    parts = path.parts
    if not path.is_absolute() or '..' in parts or len(parts) < 2:
        raise ValueError('SIGNER_RUNTIME_PATH')
    check_signing_active()
    directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        check_signing_active()
        root = os.fstat(directory)
        if (not stat.S_ISDIR(root.st_mode) or root.st_mode & 0o022
                or not _trusted_runtime_owner(root.st_uid)):
            raise ValueError('SIGNER_RUNTIME_DIRECTORY')
        for part in parts[1:-1]:
            check_signing_active()
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=directory)
            try:
                info = os.fstat(child)
                if (not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022
                        or not _trusted_runtime_owner(info.st_uid)):
                    raise ValueError('SIGNER_RUNTIME_DIRECTORY')
            except BaseException:
                os.close(child)
                raise
            os.close(directory)
            directory = child
        check_signing_active()
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=directory)
        try:
            check_signing_active()
        except BaseException:
            os.close(descriptor)
            raise
        return directory, descriptor
    except BaseException:
        os.close(directory)
        raise


class PinnedRuntimeFiles:
    def __init__(self, lock, sdk_lock_sha, files):
        # Logical names in the lock are stable across deployment directories.
        # Paths are process provisioning inputs, never claim/task fields.
        if (not isinstance(lock, dict) or set(lock) != {'schema_version', 'files'}
                or lock['schema_version'] != 'signer-runtime-files/v1'
                or not isinstance(lock['files'], list) or not 1 <= len(lock['files']) <= 256
                or not isinstance(files, dict) or sha256(canonical_bytes(lock)).hexdigest() != sdk_lock_sha):
            raise ValueError('SIGNER_RUNTIME_LOCK')
        owned = {}
        for entry in lock['files']:
            if not isinstance(entry, dict) or set(entry) != {'name', 'sha256', 'size_bytes'}:
                raise ValueError('SIGNER_RUNTIME_LOCK')
            name, digest, size = entry['name'], entry['sha256'], entry['size_bytes']
            if (not isinstance(name, str) or not 1 <= len(name) <= 200 or name in owned
                    or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-' for c in name)
                    or not isinstance(digest, str) or len(digest) != 64
                    or any(c not in '0123456789abcdef' for c in digest)
                    or type(size) is not int or not 1 <= size <= 512 << 20):
                raise ValueError('SIGNER_RUNTIME_LOCK')
            path = files.get(name)
            if not isinstance(path, (str, Path)) or not Path(path).is_absolute():
                raise ValueError('SIGNER_RUNTIME_PATH')
            owned[name] = (Path(path), digest, size)
        if set(files) != set(owned) or sum(item[2] for item in owned.values()) > 2 << 30:
            raise ValueError('SIGNER_RUNTIME_LOCK')
        self.sdk_lock_sha = sdk_lock_sha
        self._files = tuple(owned.values())

    def check(self):
        for path, expected, size in self._files:
            check_signing_active()
            # Reject symlinked directories as well as symlinked leaf files.
            # Deployment owns directory permissions; comparison after open
            # detects replacement during path traversal/read.
            if path.resolve(strict=True) != path:
                raise ValueError('SIGNER_RUNTIME_PATH')
            directory, descriptor = _open_runtime_file(path)
            try:
                before = os.fstat(descriptor)
                if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                        or not _trusted_runtime_owner(before.st_uid)
                        or before.st_size != size or before.st_mode & 0o022):
                    raise ValueError('SIGNER_RUNTIME_FILE')
                digest = sha256()
                remaining = size
                while remaining:
                    check_signing_active()
                    chunk = os.read(descriptor, min(1 << 20, remaining))
                    check_signing_active()
                    if not chunk:
                        raise ValueError('SIGNER_RUNTIME_FILE')
                    digest.update(chunk)
                    remaining -= len(chunk)
                check_signing_active()
                trailing = os.read(descriptor, 1)
                check_signing_active()
                if trailing:
                    raise ValueError('SIGNER_RUNTIME_FILE')
                after = os.fstat(descriptor)
                check_signing_active()
                current = os.stat(path.name, dir_fd=directory, follow_symlinks=False)
                external = path.stat(follow_symlinks=False)
                check_signing_active()
                identity = lambda item: (item.st_dev, item.st_ino, item.st_size,
                                         item.st_mtime_ns, item.st_ctime_ns, item.st_mode, item.st_nlink, item.st_uid)
                if (identity(before) != identity(after) or identity(after) != identity(current)
                        or identity(current) != identity(external)
                        or path.resolve(strict=True) != path or digest.hexdigest() != expected):
                    raise ValueError('SIGNER_RUNTIME_CHANGED')
            finally:
                os.close(descriptor)
                os.close(directory)
        check_signing_active()
        return True


class FileBoundInstallationInspector:
    def __init__(self, runtime_files, live_inspector):
        if not isinstance(runtime_files, PinnedRuntimeFiles) or not callable(live_inspector):
            raise ValueError('SIGNER_RUNTIME_INSPECTOR')
        self._files, self._live = runtime_files, live_inspector

    def __call__(self, policy, algorithm, certificate_chain):
        if not isinstance(policy, SigningPolicy) or policy.sdk_lock_sha != self._files.sdk_lock_sha:
            raise ValueError('SIGNER_RUNTIME_POLICY')
        owned = policy.model_copy(deep=True)
        before = canonical_bytes(owned)
        self._files.check()
        accepted = self._live(owned.model_copy(deep=True), algorithm, certificate_chain)
        self._files.check()
        if canonical_bytes(policy) != before:
            raise ValueError('SIGNER_RUNTIME_POLICY_CHANGED')
        return accepted is True
