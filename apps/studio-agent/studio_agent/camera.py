"""Camera adapter contract; the baseline implementation uses a watch folder."""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import BinaryIO, Protocol

from .journal import Entry
from .watchfolder import WatchFolder


@dataclass(frozen=True)
class CameraDevice:
    id: str
    adapter: str
    label: str


class CameraAdapter(Protocol):
    def list_devices(self) -> list[CameraDevice]: ...
    def connect(self, device_id: str) -> None: ...
    def capture_events(self, now: float | None = None) -> list[Entry]: ...
    def read_file(self, entry: Entry) -> BinaryIO: ...
    def disconnect(self) -> None: ...


class WatchFolderAdapter:
    """No native tethering claims. Source files are read-only descriptors."""
    def __init__(self, watch: WatchFolder):
        self.watch = watch
        self.connected = False

    def list_devices(self) -> list[CameraDevice]:
        return [CameraDevice(self.watch.device_id, "watch-folder", self.watch.root.name)]

    def connect(self, device_id: str) -> None:
        if device_id != self.watch.device_id:
            raise ValueError("unknown watch-folder device")
        self.connected = True

    def capture_events(self, now: float | None = None) -> list[Entry]:
        if not self.connected:
            raise ValueError("camera adapter disconnected")
        return self.watch.scan(now, max_hashes=2)

    def read_file(self, entry: Entry) -> BinaryIO:
        if (not self.connected or entry.device_id != self.watch.device_id
                or entry.state not in ("hashed", "uploading", "verified",
                                       "receipt", "done")
                or not entry.sha256
                or self.watch.journal.get(entry.device_id, entry.sequence) != entry):
            raise ValueError("camera adapter source is not current")
        fd = self.watch._open_no_symlinks(entry.relative_path)
        try:
            info = os.fstat(fd)
            if (info.st_size, info.st_mtime_ns) != (entry.size, entry.mtime_ns):
                raise ValueError("camera source changed")
            digest = self.watch._hash_exact(fd, entry.size)
            after = os.fstat(fd)
            if (digest != entry.sha256 or
                    (after.st_size, after.st_mtime_ns) != (entry.size, entry.mtime_ns)):
                raise ValueError("camera source hash changed")
            os.lseek(fd, 0, os.SEEK_SET)
            return os.fdopen(fd, "rb")
        except BaseException:
            os.close(fd)
            raise

    def disconnect(self) -> None:
        self.connected = False
