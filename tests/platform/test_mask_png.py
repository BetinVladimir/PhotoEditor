from io import BytesIO
import numpy as np
import pytest
from PIL import Image
from photo_worker.algorithms.mask_png import write_png16,png16_bound

@pytest.mark.parametrize('floating',[False,True])
def test_png16_lossless_roundtrip(floating):
    raw=np.random.default_rng(8).integers(0,65536,(9,40001),dtype=np.uint16)
    raw[0,:4]=[0,1,32768,65535]
    source=raw.astype(np.float64)/65535 if floating else raw
    stream=BytesIO();size=write_png16(source,stream,max_bytes=png16_bound(40001,9))
    assert size==len(stream.getvalue())
    stream.seek(0)
    with Image.open(stream) as image:
        assert image.format=='PNG' and image.size==(40001,9)
        assert np.array_equal(np.asarray(image),raw)


def test_strided_mask_and_partial_writer():
    class Partial(BytesIO):
        def write(self,data):return super().write(data[:7])
    source=np.arange(100,dtype=np.uint16).reshape(10,10)[:,::2]
    stream=Partial();write_png16(source,stream,max_bytes=10000)
    stream.seek(0)
    with Image.open(stream) as image:assert np.array_equal(np.asarray(image),source)


def test_budget_invalid_probability_and_cancel():
    source=np.ones((10,10),np.float32)
    with pytest.raises(ValueError,match='BUDGET'):write_png16(source,BytesIO(),max_bytes=10)
    for value in (np.nan,np.inf,-.1,1.1):
        with pytest.raises(ValueError,match='probability'):write_png16(np.full_like(source,value),BytesIO(),max_bytes=10000)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):write_png16(source,BytesIO(),max_bytes=10000,check_active=cancel)


def test_bounded_encoder_memory():
    import tracemalloc
    class Sink:
        def write(self,data):return len(data)
    source=np.random.default_rng(9).random((1024,2048),dtype=np.float32)
    tracemalloc.start()
    try:
        write_png16(source,Sink(),max_bytes=png16_bound(2048,1024))
        peak=tracemalloc.get_traced_memory()[1]
    finally:tracemalloc.stop()
    assert peak<2*1024*1024
