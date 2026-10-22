from uuid import uuid4
import os
import pytest
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota,ScratchError


def space(root,**kwargs):
    return ScratchSpace(root,uuid4(),uuid4(),1,kwargs.get('budget',100),min_free_bytes=0)


def test_reservation_budget_lifetime_and_modes(tmp_path):
    with space(tmp_path) as scratch:
        r=scratch.reserve(80)
        path=r.path('pixels','.bin')
        path.write_bytes(b'x'*80)
        r.validate()
        assert path.stat().st_mode&0o777==0o600
        assert scratch.directory.stat().st_mode&0o777==0o700
        with pytest.raises(DiskQuota):scratch.reserve(21)
        r.release();r.release()
        assert not path.exists()
        with scratch.reserve(100) as other:
            other.path('result','.json').write_bytes(b'{}')
        with pytest.raises(ScratchError):r.path('after-release')
        directory=scratch.directory
    assert not directory.exists()
    scratch.cleanup()


def test_reject_oversized_output_and_untrusted_names(tmp_path):
    with space(tmp_path) as scratch:
        with scratch.reserve(5) as r:
            with pytest.raises(ValueError):r.path('../escape')
            with pytest.raises(ValueError):r.path('safe','/outside')
            p=r.path('data');p.write_bytes(b'123456')
            with pytest.raises(DiskQuota):r.validate()


def test_existing_attempt_is_not_adopted(tmp_path):
    tenant,task=uuid4(),uuid4()
    with ScratchSpace(tmp_path,tenant,task,1,100,min_free_bytes=0) as first:
        p=first.reserve(10).path('keep');p.write_bytes(b'keep')
        with pytest.raises(FileExistsError):ScratchSpace(tmp_path,tenant,task,1,100,min_free_bytes=0)
        assert p.read_bytes()==b'keep'


def test_symlink_cleanup_does_not_touch_target(tmp_path):
    target=tmp_path/'original';target.write_bytes(b'original')
    with space(tmp_path) as scratch:
        r=scratch.reserve(10)
        p=r.path('image');p.unlink();p.symlink_to(target)
        with pytest.raises(ScratchError):r.validate()
    assert target.read_bytes()==b'original'


def test_symlink_root_rejected(tmp_path):
    real=tmp_path/'real';real.mkdir()
    link=tmp_path/'link';link.symlink_to(real,target_is_directory=True)
    with pytest.raises(ScratchError):space(link)


def test_disk_preflight_uses_available_bytes(tmp_path,monkeypatch):
    from types import SimpleNamespace
    with space(tmp_path) as scratch:
        monkeypatch.setattr(os,'fstatvfs',lambda _:SimpleNamespace(f_bavail=10,f_frsize=1))
        with pytest.raises(DiskQuota):scratch.reserve(11)


def test_materialize_checks_hash_and_keeps_reservation(tmp_path):
    import hashlib
    from io import BytesIO
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.runtime.materialize import materialize,ArtifactTransferError
    data=b'input pixels'
    ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())
    with space(tmp_path,budget=len(data)) as scratch:
        stream=BytesIO(data)
        with materialize(ref,len(data),scratch,lambda _:stream,lambda:None) as artifact:
            assert artifact.path.read_bytes()==data and stream.closed
            with pytest.raises(DiskQuota):scratch.reserve(1)
        assert not artifact.path.exists()
        for bad in [b'X'*len(data),data+b'X',data[:-1]]:
            with pytest.raises(ArtifactTransferError):materialize(ref,len(data),scratch,lambda _:BytesIO(bad),lambda:None)
            assert list(scratch.directory.iterdir())==[]


def test_materialize_cancellation_removes_partial_file(tmp_path):
    from io import BytesIO
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.runtime.materialize import materialize
    calls=0
    def cancelled():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with space(tmp_path) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            materialize(ArtifactRef(artifact_id=uuid4(),sha256='a'*64),10,scratch,lambda _:BytesIO(b'0123456789'),cancelled)
        assert list(scratch.directory.iterdir())==[]
        scratch.reserve(100).release()


def test_planned_output_stays_owned_and_requires_publication(tmp_path):
    with space(tmp_path) as scratch:
        with scratch.reserve(10) as r:
            path=r.planned_path('atomic','.npy')
            assert not path.exists()
            with pytest.raises(FileNotFoundError):r.validate_file(path)
            path.write_bytes(b'published')
            r.validate_file(path)
        assert not path.exists()
        with scratch.reserve(100) as r:r.planned_path('unused','.npy')


def test_failed_stream_releases_partial_bytes_and_allows_retry(tmp_path):
    import hashlib
    from io import BytesIO
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.runtime.materialize import materialize

    data = b'partial then retry'
    ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(data).hexdigest())

    class InterruptedStream(BytesIO):
        def read(self, size=-1):
            if self.tell():
                raise OSError('connection interrupted after partial input')
            return super().read(3)

    stream = InterruptedStream(data)
    with space(tmp_path, budget=len(data)) as scratch:
        with pytest.raises(OSError, match='connection interrupted'):
            materialize(ref, len(data), scratch, lambda _: stream, lambda: None)
        assert stream.closed
        assert list(scratch.directory.iterdir()) == []
        with materialize(ref, len(data), scratch, lambda _: BytesIO(data), lambda: None) as result:
            assert result.path.read_bytes() == data
        assert list(scratch.directory.iterdir()) == []
