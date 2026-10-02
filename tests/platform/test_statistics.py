import numpy as np
import pytest
from photo_worker.algorithms.adaptation import compute_stats20
from photo_worker.algorithms.statistics import compute_stats20_bounded
from photo_worker.algorithms.color import to_linear


@pytest.mark.parametrize('shape',[(1,1),(17,29),(71,16791)])
def test_bounded_stats_matches_reference_and_preserves_inputs(tmp_path,shape):
    rng=np.random.default_rng(21)
    rgb=rng.random((*shape,3),dtype=np.float32);person=rng.random(shape,dtype=np.float32)
    valid=rng.random(shape)>.2;valid[0,0]=True
    reference=compute_stats20(rgb,person,valid)
    workspace=np.memmap(tmp_path/'luma.bin',mode='w+',dtype=np.float64,shape=(rgb.shape[0]*rgb.shape[1],))
    rgb.flags.writeable=False;person.flags.writeable=False;valid.flags.writeable=False
    try:
        actual=compute_stats20_bounded(rgb,person,workspace,valid=valid)
        assert np.max(np.abs(actual.values-reference.values))<1e-11
        assert np.array_equal(actual.coverage_bins,reference.coverage_bins)
    finally:workspace._mmap.close()


def test_gamma_stats_matches_explicit_linearization():
    rng=np.random.default_rng(1);rgb=rng.random((32,31,3),dtype=np.float32);mask=np.ones((32,31),np.float32)
    expected=compute_stats20(to_linear(rgb),mask)
    actual=compute_stats20_bounded(rgb,mask,np.empty(32*31),encoding='gamma24')
    assert np.max(np.abs(actual.values-expected.values))<1e-12


def test_stats_cancel_empty_and_invalid_workspace():
    rgb=np.zeros((16,16,3),np.float32);mask=np.ones((16,16),np.float32)
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):compute_stats20_bounded(rgb,mask,np.empty(256),check_active=cancelled)
    with pytest.raises(ValueError,match='EMPTY'):compute_stats20_bounded(rgb,mask,np.empty(256),valid=np.zeros((16,16),bool))
    with pytest.raises(ValueError,match='WORKSPACE'):compute_stats20_bounded(rgb,mask,np.empty(2))
    mask[0,0]=2
    with pytest.raises(ValueError,match='MASK_RANGE'):compute_stats20_bounded(rgb,mask,np.empty(256))


def test_bounded_stats_all_transparent_has_no_features():
    rgb=np.ones((8,8,3),np.float32);person=np.ones((8,8),np.float32)
    with pytest.raises(ValueError,match='EMPTY'):
        compute_stats20_bounded(rgb,person,np.empty(64),alpha=np.zeros((8,8),np.uint16))


def test_exact_quantiles_do_not_copy_the_full_disk_workspace(tmp_path):
    import tracemalloc
    rgb=np.full((2048,2048,3),.3,np.float32);person=np.zeros((2048,2048),np.float32)
    workspace=np.memmap(tmp_path/'large-luma.bin',mode='w+',dtype=np.float64,shape=(2048*2048,))
    tracemalloc.start()
    try:
        result=compute_stats20_bounded(rgb,person,workspace)
        _,peak=tracemalloc.get_traced_memory()
        assert peak<16*1024*1024
        assert np.max(np.abs(result.values[:5]-float(np.float32(.3))))<1e-12
    finally:
        tracemalloc.stop();workspace._mmap.close()


def test_alpha_cannot_alias_mutable_luma_workspace():
    workspace=np.zeros(64,np.float64)
    alpha=workspace.view(np.uint16)[:64].reshape(8,8)
    with pytest.raises(ValueError,match='WORKSPACE'):
        compute_stats20_bounded(np.ones((8,8,3),np.float32),np.ones((8,8),np.float32),workspace,alpha=alpha)
    assert not workspace.any()


def test_noncontiguous_stats_preserves_quadrant_coordinates():
    rng=np.random.default_rng(17)
    rgb=rng.random((63,87,3),dtype=np.float32)[::2,::3]
    person=rng.random((63,87),dtype=np.float32)[::2,::3]
    valid=(rng.random((63,87))>.2)[::2,::3]
    reference=compute_stats20(rgb,person,valid)
    result=compute_stats20_bounded(rgb,person,np.empty(valid.size),valid=valid)
    assert np.max(np.abs(reference.values-result.values))<1e-11
    assert np.array_equal(reference.coverage_bins,result.coverage_bins)


def test_tall_narrow_stats_uses_bounded_cross_row_chunks():
    rgb=np.full((100000,1,3),.25,np.float32);person=np.ones((100000,1),np.float32)
    calls=0
    def active():
        nonlocal calls
        calls+=1
    result=compute_stats20_bounded(rgb,person,np.empty(100000),check_active=active)
    assert calls<20 and result.values[15]==1.
