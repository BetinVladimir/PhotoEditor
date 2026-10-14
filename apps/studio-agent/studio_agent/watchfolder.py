"""Bounded watch-folder scanner; never opens paths through symlinks."""

from __future__ import annotations

from dataclasses import replace

import hashlib
import errno
import os
import stat
import time
from pathlib import Path

from .journal import Entry, Journal, QueueFull
from .raw_policy import RAW_SUFFIXES, frozen_raw_policy


class WatchFolder:
    def __init__(self, root: Path, journal: Journal, device_id: str,
                 *, max_file_bytes: int = 100 * 1024**3,
                 max_queued_bytes: int = 100 * 1024**3,
                 raw_policy_json: str | None = None,
                 extensions: frozenset[str] = frozenset({
                     ".jpg", ".jpeg", ".png", ".tif", ".tiff", ".dng",
                     ".cr2", ".nef", ".arw", ".raf", ".orf", ".rw2"})):
        if root.is_symlink() or not root.is_dir() or max_file_bytes < 1 or max_queued_bytes < 1:
            raise ValueError("watch root must be a real directory")
        self.root = root.resolve(strict=True)
        root_fd = self._open_directory(())
        try:
            identity = os.fstat(root_fd)
            self._root_identity = (identity.st_dev, identity.st_ino)
        finally:
            os.close(root_fd)
        self.journal = journal
        self.device_id = device_id
        self.max_file_bytes = max_file_bytes
        self.max_queued_bytes = max_queued_bytes
        self.extensions = extensions
        if raw_policy_json is not None:
            frozen_raw_policy(raw_policy_json, "capture.dng")
        self.raw_policy_json = raw_policy_json

    def _open_directory(self, parts: tuple[str, ...]) -> int:
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
        fd = os.open(self.root.anchor, flags)
        try:
            for component in self.root.parts[1:]:
                next_fd = os.open(component, flags, dir_fd=fd)
                os.close(fd)
                fd = next_fd
            actual = os.fstat(fd)
            expected = getattr(self, "_root_identity", None)
            if expected is not None and (actual.st_dev, actual.st_ino) != expected:
                raise OSError(errno.ESTALE, "watch root identity changed")
            for part in parts:
                next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                  dir_fd=fd)
                os.close(fd)
                fd = next_fd
            return fd
        except BaseException:
            os.close(fd)
            raise

    def _files(self):
        pending = [()]
        while pending:
            parts = pending.pop()
            try:
                fd = self._open_directory(parts)
            except OSError as error:
                if error.errno not in (errno.ENOENT, errno.ENOTDIR, errno.ELOOP):
                    raise
                # A camera directory may disappear or become a symlink
                # between discovery and opening. Never follow its replacement.
                continue
            try:
                with os.scandir(fd) as entries:
                    for entry in entries:
                        if entry.is_symlink():
                            continue
                        child = (*parts, entry.name)
                        if entry.is_dir(follow_symlinks=False):
                            pending.append(child)
                        elif entry.is_file(follow_symlinks=False):
                            path = self.root.joinpath(*child)
                            if path.suffix.lower() in self.extensions:
                                yield path
            finally:
                os.close(fd)

    def _open_no_symlinks(self, relative_path: str) -> int:
        parts = Path(relative_path).parts
        if Path(relative_path).is_absolute() or not parts or any(part in ("", ".", "..") for part in parts):
            raise ValueError("unsafe relative path")
        fd = self._open_directory(parts[:-1])
        try:
            file_fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
            try:
                info = os.fstat(file_fd)
                if not stat.S_ISREG(info.st_mode):
                    raise ValueError("not a regular file")
                if info.st_nlink != 1:
                    raise ValueError("hardlinked source is outside watch-folder authority")
                return file_fd
            except BaseException:
                os.close(file_fd)
                raise
        finally:
            os.close(fd)

    @staticmethod
    def _hash_exact(fd: int, size: int) -> str | None:
        """Bound IO to the admitted size, even while a camera appends data."""
        digest = hashlib.sha256()
        remaining = size
        with os.fdopen(fd, "rb", buffering=0, closefd=False) as file:
            while remaining:
                chunk = file.read(min(1024 * 1024, remaining))
                if not chunk:
                    return None
                digest.update(chunk)
                remaining -= len(chunk)
            if file.read(1):
                return None
        return digest.hexdigest()

    def hash_stable(self, entry: Entry) -> str | None:
        if entry.state != "stable":
            raise ValueError("file is not stable")
        try:
            fd = self._open_no_symlinks(entry.relative_path)
        except (OSError, ValueError):
            return None
        try:
            before = os.fstat(fd)
            if before.st_size != entry.size or before.st_mtime_ns != entry.mtime_ns:
                return None
            digest = self._hash_exact(fd, entry.size)
            after = os.fstat(fd)
            if digest is None or (after.st_size, after.st_mtime_ns) != (entry.size, entry.mtime_ns):
                return None
            self.journal.transition(entry.device_id, entry.sequence, "stable", "hashed",
                                    sha256=digest,
                                    raw_policy_json=(self.raw_policy_json if
                                        Path(entry.relative_path).suffix.lower() in RAW_SUFFIXES else None))
            return digest
        finally:
            os.close(fd)

    def rehash_before_complete(self, entry: Entry) -> bool:
        if entry.state not in ("hashed", "uploading", "verified") or not entry.sha256:
            return False
        try:
            fd = self._open_no_symlinks(entry.relative_path)
        except (OSError, ValueError):
            return False
        try:
            before = os.fstat(fd)
            if (before.st_size, before.st_mtime_ns) != (entry.size, entry.mtime_ns):
                return False
            digest = self._hash_exact(fd, entry.size)
            after = os.fstat(fd)
            return (after.st_size, after.st_mtime_ns) == (entry.size, entry.mtime_ns) and digest == entry.sha256
        finally:
            os.close(fd)

    def scan(self, now: float | None = None, *, max_hashes: int | None = None) -> list[Entry]:
        observed_at = time.time() if now is None else now
        if max_hashes is not None and (type(max_hashes) is not int or not 1 <= max_hashes <= 100):
            raise ValueError("invalid hash batch size")
        ready = []
        for path in self._files():
            relative_path = path.relative_to(self.root).as_posix()
            try:
                fd = self._open_no_symlinks(relative_path)
                try:
                    info = os.fstat(fd)
                finally:
                    os.close(fd)
            except OSError as error:
                if error.errno == errno.ESTALE:
                    raise
                continue
            except ValueError:
                continue
            if not 0 < info.st_size <= self.max_file_bytes:
                continue
            try:
                entry = self.journal.observe(self.device_id, relative_path,
                                             info.st_size, info.st_mtime_ns,
                                             observed_at, self.max_queued_bytes)
            except QueueFull:
                continue
            if max_hashes is None and entry.state == "stable" and self.hash_stable(entry) is not None:
                updated = self.journal.get(entry.device_id, entry.sequence)
                assert updated is not None
                ready.append(updated)
        if max_hashes is not None:
            # All discoveries are durable before bounded expensive hash IO.
            # Failed reads are deferred so one changing file cannot starve peers.
            for entry in self.journal.stable_due(self.device_id, observed_at, max_hashes):
                if self.hash_stable(entry) is None:
                    self.journal.delay(entry.device_id, entry.sequence, observed_at + 5)
                    continue
                updated = self.journal.get(entry.device_id, entry.sequence)
                assert updated is not None
                ready.append(updated)
        return ready

    def retry_failed(self, sequence: int) -> Entry:
        entry = self.journal.get(self.device_id, sequence)
        if entry is None or entry.state != "error":
            raise ValueError("capture is not failed")
        if not self.rehash_before_complete(replace(entry, state="hashed")):
            raise ValueError("failed capture source is missing or changed")
        return self.journal.retry_error(self.device_id, sequence)
