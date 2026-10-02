import numpy as np
import pytest
from photo_worker.algorithms.masks import compose


def test_composition_clamps_and_protects_exactly():
    target=np.array([[-1.,.2,1.5],[.5,1.,1.]],np.float32)
    exclude=np.array([[0.,.5,0.],[0.,0.,1.]],np.float32)
    protected=np.array([[0.,0.,1.],[.5,0.,0.]],np.float32)
    before=[x.copy() for x in (target,exclude,protected)]
    output=compose(target,exclude,protected)
    assert np.array_equal(output,np.array([[0.,.1,0.],[.25,1.,0.]],np.float32))
    assert all(np.array_equal(a,b) for a,b in zip(before,(target,exclude,protected)))


def test_noncontiguous_and_memmap_output(tmp_path):
    rng=np.random.default_rng(42)
    target=rng.random((31,800),dtype=np.float32)[:,::2]
    protection=rng.random((31,800),dtype=np.float32)[:,::2]
    path=tmp_path/'mask.bin'
    out=np.memmap(path,mode='w+',shape=target.shape,dtype=np.float32)
    try:
        assert compose(target,protected=protection,out=out) is out
        assert np.array_equal(out,target*(1-protection))
    finally:out._mmap.close()


def test_invalid_alias_and_cancel():
    target=np.ones((4,4),np.float32)
    with pytest.raises(ValueError):compose(target,out=target)
    with pytest.raises(ValueError):compose(target,protected=np.ones((3,4),np.float32))
    with pytest.raises(ValueError):compose(target,exclude=np.full((4,4),np.nan))
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):compose(target,check_active=cancel)


def test_bounded_temporary_memory_and_chunk_cancellation():
    import tracemalloc
    target=np.full((1024,2048),.7,np.float32);protected=np.full_like(target,.2);out=np.empty_like(target)
    calls=0
    def active():
        nonlocal calls
        calls+=1
    tracemalloc.start()
    try:
        compose(target,protected=protected,out=out,check_active=active)
        peak=tracemalloc.get_traced_memory()[1]
    finally:tracemalloc.stop()
    assert peak<2*1024*1024 and calls>=128
    assert np.all(out==np.float32(.7)*np.float32(.8))
