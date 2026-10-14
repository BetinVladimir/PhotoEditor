"""Typed V2 documents grow to 8 MiB without changing legacy control limits."""
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.gateway import GatewayError
from photo_worker.runtime.scratch import ScratchSpace


class Payload(Contract):
    data: str


class Gateway:
    def __init__(self):
        self.bytes_uploaded = []

    def upload(self, path: Path, role: str, media_type: str, *, expected_identity=None) -> ArtifactRef:
        assert expected_identity == (path.stat().st_dev, path.stat().st_ino)
        raw = path.read_bytes()
        self.bytes_uploaded.append((len(raw), role, media_type))
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())


class Index:
    def close(self):
        pass


def artifact_session(tmp_path, *, v2):
    tenant, task = uuid4(), uuid4()
    params = ArtifactRef(artifact_id=uuid4(), sha256="a" * 64)
    ctx = SimpleNamespace(
        lease=SimpleNamespace(command=SimpleNamespace(inputs=[], params=params)),
        check_active=lambda: None,
    )
    scratch = ScratchSpace(tmp_path, tenant, task, 1, 20 * 1024 * 1024)
    gateway = Gateway()
    return TaskArtifacts(ctx, gateway, scratch, Index() if v2 else None), gateway


@pytest.mark.parametrize("v2,limit", [(False, 1_048_576), (True, 8_388_608)])
def test_typed_stage_limit_is_exact_and_legacy_stays_one_mebibyte(tmp_path, v2, limit):
    session, gateway = artifact_session(tmp_path, v2=v2)
    try:
        overhead = len(canonical_bytes(Payload(data="")))
        accepted = Payload(data="x" * (limit - overhead))
        session.stage_contract(accepted, "document")
        assert gateway.bytes_uploaded == [(limit, "document", "application/json")]
        with pytest.raises(ValueError, match="typed document limit"):
            session.stage_contract(Payload(data="x" * (limit - overhead + 1)), "document")
        assert len(gateway.bytes_uploaded) == 1
    finally:
        session.close()


def test_stage_rejects_replaced_reserved_file_before_upload(tmp_path):
    session, gateway = artifact_session(tmp_path, v2=True)
    try:
        with session.scratch.reserve(64) as reservation:
            path = reservation.path('payload', '.bin')
            path.write_bytes(b'first')
            identity = (path.stat().st_dev, path.stat().st_ino)
            path.unlink()
            path.write_bytes(b'second')
            with pytest.raises(GatewayError, match='ARTIFACT_CHANGED'):
                session.stage(path, reservation, 'document', 'application/octet-stream', expected_identity=identity)
            assert gateway.bytes_uploaded == []
    finally:
        session.close()
