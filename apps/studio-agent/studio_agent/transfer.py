"""Restart-safe capture orchestration; network adapters implement CaptureAPI."""

from __future__ import annotations

import os
from concurrent.futures import ThreadPoolExecutor
from typing import Protocol

from .journal import Entry, Journal
from .watchfolder import WatchFolder


class CaptureAPI(Protocol):
    def begin(self, entry: Entry, key: str) -> dict: ...
    def get(self, upload_id: str) -> dict: ...
    def upload_part(self, upload_id: str, part_number: int, data: bytes) -> str: ...
    def complete(self, upload_id: str, parts: list[dict], size: int, key: str) -> None: ...
    def receipt(self, entry: Entry, key: str) -> dict: ...


class SourceChanged(Exception):
    pass


class Transfer:
    def __init__(self, watch: WatchFolder, api: CaptureAPI):
        self.watch = watch
        self.journal: Journal = watch.journal
        self.api = api

    @staticmethod
    def key(entry: Entry, action: str) -> str:
        return f"capture/{entry.device_id}/{entry.sequence}/{action}"

    def _parts(self, entry: Entry, session: dict) -> None:
        part_bytes, parts_total = session["part_bytes"], session["parts_total"]
        self.journal.bind_upload(entry.device_id, entry.sequence, entry.upload_id,
                                 part_bytes, parts_total)
        saved = {part["part_no"] for part in self.journal.completed_parts(entry.device_id, entry.sequence)}
        fd = self.watch._open_no_symlinks(entry.relative_path)
        try:
            before = os.fstat(fd)
            if (before.st_size, before.st_mtime_ns) != (entry.size, entry.mtime_ns):
                raise SourceChanged("capture source changed")
            missing = iter(number for number in range(1, parts_total + 1) if number not in saved)
            # Bound memory and network IO to two parts. SQLite writes remain on
            # the owning thread, including successful siblings of failed PUTs.
            with ThreadPoolExecutor(max_workers=2) as pool:
                while True:
                    batch = []
                    for _ in range(2):
                        number = next(missing, None)
                        if number is None:
                            break
                        offset = (number - 1) * part_bytes
                        size = min(part_bytes, entry.size - offset)
                        data = os.pread(fd, size, offset)
                        if len(data) != size:
                            raise SourceChanged("capture source truncated")
                        batch.append((number, size, pool.submit(self.api.upload_part,
                                     entry.upload_id, number, data)))
                        del data
                    if not batch:
                        break
                    failure = None
                    for number, size, future in batch:
                        try:
                            etag = future.result()
                            self.journal.record_part(entry.device_id, entry.sequence, number, etag, size)
                        except Exception as error:
                            failure = failure or error
                    if failure is not None:
                        raise failure
            after = os.fstat(fd)
            if (after.st_size, after.st_mtime_ns) != (entry.size, entry.mtime_ns):
                raise SourceChanged("capture source changed")
        finally:
            os.close(fd)
        if not self.watch.rehash_before_complete(entry):
            raise SourceChanged("capture source hash changed")
        self.api.complete(entry.upload_id,
                          self.journal.completed_parts(entry.device_id, entry.sequence),
                          entry.size, self.key(entry, "complete"))

    def process(self, device_id: str, sequence: int) -> Entry:
        entry = self.journal.get(device_id, sequence)
        if entry is None or device_id != self.watch.device_id:
            raise ValueError("unknown capture")
        if entry.state == "hashed":
            if entry.upload_id is None:
                if not self.watch.rehash_before_complete(entry):
                    raise SourceChanged("capture source hash changed")
                session = self.api.begin(entry, self.key(entry, "begin"))
                self.journal.bind_upload(device_id, sequence, session["id"],
                                         session["part_bytes"], session["parts_total"])
            entry = self.journal.transition(device_id, sequence, "hashed", "uploading")
        if entry.state == "uploading":
            session = self.api.get(entry.upload_id)
            if session["state"] == "uploading":
                self._parts(entry, session)
                return self.journal.get(device_id, sequence)
            if session["state"] in ("initializing", "completing", "verifying"):
                return entry
            if session["state"] != "verified":
                raise ValueError("capture upload terminated without verification")
            entry = self.journal.transition(device_id, sequence, "uploading", "verified",
                                            upload_id=entry.upload_id)
        if entry.state == "verified":
            entry = self.journal.transition(device_id, sequence, "verified", "receipt")
        if entry.state == "receipt":
            receipt = self.api.receipt(entry, self.key(entry, "receipt"))
            if receipt.get("device_id") != device_id or receipt.get("sequence") != str(sequence) or receipt.get("content_sha") != entry.sha256 or receipt.get("upload_id") != entry.upload_id:
                raise ValueError("capture receipt does not match journal")
            entry = self.journal.transition(device_id, sequence, "receipt", "done")
        return entry
