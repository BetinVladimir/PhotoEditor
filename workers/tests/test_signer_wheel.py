"""Distribution integrity tests with synthetic bytes; not SDK qualification."""
from hashlib import sha256
import os

import pytest

from photo_worker.runtime import signer_wheel


def wheel(tmp_path, data=b'synthetic wheel fixture'):
    path = tmp_path.resolve()/'fixture.whl'
    path.write_bytes(data)
    path.chmod(0o600)
    return path


def test_official_pin_is_not_selected_by_filename_or_colocated_checksum(tmp_path):
    path = wheel(tmp_path)
    (path.parent/'checksum.txt').write_text(sha256(path.read_bytes()).hexdigest())
    with pytest.raises(ValueError, match='SIGNER_SDK_WHEEL_SHA256'):
        signer_wheel.verify_signer_sdk_wheel(path)


def test_streamed_receipt_and_descriptor_cleanup(tmp_path, monkeypatch):
    data = b'a' * ((1 << 20) + 17)
    path = wheel(tmp_path, data)
    monkeypatch.setattr(signer_wheel, 'SIGNER_SDK_WHEEL_SHA256', sha256(data).hexdigest())
    read, close = os.read, os.close
    reads, closed = [], []
    def bounded_read(fd, maximum):
        reads.append(maximum)
        return read(fd, min(maximum, 8192))
    def tracked_close(fd):
        closed.append(fd)
        return close(fd)
    monkeypatch.setattr(signer_wheel.os, 'read', bounded_read)
    monkeypatch.setattr(signer_wheel.os, 'close', tracked_close)
    receipt = signer_wheel.verify_signer_sdk_wheel(path)
    assert receipt.size_bytes == len(data) and receipt.sha256 == sha256(data).hexdigest()
    assert receipt.version == '0.32.4' and receipt.target == 'manylinux_2_28_x86_64'
    assert len(reads) > 2 and max(reads) <= 1 << 20 and len(closed) >= 2


@pytest.mark.parametrize('mode', ['symlink', 'hardlink', 'writable', 'empty', 'directory', 'oversize'])
def test_unsafe_or_unbounded_files_refused(tmp_path, monkeypatch, mode):
    path = wheel(tmp_path)
    if mode == 'symlink':
        path.rename(path.parent/'real.whl')
        path.symlink_to(path.parent/'real.whl')
    elif mode == 'hardlink': (path.parent/'alias.whl').hardlink_to(path)
    elif mode == 'writable': path.chmod(0o666)
    elif mode == 'empty': path.write_bytes(b'')
    elif mode == 'directory':
        path.unlink()
        path.mkdir()
    elif mode == 'oversize': monkeypatch.setattr(signer_wheel, 'SIGNER_SDK_WHEEL_MAX_BYTES', 1)
    with pytest.raises((ValueError, OSError)):
        signer_wheel.verify_signer_sdk_wheel(path)


def test_cancellation_closes_open_descriptor(tmp_path, monkeypatch):
    path = wheel(tmp_path)
    close, closed = os.close, []
    def tracked_close(fd):
        closed.append(fd)
        return close(fd)
    monkeypatch.setattr(signer_wheel.os, 'close', tracked_close)
    calls = 0
    def check():
        nonlocal calls
        calls += 1
        if calls == 2: raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        signer_wheel.verify_signer_sdk_wheel(path, check_active=check)
    assert len(closed) == 1


def test_boolean_denial_prevents_wheel_open(tmp_path, monkeypatch):
    path=wheel(tmp_path)
    monkeypatch.setattr(signer_wheel.os,'open',lambda *args,**kwargs:pytest.fail('wheel opened after denial'))
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        signer_wheel.verify_signer_sdk_wheel(path,check_active=lambda:False)


def test_boolean_denial_after_wheel_read_closes_descriptors(tmp_path, monkeypatch):
    data=b'fixture';path=wheel(tmp_path,data)
    monkeypatch.setattr(signer_wheel,'SIGNER_SDK_WHEEL_SHA256',sha256(data).hexdigest())
    real_read,real_close=os.read,os.close
    live=True;closed=[]
    def read_then_deny(fd,maximum):
        nonlocal live
        result=real_read(fd,maximum)
        live=False
        return result
    def tracked_close(fd):
        closed.append(fd)
        return real_close(fd)
    monkeypatch.setattr(signer_wheel.os,'read',read_then_deny)
    monkeypatch.setattr(signer_wheel.os,'close',tracked_close)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        signer_wheel.verify_signer_sdk_wheel(path,check_active=lambda:live)
    assert len(closed)>=2


def test_boolean_denial_after_wheel_stat_skips_final_path_resolution(tmp_path, monkeypatch):
    data=b'fixture';path=wheel(tmp_path,data)
    monkeypatch.setattr(signer_wheel,'SIGNER_SDK_WHEEL_SHA256',sha256(data).hexdigest())
    real_stat=os.stat;real_resolve=type(path).resolve
    live=True;resolutions=0
    def stat(name,*args,**kwargs):
        nonlocal live
        result=real_stat(name,*args,**kwargs)
        if name==path.name and kwargs.get('dir_fd') is not None:live=False
        return result
    def resolve(candidate,*args,**kwargs):
        nonlocal resolutions
        if candidate==path:resolutions+=1
        return real_resolve(candidate,*args,**kwargs)
    monkeypatch.setattr(signer_wheel.os,'stat',stat)
    monkeypatch.setattr(type(path),'resolve',resolve)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        signer_wheel.verify_signer_sdk_wheel(path,check_active=lambda:live)
    assert resolutions==1


def test_replacement_during_read_refused_even_for_same_bytes(tmp_path, monkeypatch):
    data = b'fixture'
    path = wheel(tmp_path, data)
    monkeypatch.setattr(signer_wheel, 'SIGNER_SDK_WHEEL_SHA256', sha256(data).hexdigest())
    real_read = os.read
    replaced = False
    def replace_after_read(descriptor, maximum):
        nonlocal replaced
        block = real_read(descriptor, maximum)
        if block and not replaced:
            replaced = True
            path.rename(path.parent/'old.whl')
            path.write_bytes(data)
            path.chmod(0o600)
        return block
    monkeypatch.setattr(signer_wheel.os, 'read', replace_after_read)
    with pytest.raises(ValueError, match='SIGNER_SDK_WHEEL_CHANGED'):
        signer_wheel.verify_signer_sdk_wheel(path)


def test_symlink_ancestor_refused_before_wheel_open(tmp_path):
    real = tmp_path.resolve()/'real'
    real.mkdir(mode=0o700)
    path = wheel(real)
    alias = tmp_path.resolve()/'alias'
    alias.symlink_to(real, target_is_directory=True)
    with pytest.raises((ValueError, OSError)):
        signer_wheel.verify_signer_sdk_wheel(alias/path.name)
