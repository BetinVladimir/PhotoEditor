"""Bounded watch-folder scanner; never opens paths through symlinks."""

from __future__ import annotations

from dataclasses import replace

import hashlib
import os
import stat
import time
from pathlib import Path

from .journal import Entry, Journal, QueueFull


class WatchFolder:
    def __init__(self, root: Path, journal: Journal, device_id: str,
                 *, max_file_bytes: int = 100 * 1024**3,
                 max_queued_bytes: int = 100 * 1024**3,
                 extensions: frozenset[str] = frozenset({
                     ".jpg", ".jpeg", ".png", ".tif", ".tiff", ".dng",
                     ".cr2", ".nef", ".arw", ".raf", ".orf", ".rw2"})):
        if root.is_symlink() or not root.is_dir() or max_file_bytes < 1 or max_queued_bytes < 1:
            raise ValueError("watch root must be a real directory")
        self.root = root.resolve(strict=True)
        self.journal = journal
        self.device_id = device_id
        self.max_file_bytes = max_file_bytes
        self.max_queued_bytes = max_queued_bytes
        self.extensions = extensions

    def _files(self):
        pending = [self.root]
        while pending:
            directory = pending.pop()
            with os.scandir(directory) as entries:
                for entry in entries:
                    if entry.is_symlink():
                        continue
                    if entry.is_dir(follow_symlinks=False):
                        pending.append(Path(entry.path))
                    elif entry.is_file(follow_symlinks=False):
                        path = Path(entry.path)
                        if path.suffix.lower() in self.extensions:
                            yield path

    def _open_no_symlinks(self, relative_path: str) -> int:
        parts = Path(relative_path).parts
        if Path(relative_path).is_absolute() or not parts or any(part in ("", ".", "..") for part in parts):
            raise ValueError("unsafe relative path")
        fd = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for part in parts[:-1]:
                next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                os.close(fd)
                fd = next_fd
            file_fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW, dir_fd=fd)
            if not stat.S_ISREG(os.fstat(file_fd).st_mode):
                os.close(file_fd)
                raise ValueError("not a regular file")
            return file_fd
        finally:
            os.close(fd)

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
            digest = hashlib.sha256()
            with os.fdopen(fd, "rb", closefd=False) as file:
                while chunk := file.read(1024 * 1024):
                    digest.update(chunk)
            after = os.fstat(fd)
            if (after.st_size, after.st_mtime_ns) != (entry.size, entry.mtime_ns):
                return None
            self.journal.transition(entry.device_id, entry.sequence, "stable", "hashed",
                                    sha256=digest.hexdigest())
            return digest.hexdigest()
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
            digest = hashlib.sha256()
            with os.fdopen(fd, "rb", closefd=False) as file:
                while chunk := file.read(1024 * 1024):
                    digest.update(chunk)
            after = os.fstat(fd)
            return (after.st_size, after.st_mtime_ns) == (entry.size, entry.mtime_ns) and digest.hexdigest() == entry.sha256
        finally:
            os.close(fd)

    def scan(self, now: float | None = None) -> list[Entry]:
        observed_at = time.time() if now is None else now
        ready = []
        for path in self._files():
            try:
                info = path.stat(follow_symlinks=False)
            except FileNotFoundError:
                continue
            if not 0 < info.st_size <= self.max_file_bytes:
                continue
            relative_path = path.relative_to(self.root).as_posix()
            try:
                entry = self.journal.observe(self.device_id, relative_path,
                                             info.st_size, info.st_mtime_ns,
                                             observed_at, self.max_queued_bytes)
            except QueueFull:
                continue
            if entry.state == "stable" and self.hash_stable(entry) is not None:
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
