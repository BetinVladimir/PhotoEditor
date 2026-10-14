import hashlib
import io
import json
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
from unittest.mock import Mock
import zipfile

import pytest

from photo_worker.contracts.archive import ArchiveParams
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.archive.build import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('count', [2, 2000])
def test_complete_archive_lifecycle_including_large_long_name_inventory(tmp_path, count):
    payload = b'x'
    digest = hashlib.sha256(payload).hexdigest()
    inputs = [ArtifactRef(artifact_id=uuid4(), sha256=digest) for _ in range(count)]
    params = ArchiveParams(snapshot_id=uuid4(), snapshot_digest='a' * 64, volume_no=1,
        entries=[{'artifact': ref, 'bytes': 1, 'entry_name': f'{index:04d}_' + 'x' * 171 + '.jpg'}
                 for index, ref in enumerate(inputs)])
    staged, progress, reads = {}, [], []
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'},
                          command=SimpleNamespace(inputs=inputs)),
                          check_active=lambda: None, progress=lambda done, total: progress.append((done, total)))

    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 32 << 20, min_free_bytes=0) as scratch:
        class Artifacts:
            def __init__(self): self.scratch = scratch
            def params(self, model):
                assert model is ArchiveParams
                return params
            @contextmanager
            def stream(self, ref):
                reads.append(ref)
                yield io.BytesIO(payload)
            def stage(self, path, reservation, role, mime):
                assert role == 'archive' and mime == 'application/zip'
                body = path.read_bytes()
                ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(body).hexdigest())
                staged[role] = (ref, body)
                return ref
            def stage_contract(self, value, role):
                assert role == 'archive_manifest'
                body = canonical_bytes(value)
                ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(body).hexdigest())
                staged[role] = (ref, body)
                return ref
        result = execute(ctx, Artifacts())
    archive_ref, archive_bytes = staged['archive']
    manifest_ref, manifest_bytes = staged['archive_manifest']
    manifest = json.loads(manifest_bytes)
    assert result.outputs == [archive_ref, manifest_ref]
    assert result.bytes_read == count
    assert result.bytes_written == len(archive_bytes) + len(manifest_bytes)
    assert manifest['archive_sha256'] == archive_ref.sha256
    assert manifest['archive_bytes'] == len(archive_bytes)
    assert len(manifest['entries']) == count
    assert reads == inputs
    assert progress[0] == (0, count + 2)
    assert progress[-1] == (count + 2, count + 2)
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as archive:
        assert archive.namelist() == [entry.entry_name for entry in params.entries] + ['manifest.json']
        for index in (0, count - 1):
            entry = params.entries[index]
            assert archive.read(entry.entry_name) == payload
            assert archive.getinfo(entry.entry_name).compress_type == zipfile.ZIP_STORED
        embedded = json.loads(archive.read('manifest.json'))
        assert embedded['entries'] == manifest['entries']
        assert embedded['snapshot_digest'] == params.snapshot_digest


def test_unplanned_5000_entry_manifest_overflow_refuses_before_scratch_and_image_io():
    ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(b'x').hexdigest())
    params = ArchiveParams(snapshot_id=uuid4(), snapshot_digest='a' * 64, volume_no=1,
        entries=[{'artifact': ref, 'bytes': 1, 'entry_name': f'{index:04d}.jpg'} for index in range(5000)])
    artifacts = Mock()
    artifacts.params.return_value = params
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'},
        command=SimpleNamespace(inputs=[ref] * 5000)), check_active=lambda: None, progress=Mock())
    with pytest.raises(OperationFailure) as failure:
        execute(ctx, artifacts)
    assert failure.value.error.code == 'INPUT_INVALID'
    artifacts.scratch.reserve.assert_not_called()
    artifacts.stream.assert_not_called()
    artifacts.stage.assert_not_called()
    artifacts.stage_contract.assert_not_called()
