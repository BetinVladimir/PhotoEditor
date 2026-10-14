from hashlib import sha256
from io import BytesIO
import pytest
from photo_worker.runtime.restore_objects import MinioRestoreObjects
from photo_worker.runtime.restore_inventory import RestoreObject, verify_restore_inventory


class Response:
    def __init__(self):
        self.status=200
        self.headers={'x-amz-version-id':'version-1','content-length':'5'}
        self.stream=BytesIO(b'image')
        self.closed=self.released=False
    def read(self,size,*,decode_content):
        assert decode_content is False
        return self.stream.read(size)
    def close(self):self.closed=True;self.stream.close()
    def release_conn(self):self.released=True


class Client:
    def __init__(self,response):self.response=response;self.calls=[]
    def get_object(self,bucket,key,*,version_id):
        self.calls.append((bucket,key,version_id))
        return self.response


def ref():return RestoreObject('photos','tenant/artifact','version-1',sha256(b'image').hexdigest(),5)


def test_restore_minio_adapter_reads_exact_version_without_decompression():
    response=Response();client=Client(response);buckets=['photos']
    objects=MinioRestoreObjects(client,buckets,check_active=lambda:None);buckets[0]='changed'
    result=verify_restore_inventory([ref()],objects.open_version,check_active=lambda:None)
    assert result.objects==1 and result.bytes_verified==5
    assert client.calls==[('photos','tenant/artifact','version-1')]
    assert response.closed and response.released


@pytest.mark.parametrize('mode',['status','version','length','encoding','missing_version','cancelled','body'])
def test_restore_minio_refuses_unbound_responses_and_always_releases(mode):
    response=Response();client=Client(response);checks=0
    if mode=='status':response.status=206
    if mode=='version':response.headers['x-amz-version-id']='latest'
    if mode=='length':response.headers['content-length']='6'
    if mode=='encoding':response.headers['content-encoding']='gzip'
    if mode=='missing_version':del response.headers['x-amz-version-id']
    if mode=='body':response.stream=BytesIO(b'other')
    def active():
        nonlocal checks
        checks+=1
        if mode=='cancelled' and checks==2:raise RuntimeError('cancelled')
    objects=MinioRestoreObjects(client,['photos'],check_active=active)
    with pytest.raises((ValueError,RuntimeError)):
        verify_restore_inventory([ref()],objects.open_version,check_active=lambda:None)
    assert response.closed and response.released


def test_restore_minio_foreign_bucket_refuses_before_network():
    client=Client(Response());objects=MinioRestoreObjects(client,['other'],check_active=lambda:None)
    with pytest.raises(ValueError):
        with objects.open_version(ref()):pass
    assert client.calls==[]


@pytest.mark.parametrize('denial_at',[1,2,3,4])
def test_restore_minio_explicit_false_refuses_and_releases_response(denial_at):
    response=Response();client=Client(response);checks=0
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    objects=MinioRestoreObjects(client,['photos'],check_active=active)
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        with objects.open_version(ref()) as stream:
            stream.read(5)
    if denial_at==1:assert client.calls==[]
    else:assert response.closed and response.released
