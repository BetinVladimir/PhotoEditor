"""Standalone trusted V2 adoption and finalization process."""
from __future__ import annotations

from dataclasses import dataclass, field
from ipaddress import ip_address
from threading import Event
from typing import Mapping
from urllib.parse import urlsplit
from uuid import UUID
import os
from pathlib import Path

import psycopg
from minio import Minio

from .adoption import AdoptionInvalid
from .adoption_service import AdoptionCoordinator


@dataclass(frozen=True)
class AdopterConfig:
    database_url: str = field(repr=False)
    endpoint: str
    bucket: str
    access_key: str = field(repr=False)
    secret_key: str = field(repr=False)
    development: bool = False
    layer_scratch_root: Path | None = None
    layer_scratch_bytes: int | None = None

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "AdopterConfig":
        env = os.environ if env is None else env
        values = [env.get(key, "") for key in (
            "DATABASE_URL", "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY")]
        if any(not value for value in values):
            raise ValueError("Missing V2 adopter configuration")
        development = env.get("APP_ENV") == "development"
        endpoint = urlsplit(values[1])
        try:
            loopback = endpoint.hostname == "localhost" or ip_address(endpoint.hostname or "").is_loopback
        except ValueError:
            loopback = False
        if (endpoint.scheme not in {"http", "https"} or not endpoint.netloc
                or endpoint.username or endpoint.password or endpoint.path or endpoint.query
                or endpoint.fragment or (endpoint.scheme == "http" and not (development and loopback))):
            raise ValueError("Invalid V2 adopter object-store endpoint")
        scratch_raw=env.get('PHOTO_ADOPTER_LAYER_SCRATCH_ROOT','')
        budget_raw=env.get('PHOTO_ADOPTER_LAYER_SCRATCH_BYTES','')
        if bool(scratch_raw)!=bool(budget_raw):
            raise ValueError('Layer adoption scratch root and budget must be paired')
        scratch=None;budget=None
        if scratch_raw:
            scratch=Path(scratch_raw)
            if not scratch.is_absolute() or scratch.resolve(strict=True)!=scratch or not scratch.is_dir():
                raise ValueError('Invalid layer adoption scratch root')
            if not budget_raw.isascii() or not budget_raw.isdigit() or len(budget_raw)>16:
                raise ValueError('Invalid layer adoption scratch budget')
            budget=int(budget_raw)
            if not 1<=budget<=2**53-1:raise ValueError('Invalid layer adoption scratch budget')
        return cls(values[0], values[1], values[2], values[3], values[4], development,scratch,budget)


def _advisory_key(task_id: UUID) -> str:
    return "v2-adopt:" + str(task_id)


class Adopter:
    def __init__(self, read_connection, write_connection, objects, bucket: str, *, layer_scratch_root=None, layer_scratch_bytes=None):
        self.read = read_connection
        self.write = write_connection
        self.coordinator = AdoptionCoordinator(read_connection, write_connection, objects, bucket,
            layer_scratch_root=layer_scratch_root,layer_scratch_bytes=layer_scratch_bytes)

    def run_once(self) -> dict[str, int]:
        counts = {"adopted": 0, "rejected": 0, "finalized": 0, "retry": 0}
        candidates = self.read.execute("SELECT * FROM photo.v2_pending_adoptions()").fetchall()
        for tenant_id, _, task_id in candidates:
            key = _advisory_key(task_id)
            acquired = self.read.execute(
                "SELECT pg_try_advisory_lock(hashtextextended(%s,0))", (key,)).fetchone()[0]
            if not acquired:
                continue
            try:
                try:
                    if self.coordinator.adopt(tenant_id, task_id):
                        counts["adopted"] += 1
                except AdoptionInvalid as exc:
                    code = str(exc)
                    if code in {"V2_ADOPTION_CONTEXT_STALE", "RETRYABLE_CONFLICT", "LAYER_ADOPTION_SCRATCH_REQUIRED", "ADOPTION_SCRATCH_EXCEEDED"}:
                        counts["retry"] += 1
                        continue
                    try:
                        context = self.coordinator.context(tenant_id, task_id)
                    except AdoptionInvalid:
                        counts["retry"] += 1
                        continue
                    try:
                        with self.write.transaction():
                            row = self.write.execute(
                                "SELECT photo.reject_v2_adoption(%s,%s,%s,%s)",
                                (tenant_id, task_id, context.result_id, code),
                            ).fetchone()
                            if row and row[0]:
                                counts["rejected"] += 1
                    except Exception:
                        counts["retry"] += 1
                except Exception:
                    # DB/object-store availability is not a result-validation
                    # failure. Retry later without publishing exception text.
                    counts["retry"] += 1
            finally:
                self.read.execute("SELECT pg_advisory_unlock(hashtextextended(%s,0))", (key,))
        finalizations = self.read.execute("SELECT * FROM photo.v2_pending_finalizations()").fetchall()
        for tenant_id, run_id in finalizations:
            try:
                with self.write.transaction():
                    state = self.write.execute("SELECT photo.finalize_v2_run(%s,%s)",
                                               (tenant_id, run_id)).fetchone()[0]
                if state in {"completed", "waiting_review", "failed", "cancelled"}:
                    counts["finalized"] += 1
            except Exception:
                counts["retry"] += 1
        return counts


def run_adopter(config: AdopterConfig, stop: Event | None = None) -> None:
    stop = Event() if stop is None else stop
    endpoint = urlsplit(config.endpoint)
    objects = Minio(endpoint.netloc, access_key=config.access_key,
                    secret_key=config.secret_key, secure=endpoint.scheme == "https")
    options = "-c role=photo_scheduler -c statement_timeout=30000 -c lock_timeout=3000"
    with psycopg.connect(config.database_url, autocommit=True, connect_timeout=5,
                         options=options) as read_connection:
        with psycopg.connect(config.database_url, autocommit=True, connect_timeout=5,
                             options=options) as write_connection:
            adopter = Adopter(read_connection, write_connection, objects, config.bucket,
                layer_scratch_root=config.layer_scratch_root,layer_scratch_bytes=config.layer_scratch_bytes)
            while not stop.is_set():
                adopter.run_once()
                stop.wait(1.0)
