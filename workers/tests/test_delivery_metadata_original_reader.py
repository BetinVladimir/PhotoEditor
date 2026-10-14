from contextlib import contextmanager
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.delivery_metadata_original_reader import MetadataOriginalScope,DeliveryMetadataOriginalReader


def fixture():
    original=ArtifactRef(uuid4(),'a'*64)
    scope=MetadataOriginalScope(uuid4(),uuid4(),uuid4(),ArtifactRef(uuid4(),'b'*64),uuid4(),uuid4(),original)
    class Connection:
        autocommit=True;inside=False;calls=[]
        row=(original.artifact_id,original.sha256,2,'photos','key','v1','image/jpeg')
        @contextmanager
        def transaction(self):
            self.inside=True
            try:yield
            finally:self.inside=False
        def execute(self,query,args=()):
            assert self.inside
            self.calls.append((query,args))
            return SimpleNamespace(fetchone=lambda:self.row)
    connection=Connection()
    class Objects:
        calls=[]
        def get_object(self,bucket,key,*,version_id):
            assert not connection.inside
            self.calls.append((bucket,key,version_id));return BytesIO(b'xx')
    objects=Objects()
    return DeliveryMetadataOriginalReader(connection,objects,scope=scope,allowed_bucket='photos'),connection,objects,scope


def test_original_reader_live_acl_and_exact_version_outside_sql():
    reader,connection,objects,scope=fixture()
    assert b''.join(reader.iter_bytes(scope.original,2))==b'xx'
    assert objects.calls==[('photos','key','v1')]
    assert len(connection.calls)==6
    assert connection.calls[2][1]==(scope.tenant,scope.project,scope.version,scope.source.artifact_id,scope.source.sha256,scope.actor,scope.session)
    assert not connection.inside


@pytest.mark.parametrize('mode',['revoked','version','key','size','mime','bucket','digest'])
def test_original_reader_rechecks_permission_and_location(mode):
    reader,connection,objects,scope=fixture()
    reader.evidence(scope.original)
    row=list(connection.row)
    index={'version':5,'key':4,'size':2,'mime':6,'bucket':3,'digest':1}.get(mode)
    if index is not None:row[index]=3 if mode=='size' else 'changed'
    connection.row=None if mode=='revoked' else tuple(row)
    with pytest.raises(AdoptionInvalid):reader.evidence(scope.original)
    assert objects.calls==[]


def test_original_reader_forbids_unrelated_ref_before_sql():
    reader,connection,objects,scope=fixture()
    with pytest.raises(AdoptionInvalid):reader.evidence(ArtifactRef(uuid4(),'c'*64))
    assert connection.calls==[]
