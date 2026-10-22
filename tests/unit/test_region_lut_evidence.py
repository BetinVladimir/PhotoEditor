from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.array_evidence import ArrayEvidenceSession


def lut_reader(values):
    stream=BytesIO();np.save(stream,values,allow_pickle=False)
    raw=stream.getvalue();ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    class Reader:
        calls=0
        def evidence(self,received):
            return SimpleNamespace(ref=received,source='input',role=None,media_type='application/x-npy',bytes=len(raw))
        def iter_bytes(self,received,size):
            self.calls+=1
            for offset in range(0,len(raw),65536):yield raw[offset:offset+65536]
    return Reader(),ref,raw


def test_external_lut_is_hash_verified_readonly_budgeted_and_cached(tmp_path):
    reader,ref,raw=lut_reader(np.zeros((33,33,33,3),np.float32))
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        lut=session.array(ref,(33,33,33,3),source='input',unit=True,lut=True)
        assert lut.shape==(33,33,33,3) and not lut.flags.writeable
        assert session.array(ref,lut.shape,source='input',unit=True,lut=True) is lut
        assert reader.calls==1 and session.used==len(raw)
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',['nan','outside','dtype','shape','budget'])
def test_invalid_physical_lut_is_rejected_and_cleaned(tmp_path,failure):
    values=np.zeros((33,33,33,3),np.float64 if failure=='dtype' else np.float32)
    if failure=='nan':values[4,3,2,1]=np.nan
    if failure=='outside':values[4,3,2,1]=1.001
    if failure=='shape':values=values[:32]
    reader,ref,raw=lut_reader(values)
    with pytest.raises((ValueError,AdoptionInvalid)):
        with ArrayEvidenceSession(reader,tmp_path,len(raw)-int(failure=='budget')) as session:
            session.array(ref,(33,33,33,3),source='input',unit=True,lut=True)
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('options',[{'source':'output','unit':True},{'source':'input','unit':False},
    {'source':'input','unit':True,'inverse_field':True}])
def test_lut_mapping_cannot_relax_other_array_policies(tmp_path,options):
    reader,ref,raw=lut_reader(np.zeros((33,33,33,3),np.float32))
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        with pytest.raises(AdoptionInvalid):session.array(ref,(33,33,33,3),lut=True,**options)
    assert reader.calls==0


def test_label_array_has_explicit_uint16_input_only_policy(tmp_path):
    reader,ref,raw=lut_reader(np.arange(12,dtype=np.uint16).reshape(3,4))
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        labels=session.array(ref,(3,4),source='input',labels=True)
        assert labels.dtype==np.uint16 and not labels.flags.writeable
        np.testing.assert_array_equal(labels,np.arange(12,dtype=np.uint16).reshape(3,4))
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('options',[{'source':'output'},{'source':'input','unit':True},
    {'source':'input','legacy_alpha':True},{'source':'input','inverse_field':True}])
def test_label_array_cannot_relax_float_or_output_contracts(tmp_path,options):
    reader,ref,raw=lut_reader(np.zeros((3,4),np.uint16))
    with ArrayEvidenceSession(reader,tmp_path,len(raw)) as session:
        with pytest.raises(AdoptionInvalid):session.array(ref,(3,4),labels=True,**options)
    assert reader.calls==0
