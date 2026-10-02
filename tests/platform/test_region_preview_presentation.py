from hashlib import sha256
from pathlib import Path
from uuid import uuid4

import numpy as np
import pytest
from PIL import Image

from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.presentation import prepare_region_presentation,write_region_presentation
from test_region_preview_contract import region_fixture

ROOT=Path(__file__).resolve().parents[2]/'resources/icc'
ICC_LOCK=ICCRegistry(ROOT).resolve('work_rec2020_gamma24_v1').sha256


def fixture(alpha=False):
    plan,image=region_fixture()
    registry=ICCRegistry(ROOT)
    if alpha:
        image['alpha']=dict(artifact_id=str(uuid4()),sha256='f'*64)
        plan['inputs'].append(dict(ref=image['alpha'],contract='NpyArray'))
        plan['inputs'].sort(key=lambda item:item['ref']['artifact_id'])
    image['color']['icc_sha256']=registry.resolve(image['color']['profile_id']).sha256
    plan['source']['ref']['sha256']=canonical_hash(image)
    plan['frozen_recipe']['icc_lock_sha']=ICC_LOCK
    plan['recipe']['ref']['sha256']=canonical_hash(plan['frozen_recipe'])
    return RegionPreviewParams.model_validate_json(canonical_bytes(plan)),canonical_bytes(image)


def test_region_conversion_and_encoded_icc_match_existing_lcms(tmp_path):
    params,body=fixture()
    rgb=np.full((300,400,3),.5,np.float32)
    result=prepare_region_presentation(params,body,rgb,None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    registry=ICCRegistry(ROOT)
    expected=LittleCMS().convert(np.round(rgb*65535).astype(np.uint16),registry.resolve('work_rec2020_gamma24_v1').data,registry.resolve('srgb_v1').data).astype(np.float32)/65535
    assert np.array_equal(result.rgb,expected)
    assert len(result.cache_key)==64 and result.media_type=='image/jpeg'
    path=tmp_path/'region.jpg'
    assert write_region_presentation(path,result)>0
    with Image.open(path) as encoded:
        assert encoded.size==(400,300)
        assert encoded.info['icc_profile']==result.profile
        assert not encoded.info.get('exif')
    assert prepare_region_presentation(params,body,rgb,None,ROOT,runtime_icc_lock_sha=ICC_LOCK).cache_key==result.cache_key


def test_region_icc_lock_is_required_before_extraction():
    params,body=fixture()
    params.frozen_recipe.icc_lock_sha='a'*64
    with pytest.raises(ValueError,match='ICC_LOCK'):
        prepare_region_presentation(params,body,np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)


def test_region_source_icc_binding_is_checked():
    plan,image=region_fixture()
    plan['frozen_recipe']['icc_lock_sha']=ICC_LOCK
    plan['recipe']['ref']['sha256']=canonical_hash(plan['frozen_recipe'])
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    with pytest.raises(ValueError,match='ICC_SOURCE'):
        prepare_region_presentation(params,canonical_bytes(image),np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)


def test_region_alpha_webp_retains_profile_and_removes_metadata(tmp_path):
    params,body=fixture(alpha=True)
    rgb=np.full((300,400,3),.25,np.float32)
    alpha=np.linspace(0,1,300*400,dtype=np.float32).reshape(300,400)
    result=prepare_region_presentation(params,body,rgb,alpha,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    assert result.media_type=='image/webp' and np.array_equal(result.alpha,alpha)
    path=tmp_path/'region.webp'
    assert write_region_presentation(path,result)>0
    with Image.open(path) as encoded:
        assert encoded.mode=='RGBA' and encoded.size==(400,300)
        assert encoded.info['icc_profile']==result.profile
        assert not encoded.info.get('exif') and not encoded.info.get('xmp')
    with pytest.raises(ValueError,match='SUFFIX'):
        write_region_presentation(tmp_path/'wrong.jpg',result)


def test_runtime_icc_pin_is_not_inferred_from_manifest_file_hash():
    params,body=fixture()
    assert params.frozen_recipe.icc_lock_sha==ICC_LOCK
    assert ICC_LOCK!=sha256((ROOT/'profiles.lock.json').read_bytes()).hexdigest()
    rgb=np.zeros((300,400,3),np.float32)
    prepare_region_presentation(params,body,rgb,None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    with pytest.raises(ValueError,match='ICC_LOCK'):
        prepare_region_presentation(params,body,rgb,None,ROOT,
            runtime_icc_lock_sha=sha256((ROOT/'profiles.lock.json').read_bytes()).hexdigest())
