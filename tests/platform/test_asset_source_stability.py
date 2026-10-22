"""Source mutation/cancellation boundaries; authored, execution deferred."""
from hashlib import sha256
import pytest
from photo_worker.operations.asset import verify


@pytest.mark.parametrize('phase',['metadata','raster'])
def test_verify_decode_rejects_same_size_mutation_after_initial_hash(tmp_path,monkeypatch,phase):
    from photo_worker.operations.asset import inspect
    path=tmp_path/'input.png';path.write_bytes(b'\x89PNG\r\n\x1a\n'+b'a'*40)
    expected=sha256(path.read_bytes()).hexdigest()
    def mutate():
        raw=bytearray(path.read_bytes());raw[-1]^=1;path.write_bytes(raw)
    def metadata(_source):
        if phase=='metadata':mutate()
    def raster(_path,**_kwargs):
        if phase=='raster':mutate()
    monkeypatch.setattr(inspect,'read_metadata',metadata)
    monkeypatch.setattr(verify,'verify_raster_decode',raster)
    with pytest.raises(verify.AssetError,match='INPUT_HASH_MISMATCH'):
        verify.verify_decode(path,path.stat().st_size,expected)


def test_verify_size_rechecks_activity_after_last_chunk(tmp_path):
    path=tmp_path/'input.png';path.write_bytes(b'a')
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled_after_read')
    with pytest.raises(RuntimeError,match='cancelled_after_read'):
        verify.verify_size(path,check_active=active)
