import hashlib
import io
import json
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence
from photo_worker.v2.portrait_donor_material import read_donor_image_material
from photo_worker.v2.portrait_open_eyes_material import prepare_open_eyes_material_patch
from test_portrait_donor_images import manifest_arguments


class Reader:
    def __init__(self,ref,body):
        self.ref,self.body=ref,body
    def evidence(self,ref):
        assert ref==self.ref
        return ArtifactEvidence(ref,'application/x-npy',len(self.body),None,'input')
    def iter_bytes(self,ref,max_bytes):
        for i in range(0,len(self.body),65536):
            yield self.body[i:i+65536]


def case():
    args=manifest_arguments()
    image=json.loads(args[1]);image['alpha']=None
    stream=io.BytesIO();np.save(stream,np.ones((100,100,3),np.float32),allow_pickle=False)
    body=stream.getvalue();image['pixels']['sha256']=hashlib.sha256(body).hexdigest()
    raw=canonical_bytes(image);params=args[0]
    for binding in (params.base,params.donor):
        binding.source.sha256=hashlib.sha256(raw).hexdigest()
    ref=ArtifactRef.parse(image['pixels'])
    return params,raw,Reader(ref,body)


def test_material_is_sha_verified_readonly_and_scratch_is_cleaned(tmp_path):
    params,raw,reader=case()
    with read_donor_image_material(params,raw,raw,reader,scratch_root=tmp_path,
            max_bytes=1_000_000,check_active=lambda:None,alpha_mode='require_float32') as material:
        np.testing.assert_array_equal(material.source_rgb,1)
        assert not material.donor_rgb.flags.writeable
        assert material.source_alpha is None
        assert list(tmp_path.iterdir())
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',['sha','budget'])
def test_corrupt_or_over_budget_material_returns_no_arrays(tmp_path,failure):
    params,raw,reader=case()
    if failure=='sha':reader.body=reader.body[:-1]+b'X'
    with pytest.raises(ValueError,match='ARRAY_BYTES_INVALID|SCRATCH_EXCEEDED'):
        with read_donor_image_material(params,raw,raw,reader,scratch_root=tmp_path,
                max_bytes=1 if failure=='budget' else 1_000_000,
                check_active=lambda:None,alpha_mode='require_float32'):
            pytest.fail('Invalid material returned')
    assert not list(tmp_path.iterdir())


def test_physical_material_drives_patch_and_stays_open_until_output_consumed(tmp_path):
    params,raw,reader=case()
    args=manifest_arguments()
    proof=json.loads(args[3])
    proof['source']=params.base.model_dump(mode='json')
    proof['donor']=params.donor.model_dump(mode='json')
    proof_raw=canonical_bytes(proof)
    params.confirmation.sha256=hashlib.sha256(proof_raw).hexdigest()
    faces=[]
    for body,binding,ref in zip(args[4:6],(params.base,params.donor),
                              (params.source_faces,params.donor_faces)):
        doc=json.loads(body);doc['binding']=binding.model_dump(mode='json')
        body=canonical_bytes(doc);ref.sha256=hashlib.sha256(body).hexdigest();faces.append(body)
    calls=[]
    with prepare_open_eyes_material_patch(params,raw,raw,proof_raw,*faces,reader,
            args[8],args[9],source_origin=(0,0),min_face_edge_px=40,
            scratch_root=tmp_path,max_bytes=1_000_000,check_active=lambda:None,
            authority=lambda p,r:calls.append(1) or True,alpha_mode='require_float32') as (result,material):
        assert len(calls)==4 and result.requires_review
        np.testing.assert_array_equal(result.pixels,1)
        np.testing.assert_array_equal(material.source_rgb,1)
        assert list(tmp_path.iterdir())
    assert not list(tmp_path.iterdir())


def test_material_processing_denies_before_reader_access(tmp_path):
    args=manifest_arguments()
    with pytest.raises(ValueError,match='DONOR_AUTHORITY'):
        with prepare_open_eyes_material_patch(*args[:6],None,args[8],args[9],
                source_origin=(0,0),min_face_edge_px=40,scratch_root=tmp_path,
                max_bytes=1_000_000,check_active=lambda:None,authority=lambda p,r:False,
                alpha_mode='require_float32'):
            pytest.fail('Denied operation yielded output')
    assert not list(tmp_path.iterdir())
