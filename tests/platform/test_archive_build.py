import hashlib
import io
import struct
import zipfile
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.archive import ArchiveEntry, ArchiveParams
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.archive.build import _copy_entry, execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace


class Context:
    def check_active(self):
        return None


def ref(char='a'):
    return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)


def test_archive_params_reject_paths_and_casefold_collisions():
    item = ref()
    with pytest.raises(ValueError):
        ArchiveEntry(artifact=item, bytes=1, entry_name='../secret.jpg')
    with pytest.raises(ValueError):
        ArchiveEntry(artifact=item, bytes=1, entry_name='MANIFEST.JSON')
    with pytest.raises(ValueError):
        ArchiveParams(snapshot_id='s', snapshot_digest='b' * 64, volume_no=1, entries=[
            ArchiveEntry(artifact=item, bytes=1, entry_name='Photo.jpg'),
            ArchiveEntry(artifact=ref('c'), bytes=1, entry_name='photo.JPG'),
        ])


def test_copy_entry_requires_exact_size_and_digest():
    data = b'photo-data'
    output = io.BytesIO()
    digest = _copy_entry(Context(), io.BytesIO(data), output, len(data), hashlib.sha256(data).hexdigest())
    assert output.getvalue() == data
    assert digest == hashlib.sha256(data).hexdigest()
    with pytest.raises(OperationFailure):
        _copy_entry(Context(), io.BytesIO(data), io.BytesIO(), len(data) + 1, hashlib.sha256(data).hexdigest())
    with pytest.raises(OperationFailure):
        _copy_entry(Context(), io.BytesIO(data), io.BytesIO(), len(data), '0' * 64)


def test_execute_streams_verified_zip64_and_manifest(tmp_path):
    first, second = b'first-photo', b'second-photo'
    refs = [ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(data).hexdigest()) for data in (first, second)]
    params = ArchiveParams(snapshot_id=uuid4(), snapshot_digest='d' * 64, volume_no=2, entries=[
        ArchiveEntry(artifact=refs[0], bytes=len(first), entry_name='first.jpg'),
        ArchiveEntry(artifact=refs[1], bytes=len(second), entry_name='second.jpg'),
    ])

    class ExecutionContext(Context):
        lease = SimpleNamespace(execution_profile={'backend': 'cpu'}, command=SimpleNamespace(inputs=refs))

        def progress(self, done, total):
            assert 0 <= done <= total

    class Artifacts:
        def __init__(self):
            self.scratch = ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2 * 1024**2, min_free_bytes=0)
            self.archive = None
            self.manifest = None

        def params(self, model):
            assert model is ArchiveParams
            return params

        @contextmanager
        def stream(self, artifact):
            yield io.BytesIO({refs[0].artifact_id: first, refs[1].artifact_id: second}[artifact.artifact_id])

        def stage(self, path, reservation, role, media_type):
            assert role == 'archive' and media_type == 'application/zip'
            self.archive = path.read_bytes()
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(self.archive).hexdigest())

        def stage_contract(self, value, role):
            assert role == 'archive_manifest'
            self.manifest = value
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(value.model_dump_json().encode()).hexdigest())

    artifacts = Artifacts()
    try:
        result = execute(ExecutionContext(), artifacts)
        assert len(result.outputs) == 2 and artifacts.manifest.archive_sha256 == hashlib.sha256(artifacts.archive).hexdigest()
        with zipfile.ZipFile(io.BytesIO(artifacts.archive)) as archive:
            assert archive.namelist() == ['first.jpg', 'second.jpg', 'manifest.json']
            assert archive.read('first.jpg') == first and archive.read('second.jpg') == second
            assert archive.testzip() is None
        name_size,extra_size=struct.unpack_from('<HH',artifacts.archive,26)
        local_extra=artifacts.archive[30+name_size:30+name_size+extra_size]
        assert local_extra.startswith(b'\x01\x00')
    finally:
        artifacts.scratch.cleanup()


def test_archive_disk_budget_failure_publishes_nothing(tmp_path):
    data=b'bounded-input'
    artifact=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())
    params=ArchiveParams(snapshot_id=uuid4(),snapshot_digest='e'*64,volume_no=1,entries=[
        ArchiveEntry(artifact=artifact,bytes=len(data),entry_name='photo.jpg')])

    class ExecutionContext(Context):
        lease=SimpleNamespace(execution_profile={'backend':'cpu'},command=SimpleNamespace(inputs=[artifact]))
        def progress(self,done,total):pass

    class Artifacts:
        def __init__(self):
            self.scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,128,min_free_bytes=0)
            self.staged=[]
        def params(self,model):return params
        @contextmanager
        def stream(self,ref):yield io.BytesIO(data)
        def stage(self,*args):self.staged.append(args);raise AssertionError('published after disk failure')
        def stage_contract(self,*args):self.staged.append(args);raise AssertionError('published after disk failure')

    artifacts=Artifacts()
    try:
        with pytest.raises(Exception,match='scratch|quota|budget|reservation'):
            execute(ExecutionContext(),artifacts)
        assert not artifacts.staged
        assert not any(path.is_file() for path in tmp_path.rglob('*'))
    finally:
        artifacts.scratch.cleanup()


def test_archive_verification_checks_fence_between_large_member_blocks(tmp_path):
    from photo_worker.operations.archive.build import _verify_archive
    data=b'x'*(3*1024*1024)
    artifact=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())
    entry=ArchiveEntry(artifact=artifact,bytes=len(data),entry_name='large.bin')
    path=tmp_path/'archive.zip';manifest=b'{}'
    with zipfile.ZipFile(path,'w',allowZip64=True) as archive:
        with archive.open(entry.entry_name,'w',force_zip64=True) as output:output.write(data)
        archive.writestr('manifest.json',manifest)
    class FenceLost(RuntimeError):pass
    class Cancel:
        calls=0
        def check_active(self):
            self.calls+=1
            if self.calls==3:raise FenceLost('fence lost')
    ctx=Cancel()
    with pytest.raises(FenceLost,match='fence lost'):
        _verify_archive(ctx,path,[entry],manifest)
    assert ctx.calls==3
    _verify_archive(Context(),path,[entry],manifest)
    wrong=entry.model_copy(update={'artifact':ref('0')})
    with pytest.raises(OperationFailure):
        _verify_archive(Context(),path,[wrong],manifest)


def test_copy_entry_refuses_invalid_or_oversized_stream_blocks():
    class Invalid:
        def read(self,count):return 'not bytes'
    with pytest.raises(OperationFailure):
        _copy_entry(Context(),Invalid(),io.BytesIO(),10,'a'*64)
    class Oversized:
        calls=0
        def read(self,count):
            self.calls+=1
            return b'x'*(count+1)
    source=Oversized()
    with pytest.raises(OperationFailure):
        _copy_entry(Context(),source,io.BytesIO(),2*1024*1024,'a'*64)
    assert source.calls==1


@pytest.mark.parametrize('code,expected,retryable', [
    ('ARTIFACT_MISSING', 'ARTIFACT_MISSING', False),
    ('ARTIFACT_HASH_MISMATCH', 'ARTIFACT_HASH_MISMATCH', False),
    ('ARTIFACT_SIZE_MISMATCH', 'ARTIFACT_HASH_MISMATCH', False),
    ('DEPENDENCY_UNAVAILABLE', 'DEPENDENCY_UNAVAILABLE', True),
])
def test_archive_failure_retains_exact_declared_member_and_retry_class(code, expected, retryable):
    from photo_worker.operations.archive.build import _entry_failure
    from photo_worker.runtime.gateway import GatewayError
    entry = ArchiveEntry(artifact=ref(), bytes=12, entry_name='photo.jpg')
    failure = _entry_failure(GatewayError(code, retryable=True), entry)
    assert failure.error.code == expected
    assert failure.error.retryable is retryable
    assert failure.error.args == {'artifact_id': str(entry.artifact.artifact_id),
                                  'sha256': entry.artifact.sha256}


def test_archive_failure_does_not_relabel_lease_loss_as_a_member_failure():
    from photo_worker.operations.archive.build import _entry_failure
    from photo_worker.runtime.gateway import GatewayError
    original = GatewayError('LEASE_LOST')
    with pytest.raises(GatewayError) as caught:
        _entry_failure(original, ArchiveEntry(artifact=ref(), bytes=1, entry_name='photo.jpg'))
    assert caught.value is original


def test_execute_archive_bytes_and_manifest_metadata_ignore_wall_clock(tmp_path, monkeypatch):
    """Identical planned volume must publish identical ZIP bytes across attempts."""
    import json
    from photo_worker.contracts.base import canonical_bytes

    data = b'unchanged encoded full-resolution bytes'
    source = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(data).hexdigest())
    params = ArchiveParams(snapshot_id=uuid4(), snapshot_digest='d' * 64, volume_no=1,
        entries=[ArchiveEntry(artifact=source, bytes=len(data), entry_name='photo.jpg')])

    class ArchiveContext(Context):
        lease = SimpleNamespace(execution_profile={'backend': 'cpu'}, command=SimpleNamespace(inputs=[source]))

        def progress(self, done, total):
            assert 0 <= done <= total

    class ArchiveArtifacts:
        def __init__(self):
            self.scratch = ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2 * 1024**2, min_free_bytes=0)

        def params(self, model):
            assert model is ArchiveParams
            return params

        @contextmanager
        def stream(self, artifact):
            assert artifact == source
            yield io.BytesIO(data)

        def stage(self, path, reservation, role, media_type):
            assert (role, media_type) == ('archive', 'application/zip')
            self.zip_bytes = path.read_bytes()
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(self.zip_bytes).hexdigest())

        def stage_contract(self, manifest, role):
            assert role == 'archive_manifest'
            self.manifest = manifest
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())

    attempts = []
    clocks = [(2021, 2, 3, 4, 5, 6, 2, 34, -1), (2035, 9, 10, 11, 12, 14, 0, 253, -1)]
    for clock in clocks:
        # The old string-name writestr path consumes this clock for manifest.json.
        with monkeypatch.context() as patch:
            patch.setattr(zipfile.time, 'localtime', lambda *args, value=clock: value)
            artifacts = ArchiveArtifacts()
            try:
                result = execute(ArchiveContext(), artifacts)
                assert len(result.outputs) == 2
                assert artifacts.manifest.archive_sha256 == hashlib.sha256(artifacts.zip_bytes).hexdigest()
                assert artifacts.manifest.archive_bytes == len(artifacts.zip_bytes)
                attempts.append((artifacts.zip_bytes, canonical_bytes(artifacts.manifest)))
            finally:
                artifacts.scratch.cleanup()

    assert attempts[0] == attempts[1]
    with zipfile.ZipFile(io.BytesIO(attempts[0][0])) as archive:
        assert archive.namelist() == ['photo.jpg', 'manifest.json']
        assert archive.read('photo.jpg') == data
        info = archive.getinfo('manifest.json')
        assert info.date_time == (1980, 1, 1, 0, 0, 0)
        assert info.external_attr >> 16 == 0o600
        assert info.compress_type == zipfile.ZIP_DEFLATED
        embedded = json.loads(archive.read(info))
        assert embedded['snapshot_id'] == str(params.snapshot_id)
        assert embedded['snapshot_digest'] == params.snapshot_digest
        assert embedded['entries'][0]['artifact'] == source.model_dump(mode='json')
        assert archive.testzip() is None
