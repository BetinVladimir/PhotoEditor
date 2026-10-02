import json
from pathlib import Path
import tracemalloc
import numpy as np
import pytest
from photo_worker.algorithms.sampling import sample_pixels,split_blocks

FIXTURES=json.loads((Path(__file__).resolve().parents[1]/'fixtures/sampling-reference.json').read_text())

@pytest.mark.parametrize('fixture',FIXTURES)
def test_original_algorithm_digest_preserved(fixture):
    rng=np.random.default_rng(fixture['seed'])
    rgb=rng.random((257,263,3),dtype=np.float32);mask=rng.random((257,263))>.2
    result=sample_pixels(rgb,mask,fixture['seed'],fixture['count'])
    assert result.digest==fixture['digest'] and not result.indices.flags.writeable


def test_noncontiguous_input_and_mask_keep_flat_index_semantics():
    rgb=np.random.default_rng(3).random((129,255,3),dtype=np.float32)[::2,::2]
    mask=np.ones((129,255),bool)[::2,::2]
    one=sample_pixels(rgb,mask,42,2000)
    two=sample_pixels(rgb.copy(),mask.copy(),42,2000)
    assert np.array_equal(one.indices,two.indices)
    assert len(np.unique(one.indices))==2000


def test_cancel_during_scan_and_nonfinite_outside_mask():
    rgb=np.zeros((256,256,3),np.float32);mask=np.zeros((256,256),bool);mask[:128]=True
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):sample_pixels(rgb,mask,42,check_active=active)
    rgb[-1,-1,0]=np.nan
    with pytest.raises(ValueError):sample_pixels(rgb,mask,42)


def test_sample_memory_does_not_materialize_population():
    # Inputs exist before measurement. Old all-pixel indexing/luma temporaries
    # exceed this bound; the new path only keeps ranks and bounded chunks.
    rgb=np.zeros((2048,2048,3),np.float32);mask=np.ones((2048,2048),bool)
    tracemalloc.start()
    try:
        result=sample_pixels(rgb,mask,42)
        _,peak=tracemalloc.get_traced_memory()
    finally:tracemalloc.stop()
    assert len(result.indices)==65536 and peak<32*1024*1024


@pytest.mark.parametrize('count',[1,2000,65536])
def test_gamma24_sampling_matches_explicit_linear_conversion(count):
    from photo_worker.algorithms.color import to_linear
    rng=np.random.default_rng(94)
    encoded=rng.random((271,267,3),dtype=np.float32)
    mask=rng.random((271,267))>.2
    expected=sample_pixels(to_linear(encoded),mask,42,count)
    actual=sample_pixels(encoded,mask,42,count,encoding='gamma24')
    assert actual.digest==expected.digest and np.array_equal(actual.indices,expected.indices)


def test_unknown_sample_encoding_rejected():
    with pytest.raises(ValueError,match='ENCODING'):
        sample_pixels(np.zeros((8,8,3),np.float32),np.ones((8,8),bool),42,encoding='srgb')


def test_spatial_split_excludes_ignore_mask_without_changing_inputs():
    valid=np.ones((80,96),bool);ignore=np.zeros((80,96),np.float32);ignore[:,20:40]=1
    train=np.empty_like(valid);holdout=np.empty_like(valid)
    result=split_blocks(valid,42,exclude_mask=ignore,train_out=train,holdout_out=holdout)
    assert not result.train[:,20:40].any() and not result.holdout[:,20:40].any()
    assert result.train.any() and result.holdout.any() and valid.all()


def test_spatial_split_rejects_invalid_ignore_mask():
    with pytest.raises(ValueError,match='EXCLUDE'):
        split_blocks(np.ones((16,16),bool),42,exclude_mask=np.zeros((8,8),np.float32))
