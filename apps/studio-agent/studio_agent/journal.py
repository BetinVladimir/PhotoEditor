"""Durable, device-scoped watch-folder journal for V24 capture."""

from __future__ import annotations

import math
import sqlite3
import time
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Entry:
    device_id: str
    sequence: int
    relative_path: str
    size: int
    mtime_ns: int
    state: str
    sha256: str | None
    upload_id: str | None
    retries: int
    last_error: str | None


class QueueFull(Exception):
    pass


class Journal:
    _next_states = {
        "stable": "hashed", "hashed": "uploading", "uploading": "verified",
        "verified": "receipt", "receipt": "done", "error": "hashed",
    }

    def __init__(self, path: Path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(path, isolation_level=None)
        self.db.row_factory = sqlite3.Row
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute("PRAGMA foreign_keys=ON")
        self.db.execute("PRAGMA busy_timeout=5000")
        self.db.executescript("""
            CREATE TABLE IF NOT EXISTS entries (
                device_id TEXT NOT NULL,
                sequence INTEGER NOT NULL CHECK(sequence > 0),
                relative_path TEXT NOT NULL,
                size INTEGER NOT NULL CHECK(size > 0),
                mtime_ns INTEGER NOT NULL,
                state TEXT NOT NULL CHECK(state IN
                    ('discovered','stable','hashed','uploading','verified','receipt','done','error')),
                sha256 TEXT,
                upload_id TEXT,
                retries INTEGER NOT NULL DEFAULT 0,
                last_error TEXT,
                observed_at REAL NOT NULL,
                PRIMARY KEY(device_id, sequence),
                UNIQUE(device_id, relative_path, size, mtime_ns)
            );
            CREATE INDEX IF NOT EXISTS entries_pending
                ON entries(device_id,state,sequence);
            CREATE TABLE IF NOT EXISTS multipart_uploads (
                device_id TEXT NOT NULL,
                sequence INTEGER NOT NULL,
                upload_id TEXT NOT NULL,
                part_bytes INTEGER NOT NULL CHECK(part_bytes > 0),
                parts_total INTEGER NOT NULL CHECK(parts_total BETWEEN 1 AND 10000),
                PRIMARY KEY(device_id,sequence),
                FOREIGN KEY(device_id,sequence) REFERENCES entries(device_id,sequence)
            );
            CREATE TABLE IF NOT EXISTS multipart_parts (
                device_id TEXT NOT NULL,
                sequence INTEGER NOT NULL,
                part_number INTEGER NOT NULL CHECK(part_number BETWEEN 1 AND 10000),
                etag TEXT NOT NULL,
                bytes INTEGER NOT NULL CHECK(bytes > 0),
                PRIMARY KEY(device_id,sequence,part_number),
                FOREIGN KEY(device_id,sequence) REFERENCES multipart_uploads(device_id,sequence)
            );
        """)
        columns = {row["name"] for row in self.db.execute("PRAGMA table_info(entries)")}
        if "next_attempt_at" not in columns:
            self.db.execute("ALTER TABLE entries ADD COLUMN next_attempt_at REAL NOT NULL DEFAULT 0")

    def close(self) -> None:
        self.db.close()

    def _entry(self, row: sqlite3.Row | None) -> Entry | None:
        if row is None:
            return None
        return Entry(*(row[key] for key in (
            "device_id", "sequence", "relative_path", "size", "mtime_ns",
            "state", "sha256", "upload_id", "retries", "last_error")))

    def observe(self, device_id: str, relative_path: str, size: int,
                mtime_ns: int, now: float | None = None,
                max_queued_bytes: int | None = None) -> Entry:
        if not device_id or not relative_path or size < 1 or mtime_ns < 0:
            raise ValueError("invalid observation")
        observed_at = time.time() if now is None else now
        self.db.execute("BEGIN IMMEDIATE")
        try:
            row = self.db.execute("""SELECT * FROM entries WHERE device_id=? AND
                relative_path=? AND size=? AND mtime_ns=?""",
                (device_id, relative_path, size, mtime_ns)).fetchone()
            if row is None:
                if max_queued_bytes is not None and self.queued_bytes(device_id) + size > max_queued_bytes:
                    raise QueueFull("watch queue byte limit reached")
                sequence = self.db.execute("SELECT COALESCE(MAX(sequence),0)+1 FROM entries WHERE device_id=?",
                                           (device_id,)).fetchone()[0]
                self.db.execute("""INSERT INTO entries(device_id,sequence,relative_path,
                    size,mtime_ns,state,observed_at) VALUES(?,?,?,?,?,'discovered',?)""",
                    (device_id, sequence, relative_path, size, mtime_ns, observed_at))
            elif row["state"] == "discovered" and observed_at - row["observed_at"] >= 1:
                self.db.execute("""UPDATE entries SET state='stable' WHERE
                    device_id=? AND sequence=? AND state='discovered'""", (device_id, row["sequence"]))
                sequence = row["sequence"]
            else:
                sequence = row["sequence"]
            result = self.get(device_id, sequence)
            self.db.execute("COMMIT")
            assert result is not None
            return result
        except BaseException:
            self.db.execute("ROLLBACK")
            raise

    def get(self, device_id: str, sequence: int) -> Entry | None:
        return self._entry(self.db.execute(
            "SELECT * FROM entries WHERE device_id=? AND sequence=?", (device_id, sequence)).fetchone())

    def transition(self, device_id: str, sequence: int, before: str, after: str,
                   *, sha256: str | None = None, upload_id: str | None = None,
                   error: str | None = None) -> Entry:
        if (self._next_states.get(before) != after
                or after == "hashed" and before == "stable" and sha256 is None
                or sha256 is not None and (len(sha256) != 64 or any(c not in "0123456789abcdef" for c in sha256))
                or after == "verified" and not upload_id):
            raise ValueError("invalid journal transition")
        if before == "error":
            current = self.get(device_id, sequence)
            if current is None or current.sha256 is None:
                raise ValueError("unhashed error cannot enter upload queue")
        values = [after, sha256, upload_id, error, device_id, sequence, before, upload_id, upload_id]
        changed = self.db.execute("""UPDATE entries SET state=?,sha256=COALESCE(?,sha256),
            upload_id=COALESCE(?,upload_id),last_error=? WHERE device_id=? AND sequence=?
            AND state=? AND (upload_id IS NULL OR ? IS NULL OR upload_id=?)""", values).rowcount
        if changed != 1:
            raise ValueError("stale journal state")
        result = self.get(device_id, sequence)
        assert result is not None
        return result

    def bind_upload(self, device_id: str, sequence: int, upload_id: str,
                    part_bytes: int, parts_total: int) -> None:
        """Persist the server admission before sending any multipart bytes."""
        if not upload_id or len(upload_id) > 128 or not 1 <= part_bytes <= 16 * 1024**2 or not 1 <= parts_total <= 10000:
            raise ValueError("invalid multipart admission")
        self.db.execute("BEGIN IMMEDIATE")
        try:
            entry = self.get(device_id, sequence)
            if entry is None or entry.state not in ("hashed", "uploading") or not entry.sha256:
                raise ValueError("entry cannot bind upload")
            if parts_total != (entry.size + part_bytes - 1) // part_bytes:
                raise ValueError("multipart layout does not match source")
            prior = self.db.execute("SELECT * FROM multipart_uploads WHERE device_id=? AND sequence=?",
                                    (device_id, sequence)).fetchone()
            if prior is not None and (prior["upload_id"], prior["part_bytes"], prior["parts_total"]) != (upload_id, part_bytes, parts_total):
                raise ValueError("multipart admission changed")
            if entry.upload_id not in (None, upload_id):
                raise ValueError("upload identity changed")
            self.db.execute("""INSERT OR IGNORE INTO multipart_uploads
                (device_id,sequence,upload_id,part_bytes,parts_total) VALUES(?,?,?,?,?)""",
                (device_id, sequence, upload_id, part_bytes, parts_total))
            self.db.execute("UPDATE entries SET upload_id=? WHERE device_id=? AND sequence=?",
                            (upload_id, device_id, sequence))
            self.db.execute("COMMIT")
        except BaseException:
            self.db.execute("ROLLBACK")
            raise

    def record_part(self, device_id: str, sequence: int, part_number: int,
                    etag: str, size: int) -> None:
        """An acknowledged part is immutable so a completion replay is exact."""
        if not etag or len(etag) > 256 or any(c in etag for c in "\r\n"):
            raise ValueError("invalid part ETag")
        self.db.execute("BEGIN IMMEDIATE")
        try:
            upload = self.db.execute("""SELECT m.*,e.size,e.state FROM multipart_uploads m
                JOIN entries e USING(device_id,sequence) WHERE m.device_id=? AND m.sequence=?""",
                (device_id, sequence)).fetchone()
            if upload is None or upload["state"] != "uploading" or not 1 <= part_number <= upload["parts_total"]:
                raise ValueError("part is outside active upload")
            expected = min(upload["part_bytes"], upload["size"] - (part_number - 1) * upload["part_bytes"])
            if size != expected:
                raise ValueError("part length mismatch")
            prior = self.db.execute("""SELECT etag,bytes FROM multipart_parts
                WHERE device_id=? AND sequence=? AND part_number=?""",
                (device_id, sequence, part_number)).fetchone()
            if prior is not None and (prior["etag"], prior["bytes"]) != (etag, size):
                raise ValueError("acknowledged part changed")
            self.db.execute("""INSERT OR IGNORE INTO multipart_parts
                (device_id,sequence,part_number,etag,bytes) VALUES(?,?,?,?,?)""",
                (device_id, sequence, part_number, etag, size))
            self.db.execute("COMMIT")
        except BaseException:
            self.db.execute("ROLLBACK")
            raise

    def completed_parts(self, device_id: str, sequence: int) -> list[dict]:
        return [{"part_no": row["part_number"], "etag": row["etag"]}
                for row in self.db.execute("""SELECT part_number,etag FROM multipart_parts
                    WHERE device_id=? AND sequence=? ORDER BY part_number""", (device_id, sequence))]

    def fail(self, device_id: str, sequence: int, before: str, reason: str) -> Entry:
        if not reason or len(reason) > 200:
            raise ValueError("invalid error reason")
        changed = self.db.execute("""UPDATE entries SET state='error',retries=retries+1,
            last_error=? WHERE device_id=? AND sequence=? AND state=?""",
            (reason, device_id, sequence, before)).rowcount
        if changed != 1:
            raise ValueError("stale journal state")
        result = self.get(device_id, sequence)
        assert result is not None
        return result

    def recover(self, device_id: str) -> None:
        # Upload completion uses a deterministic idempotency key from
        # (device_id,sequence); a crash during network IO may safely retry.
        self.db.execute("""UPDATE entries SET state='hashed' WHERE device_id=?
            AND state='uploading' AND sha256 IS NOT NULL""", (device_id,))

    def pending(self, device_id: str, state: str, limit: int = 2) -> list[Entry]:
        if limit < 1 or limit > 100:
            raise ValueError("invalid limit")
        return [self._entry(row) for row in self.db.execute("""SELECT * FROM entries
            WHERE device_id=? AND state=? ORDER BY sequence LIMIT ?""",
            (device_id, state, limit))]

    def queued_bytes(self, device_id: str) -> int:
        return self.db.execute("""SELECT COALESCE(SUM(size),0) FROM entries
            WHERE device_id=? AND state!='done'""", (device_id,)).fetchone()[0]

    def due(self, device_id: str, now: float, limit: int = 2) -> list[Entry]:
        if not 1 <= limit <= 100:
            raise ValueError("invalid limit")
        return [self._entry(row) for row in self.db.execute("""SELECT * FROM entries
            WHERE device_id=? AND state IN ('hashed','uploading','verified','receipt')
            AND next_attempt_at<=? ORDER BY sequence LIMIT ?""", (device_id, now, limit))]

    def delay(self, device_id: str, sequence: int, until: float, reason: str | None = None) -> None:
        if not 0 <= until or reason is not None and (not reason or len(reason) > 100):
            raise ValueError("invalid retry schedule")
        changed = self.db.execute("""UPDATE entries SET next_attempt_at=?,last_error=?,
            retries=retries+? WHERE device_id=? AND sequence=? AND state!='done'""",
            (until, reason, int(reason is not None), device_id, sequence)).rowcount
        if changed != 1:
            raise ValueError("capture is not pending")

    def summary(self, device_id: str, now: float | None = None) -> dict:
        """Bounded operational counters; never expose file paths or source hashes."""
        current_time = time.time() if now is None else now
        if not device_id or not math.isfinite(current_time) or current_time < 0:
            raise ValueError("invalid queue summary scope")
        rows = self.db.execute("""SELECT state,COUNT(*) AS count,SUM(size) AS bytes,
            MIN(observed_at) AS oldest,
            SUM(CASE WHEN state IN ('hashed','uploading','verified','receipt')
                AND next_attempt_at<=? THEN 1 ELSE 0 END) AS due
            FROM entries WHERE device_id=? GROUP BY state""",
            (current_time, device_id)).fetchall()
        counts = {state: 0 for state in ('discovered','stable','hashed','uploading',
                                       'verified','receipt','done','error')}
        queued_bytes, oldest, due = 0, None, 0
        for row in rows:
            counts[row["state"]] = row["count"]
            due += row["due"]
            if row["state"] != "done":
                queued_bytes += row["bytes"]
                oldest = row["oldest"] if oldest is None else min(oldest, row["oldest"])
        return {"schema_version": 1, "device_id": device_id, "states": counts,
                "queued_bytes": queued_bytes, "due_count": due,
                "oldest_pending_age_seconds": None if oldest is None else max(0, current_time-oldest)}

    def retry_error(self, device_id: str, sequence: int) -> Entry:
        """Explicit operator retry after the caller has revalidated source bytes."""
        self.db.execute("BEGIN IMMEDIATE")
        try:
            entry = self.get(device_id, sequence)
            if entry is None or entry.state != "error" or not entry.sha256:
                raise ValueError("only hashed failed captures may be retried")
            self.db.execute("""UPDATE entries SET state='hashed',next_attempt_at=0,
                last_error=NULL WHERE device_id=? AND sequence=? AND state='error'""",
                (device_id, sequence))
            result = self.get(device_id, sequence)
            self.db.execute("COMMIT")
            assert result is not None
            return result
        except BaseException:
            self.db.execute("ROLLBACK")
            raise
