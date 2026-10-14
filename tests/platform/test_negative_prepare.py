from uuid import uuid4
import numpy as np
import pytest
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.optical.negative_prepare import prepare_negative_files
from test_negative_frame import fixture


@pytest.mark.parametrize('mode',['ok','zero','cancel','ram'])
def test_negative_attempt_files_cleanup_and_real_mask(tmp_path,mode):
    p,raw=fixture();scan=np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3)
    if mode=='zero':p['strength']=0
    calls=[]
    def active():
        calls.append(1)
        if mode=='cancel' and len(calls)==4:raise ValueError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        def run():
            with prepare_negative_files(scan,p,raw,scratch,input_icc_sha='c'*64,epsilon=1e-6,
                    ram_max_bytes=1 if mode=='ram' else 1<<30,check_active=active) as files:
                rgb=np.load(files.rgb);mask=np.load(files.changed_mask)
                if mode=='zero':np.testing.assert_array_equal(rgb,scan)
                else:np.testing.assert_allclose(rgb,0,atol=1e-7)
                np.testing.assert_array_equal(mask,0 if mode=='zero' else 1)
                assert files.changed_pixels==(0 if mode=='zero' else 1) and files.review_required
                files.reservation.validate()
        if mode in ('cancel','ram'):
            with pytest.raises(ValueError):run()
        else:run()
        assert not list(scratch.directory.iterdir())


def test_pinned_negative_preparation_owns_runtime_during_staged_use(tmp_path):
    from photo_worker.operations.optical.negative_prepare import prepare_pinned_negative_files
    from photo_worker.contracts.base import canonical_hash
    from test_negative_runtime_lock import lock
    p,raw=fixture();runtime=lock();digest=canonical_hash(runtime)
    scan=np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        with prepare_pinned_negative_files(scan,p,raw,scratch,input_icc_sha='c'*64,
                runtime_lock=runtime,runtime_sha=digest,ram_max_bytes=1<<30,check_active=lambda:None) as files:
            runtime.epsilon=1e-7;runtime.components.clear()
            np.testing.assert_allclose(np.load(files.rgb),0,atol=1e-7)
        assert not list(scratch.directory.iterdir())


def test_bad_runtime_pin_refused_before_input_or_scratch(tmp_path):
    from photo_worker.operations.optical.negative_prepare import prepare_pinned_negative_files
    from photo_worker.runtime.executor import OperationFailure
    from test_negative_runtime_lock import lock
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
            with prepare_pinned_negative_files(None,None,None,scratch,input_icc_sha='c'*64,
                    runtime_lock=lock(),runtime_sha='0'*64,ram_max_bytes=1<<30,check_active=lambda:None):
                pytest.fail('Invalid runtime yielded output')
        assert not list(scratch.directory.iterdir())


def test_pinned_negative_preparation_owns_inputs_before_first_callback(tmp_path):
    from photo_worker.operations.optical.negative_prepare import prepare_pinned_negative_files
    from photo_worker.contracts.base import canonical_hash
    from test_negative_runtime_lock import lock
    p,raw=fixture();raw=bytearray(raw);runtime=lock();digest=canonical_hash(runtime)
    scan=np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3)
    calls=[]
    def active():
        if not calls:
            p['strength']=0
            p['calibration']['sha256']='0'*64
            raw[:]=b'{}'
            runtime.epsilon=1e-7
            runtime.components.clear()
        calls.append(1)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        with prepare_pinned_negative_files(scan,p,raw,scratch,input_icc_sha='c'*64,
                runtime_lock=runtime,runtime_sha=digest,ram_max_bytes=1<<30,check_active=active) as files:
            np.testing.assert_allclose(np.load(files.rgb),0,atol=1e-7)
            np.testing.assert_array_equal(np.load(files.changed_mask),1)
            assert files.changed_pixels==1
        assert not list(scratch.directory.iterdir())
    assert calls
