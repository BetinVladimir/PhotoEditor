import hashlib
import json
from pathlib import Path
from uuid import uuid4
import pytest
import numpy as np
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.portrait_donor_images import verify_donor_working_image
from photo_worker.v2 import portrait_open_eyes_prepare as preparation
from test_portrait_open_eyes_prepare import arguments


def fixture():
    path=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json'
    raw=canonical_bytes(json.loads(path.read_text())[0])
    image=WorkingImage.model_validate_json(raw)
    binding=VersionBinding.model_validate_json(json.dumps({'asset_version_id':str(uuid4()),
        'source':{'artifact_id':str(uuid4()),'sha256':hashlib.sha256(raw).hexdigest()},'canvas_hash':canonical_hash(image.canvas)}))
    return raw,binding


def test_image_manifest_binds_canvas_and_dtype():
    raw,binding=fixture()
    image=verify_donor_working_image(raw,binding)
    assert image.dtype=='float32'
    binding.canvas_hash='0'*64
    with pytest.raises(ValueError,match='BINDING'):
        verify_donor_working_image(raw,binding)


def test_changed_bytes_cannot_replace_source_manifest():
    raw,binding=fixture()
    with pytest.raises(ValueError,match='SHA'):
        verify_donor_working_image(raw+b' ',binding)


def manifest_arguments(encoding='linear'):
    args=arguments()
    params=args[0].model_copy(deep=True)
    raw,_=fixture()
    image=WorkingImage.model_validate_json(raw).model_dump(mode='json')
    image['canvas']['width']=100
    image['canvas']['height']=100
    image['transform_chain']={'steps':[], 'hash':canonical_hash({'steps':[]})}
    image['color']['profile_id']='linear_srgb_v1' if encoding=='linear' else 'work_rec2020_gamma24_v1'
    image['color']['encoding']=encoding
    raw=canonical_bytes(image)
    for binding in (params.base,params.donor):
        binding.source.sha256=hashlib.sha256(raw).hexdigest()
        binding.canvas_hash=canonical_hash(image['canvas'])
    proof=json.loads(args[1])
    proof['source']=params.base.model_dump(mode='json')
    proof['donor']=params.donor.model_dump(mode='json')
    proof_raw=canonical_bytes(proof)
    params.confirmation.sha256=hashlib.sha256(proof_raw).hexdigest()
    faces=[]
    for doc_raw,binding,ref in zip(args[2:4],(params.base,params.donor),
                                  (params.source_faces,params.donor_faces)):
        doc=json.loads(doc_raw)
        doc['binding']=binding.model_dump(mode='json')
        face_raw=canonical_bytes(doc)
        ref.sha256=hashlib.sha256(face_raw).hexdigest()
        faces.append(face_raw)
    return (params,raw,raw,proof_raw,*faces,*args[4:8],*args[10:],lambda:None,lambda p,r:True)


def test_manifest_path_derives_dimensions(monkeypatch):
    calls=[]
    def prepare(*args, **kwargs):
        calls.append(args)
        assert kwargs['encoding']=='linear'
        return 'prepared'
    monkeypatch.setattr(preparation,'prepare_open_eyes_patch',prepare)
    assert preparation.prepare_open_eyes_manifest_patch(*manifest_arguments())=='prepared'
    assert calls[0][8:10]==((100,100),(100,100))


def test_manifest_path_passes_verified_gamma_encoding(monkeypatch):
    def prepare(*args, **kwargs):
        assert kwargs['encoding']=='gamma24'
        return 'gamma-prepared'
    monkeypatch.setattr(preparation,'prepare_open_eyes_patch',prepare)
    assert preparation.prepare_open_eyes_manifest_patch(*manifest_arguments('gamma24'))=='gamma-prepared'


@pytest.mark.parametrize('encoding',['linear','gamma24'])
def test_pinned_manifests_to_reviewed_pixels_without_processing_mock(encoding):
    args=list(manifest_arguments(encoding));calls=[]
    args[-1]=lambda p,r:calls.append(1) or True
    original=args[6].copy()
    result=preparation.prepare_open_eyes_manifest_patch(*args)
    assert len(calls)==3 and result.requires_review and not result.pixels.flags.writeable
    assert result.pixels[50,50,0]>1
    np.testing.assert_array_equal(result.pixels.view(np.uint32)[args[8]==0],
                                  original.view(np.uint32)[args[8]==0])
    np.testing.assert_array_equal(args[6],original)


def test_corrupt_manifest_never_reaches_authority():
    args=list(manifest_arguments('gamma24'));args[2]+=b' '
    def forbidden(p,r):
        pytest.fail('Corrupt donor manifest reached authority')
    args[-1]=forbidden
    with pytest.raises(ValueError,match='DONOR_IMAGE_SHA'):
        preparation.prepare_open_eyes_manifest_patch(*args)
