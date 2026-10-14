from contextlib import contextmanager
from dataclasses import replace
from hashlib import sha256
from io import BytesIO
import pytest

from photo_worker.runtime.restore_inventory import RestoreObject, verify_restore_inventory


def reference(data=b'image'):
    return RestoreObject('photos', 'tenant/artifact', 'immutable-version', sha256(data).hexdigest(), len(data))


def test_restore_inventory_streams_exact_versions_and_owns_evidence():
    data=b'x'*(2*1024*1024+7)
    ref=reference(data)
    reads=[]
    class Stream(BytesIO):
        def read(self, size=-1):
            assert 0 < size <= 1 << 20
            reads.append(size)
            return super().read(size)
    stream=Stream(data)
    @contextmanager
    def source(got):
        assert got == ref
        try: yield stream
        finally: stream.close()
    result=verify_restore_inventory([ref],source,check_active=lambda:None)
    assert result.objects==1 and result.bytes_verified==len(data)
    assert stream.closed and len(reads)==4
    assert result==verify_restore_inventory([ref],lambda _:BytesIO(data),check_active=lambda:None)


@pytest.mark.parametrize('mode',['short','long','hash','missing','cancelled'])
def test_restore_inventory_refuses_missing_or_changed_bytes_and_closes_stream(mode):
    ref=reference();stream=BytesIO(b'image')
    if mode=='short':stream=BytesIO(b'imag')
    if mode=='long':stream=BytesIO(b'images')
    if mode=='hash':stream=BytesIO(b'other')
    checks=0
    def active():
        nonlocal checks
        checks+=1
        if mode=='cancelled' and checks==3:raise RuntimeError('cancelled')
    def source(_):
        if mode=='missing':raise FileNotFoundError('missing version')
        return stream
    with pytest.raises((ValueError,FileNotFoundError,RuntimeError)):
        verify_restore_inventory([ref],source,check_active=active)
    if mode not in ('missing',):assert stream.closed


@pytest.mark.parametrize('mode',['duplicate','reordered','null_version','negative','boolean','sha','budget','count'])
def test_restore_inventory_catalogue_refusal(mode):
    ref=reference();refs=[ref];kwargs={}
    if mode=='duplicate':refs=[ref,ref]
    if mode=='reordered':refs=[replace(ref,key='z'),ref]
    if mode=='null_version':refs=[replace(ref,version='null')]
    if mode=='negative':refs=[replace(ref,size=-1)]
    if mode=='boolean':refs=[replace(ref,size=True)]
    if mode=='sha':refs=[replace(ref,sha256='A'*64)]
    if mode=='budget':kwargs['maximum_bytes']=4
    if mode=='count':refs=[ref,replace(ref,key='z')];kwargs['maximum_objects']=1
    with pytest.raises(ValueError):
        verify_restore_inventory(refs,lambda _:BytesIO(b'image'),check_active=lambda:None,**kwargs)


@pytest.mark.parametrize('denial_at',[1,4,8])
def test_restore_inventory_explicit_false_never_returns_evidence(denial_at):
    checks=0;opened=[]
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    def source(_):
        stream=BytesIO(b'image');opened.append(stream);return stream
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        verify_restore_inventory([reference()],source,check_active=active)
    if denial_at==1:assert opened==[]
    else:assert opened and all(stream.closed for stream in opened)
