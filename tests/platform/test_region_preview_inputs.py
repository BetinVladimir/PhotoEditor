from contextlib import contextmanager, nullcontext
from types import SimpleNamespace
import json
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.base import canonical_hash
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from uuid import uuid4
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.inputs import prepare_region_source
from test_region_preview_contract import region_fixture


def test_region_source_maps_full_canvas_and_releases_input(tmp_path):
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(json.dumps(plan))
    path=tmp_path/'pixels.npy'
    pixels=np.full((300,400,3),1.5,np.float32)
    np.save(path,pixels,allow_pickle=False)
    events=[]
    class Artifacts:
        @contextmanager
        def input(self,ref,*,suffix):
            assert ref==params.validate_source(canonical_bytes(image)).pixels
            events.append('open')
            try:yield SimpleNamespace(path=path)
            finally:events.append('close')
    with prepare_region_source(params,canonical_bytes(image),Artifacts(),alpha_mode='require_float32',max_normalization_bytes=0) as source:
        assert source.rgb.shape==(300,400,3)
        assert np.array_equal(source.rgb,pixels)
        assert source.alpha is None
        mapping=source.rgb
    assert mapping._mmap.closed
    assert events==['open','close']


@pytest.mark.parametrize('case',['source','mode','budget','shape','nonfinite'])
def test_region_source_rejects_bad_bytes_policy_and_arrays(tmp_path,case):
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(json.dumps(plan))
    raw=canonical_bytes(image)
    path=tmp_path/'pixels.npy'
    pixels=np.zeros((300,400,3) if case!='shape' else (10,10,3),np.float32)
    if case=='nonfinite':pixels[-1,-1,0]=np.nan
    np.save(path,pixels,allow_pickle=False)
    events=[]
    class Artifacts:
        @contextmanager
        def input(self,ref,*,suffix):
            events.append('open')
            try:yield SimpleNamespace(path=path)
            finally:events.append('close')
    if case=='source':raw+=b' '
    with pytest.raises(ValueError):
        with prepare_region_source(params,raw,Artifacts(),alpha_mode='bad' if case=='mode' else 'require_float32',max_normalization_bytes=-1 if case=='budget' else 0):
            pytest.fail('invalid source accepted')
    assert events==([] if case in ('source','mode','budget') else ['open','close'])


def alpha_fixture(tmp_path,dtype):
    plan,image=region_fixture()
    alpha_ref=dict(artifact_id=str(uuid4()),sha256='f'*64)
    image['alpha']=alpha_ref
    source_body=canonical_bytes(image)
    plan['source']['ref']['sha256']=canonical_hash(image)
    plan['source']['bytes']=len(source_body)
    plan['recipe']['ref']['sha256']=canonical_hash(plan['frozen_recipe'])
    plan['recipe']['bytes']=len(canonical_bytes(plan['frozen_recipe']))
    plan['inputs'].append(dict(ref=alpha_ref,contract='NpyArray'))
    plan['inputs'].sort(key=lambda entry:entry['ref']['artifact_id'])
    params=RegionPreviewParams.model_validate_json(json.dumps(plan))
    pixels_path=tmp_path/'rgb.npy';alpha_path=tmp_path/'alpha.npy'
    np.save(pixels_path,np.ones((300,400,3),np.float32),allow_pickle=False)
    values=np.resize(np.array([0,1,32768,65535],np.uint16),(300,400))
    if dtype=='float32':values=values.astype(np.float32)/np.float32(65535)
    np.save(alpha_path,values,allow_pickle=False)
    events=[]
    class Artifacts:
        @contextmanager
        def input(self,ref,*,suffix):
            role='rgb' if str(ref.artifact_id)==str(image['pixels']['artifact_id']) else 'alpha'
            events.append(('open',role))
            try:yield SimpleNamespace(path=pixels_path if role=='rgb' else alpha_path)
            finally:events.append(('close',role))
    return params,source_body,values,Artifacts(),events


@pytest.mark.parametrize('dtype',['uint16','float32'])
@pytest.mark.parametrize('abort',[False,True])
def test_region_alpha_precision_and_context_cleanup(tmp_path,dtype,abort):
    params,body,expected,artifacts,events=alpha_fixture(tmp_path,dtype)
    artifacts.scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,10_000_000,min_free_bytes=0)
    try:
        with pytest.raises(RuntimeError,match="render aborted") if abort else nullcontext():
            with prepare_region_source(params,body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=300*400*4+4096) as source:
                assert source.alpha.dtype==np.float32
                target=expected.astype(np.float32)/np.float32(65535) if dtype=='uint16' else expected
                assert np.array_equal(source.alpha,target)
                mapped=source.alpha
                if abort:raise RuntimeError("render aborted")
        assert mapped._mmap.closed
        assert events==[('open','rgb'),('open','alpha'),('close','alpha'),('close','rgb')]
        with artifacts.scratch.reserve(10_000_000):
            pass  # entire budget is available again after conversion
    finally:artifacts.scratch.cleanup()


def test_region_alpha_budget_rejected_before_io(tmp_path):
    params,body,_,artifacts,events=alpha_fixture(tmp_path,'uint16')
    with pytest.raises(ValueError,match='REGION_ALPHA_BUDGET_INVALID'):
        with prepare_region_source(params,body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=300*400*4):
            pytest.fail('missing header budget accepted')
    assert events==[]


def test_region_legacy_alpha_requires_explicit_conversion(tmp_path):
    params,body,_,artifacts,events=alpha_fixture(tmp_path,'uint16')
    with pytest.raises(ValueError):
        with prepare_region_source(params,body,artifacts,alpha_mode='require_float32',max_normalization_bytes=0):
            pytest.fail('implicit alpha conversion')
    assert events==[('open','rgb'),('open','alpha'),('close','alpha'),('close','rgb')]


def test_region_source_resource_gate_runs_before_materialization(tmp_path):
    params,body,_,artifacts,events=alpha_fixture(tmp_path,'uint16')
    profile=dict(resource_estimate=dict(pixel_count=120000,ram_bytes=1,scratch_bytes=1))
    with pytest.raises(OperationFailure):
        with prepare_region_source(params,body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=300*400*4+4096,execution_profile=profile):
            pytest.fail('resource gate bypassed')
    assert events==[]


def test_region_cancellation_inside_alpha_conversion_releases_scratch(tmp_path):
    params,body,_,artifacts,events=alpha_fixture(tmp_path,'uint16')
    class TrackedScratch(ScratchSpace):
        converting=False
        @contextmanager
        def reserve(self,size):
            with super().reserve(size) as reservation:
                self.converting=True
                yield reservation
    artifacts.scratch=TrackedScratch(tmp_path,uuid4(),uuid4(),1,10_000_000,min_free_bytes=0)
    def check():
        if artifacts.scratch.converting:raise RuntimeError('attempt cancelled')
    try:
        with pytest.raises(RuntimeError,match='attempt cancelled'):
            with prepare_region_source(params,body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=300*400*4+4096,check_active=check):
                pytest.fail('cancelled conversion yielded')
        assert artifacts.scratch.converting
        assert events==[('open','rgb'),('open','alpha'),('close','alpha'),('close','rgb')]
        with artifacts.scratch.reserve(10_000_000):pass
    finally:artifacts.scratch.cleanup()
