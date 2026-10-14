from contextlib import contextmanager
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
from PIL import Image
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.assets import DecodeParams
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.asset.verify import verify_decode
from photo_worker.operations.asset.decode import icc_to_work
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_background_decode import normalize_background_asset,background_decode_peak


def test_background_normalization_matches_existing_decoder(tmp_path):
    registry=ICCRegistry(Path(__file__).parents[2]/'resources/icc')
    path=tmp_path/'source.png'
    Image.new('RGB',(29,17),(110,70,210)).save(path,icc_profile=registry.resolve('srgb_v1').data)
    pin=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    params=DecodeParams(source=pin,expected_bytes=path.stat().st_size,suffix='.png')
    expected=icc_to_work(verify_decode(path),tmp_path/'expected.npy',registry)
    @contextmanager
    def input(ref,*,suffix):
        assert ref==pin and suffix=='.png'
        yield SimpleNamespace(path=path)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:
        with normalize_background_asset(SimpleNamespace(input=input,scratch=scratch),params,registry,
                execution_profile={"resource_estimate":background_decode_peak(29,17,params.expected_bytes)},
                retained_resources={"ram_bytes":0,"scratch_bytes":0}) as result:
            output=result.image.rgb_path
            np.testing.assert_array_equal(result.rgb,np.load(expected.rgb_path))
            assert not result.rgb.flags.writeable
            assert result.image.profile_id=='work_rec2020_gamma24_v1'
            assert result.image.source_sha256==pin.sha256
        assert not output.exists()


def test_underbudget_background_rejected_before_native_decode(tmp_path,monkeypatch):
    import pytest
    from photo_worker.runtime.executor import OperationFailure
    import photo_worker.v2.render_background_decode as module
    registry=ICCRegistry(Path(__file__).parents[2]/'resources/icc')
    path=tmp_path/'source.png';Image.new('RGB',(31,19),(1,2,3)).save(path)
    params=DecodeParams(source=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest()),
        expected_bytes=path.stat().st_size,suffix='.png',assume_profile='srgb_v1')
    @contextmanager
    def input(ref,*,suffix):yield SimpleNamespace(path=path)
    def forbidden(*args,**kwargs):pytest.fail('native decoder reached before admission')
    monkeypatch.setattr(module,'icc_to_work',forbidden)
    peak=background_decode_peak(31,19,params.expected_bytes)
    retained=dict(ram_bytes=4000,scratch_bytes=5000)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:
        for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED')]:
            estimate={k:peak[k]+retained[k] for k in peak};estimate[key]-=1
            with pytest.raises(OperationFailure,match=code):
                with normalize_background_asset(SimpleNamespace(input=input,scratch=scratch),params,registry,
                        execution_profile={'resource_estimate':estimate},retained_resources=retained):
                    pytest.fail('underbudget decoder accepted')
        assert not list(root.rglob('*.npy'))


def test_invalid_background_budget_rejected_before_input_io():
    import pytest
    from photo_worker.runtime.executor import OperationFailure
    @contextmanager
    def forbidden(*args,**kwargs):
        pytest.fail('input IO before valid admission');yield
    params=DecodeParams(source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),expected_bytes=123,suffix='.png')
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        with normalize_background_asset(SimpleNamespace(input=forbidden),params,None,
                execution_profile={'resource_estimate':{'ram_bytes':True,'scratch_bytes':123}},
                retained_resources={'ram_bytes':0,'scratch_bytes':0}):pytest.fail('invalid budget accepted')
