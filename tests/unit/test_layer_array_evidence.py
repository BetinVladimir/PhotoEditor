from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.array_evidence import ArrayEvidenceSession


def fixture(dtype=np.float32):
    stream=BytesIO();np.save(stream,np.array([[0,32768 if dtype==np.uint16 else 1]],dtype),allow_pickle=False)
    raw=stream.getvalue();ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    class Reader:
        calls=0;closed=0;body=raw
        def evidence(self,received):return SimpleNamespace(ref=received,source='input',role=None,media_type='application/x-npy',bytes=len(raw))
        def iter_bytes(self,received,size):
            self.calls+=1
            try:
                for start in range(0,len(self.body),7):yield self.body[start:start+7]
            finally:self.closed+=1
    return Reader(),ref,raw


def test_mapping_is_readonly_cached_and_removed(tmp_path):
    reader,ref,raw=fixture()
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        mapped=session.array(ref,(1,2),source='input',unit=True)
        assert not mapped.flags.writeable
        assert session.array(ref,(1,2),source='input',unit=True) is mapped
        assert reader.calls==reader.closed==1
        np.testing.assert_array_equal(mapped,[[0,1]])
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',['budget','hash','truncated','shape'])
def test_failures_close_stream_and_remove_files(tmp_path,failure):
    reader,ref,raw=fixture()
    if failure=='hash':reader.body=raw[:-1]+b'x'
    if failure=='truncated':reader.body=raw[:-1]
    with pytest.raises((AdoptionInvalid,ValueError)):
        with ArrayEvidenceSession(reader,tmp_path,len(raw)-1 if failure=='budget' else len(raw)) as session:
            session.array(ref,(2,1) if failure=='shape' else (1,2),source='input')
    assert reader.closed==reader.calls
    assert not list(tmp_path.iterdir())


def test_explicit_legacy_alpha_is_exact_budgeted_and_cached(tmp_path):
    reader,ref,raw=fixture(np.uint16)
    with ArrayEvidenceSession(reader,tmp_path,len(raw)+8+4096) as session:
        alpha=session.alpha(ref,(1,2),mode='normalize_uint16')
        np.testing.assert_array_equal(alpha,np.array([[0,32768]],np.float32)/np.float32(65535))
        assert alpha.dtype==np.float32 and not alpha.flags.writeable
        assert session.alpha(ref,(1,2),mode='normalize_uint16') is alpha
        assert reader.calls==reader.closed==1
        assert session.used==len(raw)+8+4096
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',['strict','budget','output','mode'])
def test_legacy_alpha_cannot_bypass_policy_or_budget(tmp_path,failure):
    reader,ref,raw=fixture(np.uint16)
    limit=len(raw)+8+4096-int(failure=='budget')
    with pytest.raises((AdoptionInvalid,ValueError)):
        with ArrayEvidenceSession(reader,tmp_path,limit) as session:
            if failure=='output':session.array(ref,(1,2),source='output',unit=True,legacy_alpha=True)
            else:session.alpha(ref,(1,2),mode='automatic' if failure=='mode' else 'require_float32' if failure=='strict' else 'normalize_uint16')
    assert reader.calls==reader.closed
    if failure in ('output','mode'):assert reader.calls==0
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('source,allowed',[('input',False),('input',True),('output',True)])
def test_legacy_npy_media_requires_explicit_input_policy(tmp_path,source,allowed):
    reader,ref,raw=fixture()
    reader.evidence=lambda received:SimpleNamespace(ref=received,source=source,role=None,media_type='application/octet-stream',bytes=len(raw))
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        if source=='input' and allowed:
            mapped=session.array(ref,(1,2),source=source,legacy_input_npy=allowed)
            np.testing.assert_array_equal(mapped,[[0,1]])
        else:
            with pytest.raises(AdoptionInvalid):
                session.array(ref,(1,2),source=source,legacy_input_npy=allowed)
            assert reader.calls==0
    assert not list(tmp_path.iterdir())
