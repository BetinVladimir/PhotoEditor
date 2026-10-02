"""Bounded binary validation at the lens adoption trust boundary."""
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.lens_adoption import verify_lens_array


class Reader:
    def __init__(self,array,role='pixels'):
        stream=BytesIO();np.save(stream,array,allow_pickle=False)
        self.payload=stream.getvalue();self.role=role;self.closed=False
        self.ref=ArtifactRef(uuid4(),sha256(self.payload).hexdigest())
        self.size=len(self.payload)
    def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',self.size,self.role,'output')
    def iter_bytes(self,ref,limit):
        try:
            for offset in range(0,len(self.payload),7):yield self.payload[offset:offset+7]
        finally:self.closed=True


def test_hdr_values_and_fragmented_header_are_preserved():
    array=np.array([[[-2.,8.,0.],[1.,1.,1.]]],dtype='<f4')
    reader=Reader(array)
    verify_lens_array(reader,reader.ref,array.shape,'pixels')
    assert reader.closed
    np.testing.assert_array_equal(np.load(BytesIO(reader.payload)),array)


@pytest.mark.parametrize('value',[float('nan'),float('inf'),float('-inf')])
def test_nonfinite_output_is_rejected_even_with_matching_sha(value):
    reader=Reader(np.full((2,2),value,dtype='<f4'),'alpha')
    with pytest.raises(AdoptionInvalid,match='VALUES_INVALID'):
        verify_lens_array(reader,reader.ref,(2,2),'alpha',unit=True)
    assert reader.closed


@pytest.mark.parametrize('value',[-.01,1.01])
def test_alpha_unit_bounds_are_enforced(value):
    reader=Reader(np.full((2,2),value,dtype='<f4'),'alpha')
    with pytest.raises(AdoptionInvalid,match='VALUES_INVALID'):
        verify_lens_array(reader,reader.ref,(2,2),'alpha',unit=True)


def test_same_size_wrong_shape_and_sha_mismatch_are_rejected():
    reader=Reader(np.zeros((1,4),dtype='<f4'),'mask_pixels')
    with pytest.raises(AdoptionInvalid,match='HEADER_INVALID'):
        verify_lens_array(reader,reader.ref,(2,2),'mask_pixels',unit=True)
    reader=Reader(np.zeros((2,2),dtype='<f4'),'mask_pixels')
    wrong=ArtifactRef(reader.ref.artifact_id,'a'*64)
    with pytest.raises(AdoptionInvalid,match='BYTES_INVALID'):
        verify_lens_array(reader,wrong,(2,2),'mask_pixels',unit=True)


def test_stream_truncation_is_rejected_and_reader_closed():
    reader=Reader(np.zeros((2,2),dtype='<f4'),'alpha')
    reader.payload=reader.payload[:-1]
    with pytest.raises(AdoptionInvalid,match='BYTES_INVALID'):
        verify_lens_array(reader,reader.ref,(2,2),'alpha',unit=True)
    assert reader.closed


def test_oversize_reader_block_is_rejected():
    reader=Reader(np.zeros((200,200),dtype='<f4'),'alpha')
    reader.iter_bytes=lambda ref,limit:iter([reader.payload])
    with pytest.raises(AdoptionInvalid,match='STREAM_INVALID'):
        verify_lens_array(reader,reader.ref,(200,200),'alpha',unit=True)


def test_version_pinned_reader_closes_and_releases_on_consumer_failure():
    from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
    payload=b'x'*100000
    ref=ArtifactRef(uuid4(),sha256(payload).hexdigest())
    class Connection:
        autocommit=True
        def execute(self,query,args):
            assert 'v2_adoption_artifact' in query
            return self
        def fetchone(self):return ('test','key','exact-version',ref.sha256,len(payload),'application/x-npy','pixels','output')
    class Response(BytesIO):
        released=False
        def release_conn(self):self.released=True
    response=Response(payload)
    class Objects:
        def get_object(self,bucket,key,*,version_id):
            assert (bucket,key,version_id)==('test','key','exact-version')
            return response
    reader=PostgresObjectEvidenceReader(Connection(),Objects(),tenant_id=uuid4(),task_id=uuid4(),attempt=1,fence=1,allowed_bucket='test')
    stream=reader.iter_bytes(ref,len(payload))
    assert len(next(stream))==65536
    stream.close()
    assert response.closed and response.released


def test_version_pinned_reader_refuses_limit_before_object_io():
    from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
    ref=ArtifactRef(uuid4(),'a'*64)
    class Connection:
        autocommit=True
        def execute(self,query,args):return self
        def fetchone(self):return ('test','key','v1',ref.sha256,1024,'application/x-npy','alpha','output')
    class Objects:
        def get_object(self,*args,**kwargs):pytest.fail('Admission failed to bound IO')
    reader=PostgresObjectEvidenceReader(Connection(),Objects(),tenant_id=uuid4(),task_id=uuid4(),attempt=1,fence=1,allowed_bucket='test')
    with pytest.raises(AdoptionInvalid,match='STREAM_TOO_LARGE'):
        list(reader.iter_bytes(ref,1023))


def test_changed_mask_must_be_binary():
    reader=Reader(np.full((2,2),.5,dtype='<f4'),'mask_pixels')
    with pytest.raises(AdoptionInvalid,match='VALUES_INVALID'):
        verify_lens_array(reader,reader.ref,(2,2),'mask_pixels',unit=True,binary=True)


@pytest.mark.parametrize('version',[None,'','null'])
def test_unversioned_object_is_refused_before_io(version):
    from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
    ref=ArtifactRef(uuid4(),'a'*64)
    class Connection:
        autocommit=True
        def execute(self,query,args):return self
        def fetchone(self):return ('test','key',version,ref.sha256,16,'application/x-npy','pixels','output')
    class Objects:
        def get_object(self,*args,**kwargs):pytest.fail('Unpinned object IO')
    reader=PostgresObjectEvidenceReader(Connection(),Objects(),tenant_id=uuid4(),task_id=uuid4(),attempt=1,fence=1,allowed_bucket='test')
    with pytest.raises(AdoptionInvalid,match='EVIDENCE_MISMATCH'):
        list(reader.iter_bytes(ref,16))
