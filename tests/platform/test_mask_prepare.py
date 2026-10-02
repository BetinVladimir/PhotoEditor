from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.operations.mask.prepare import prepare_raster
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota
from photo_worker.contracts.manual_mask import Stroke
from test_manual_mask import document,point,collect


def test_owned_outputs_and_cleanup(tmp_path):
    doc=document([Stroke(kind='stroke',sequence=0,mode='add',points=[point(.3,.4),point(.8,.7)],radius=.2,hardness=.7)])
    expected=collect(doc,256)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        with prepare_raster(doc,'a'*64,scratch,tile=256) as files:
            assert not files.protected_nonzero
            for path,png,want in zip((files.target,files.protected),(files.target_png,files.protected_png),expected):
                files.arrays.validate_file(path);files.pngs.validate_file(png)
                assert np.array_equal(np.load(path,allow_pickle=False),want)
                with Image.open(png) as image:assert np.array_equal(np.asarray(image),np.rint(want.astype(np.float64)*65535).astype(np.uint16))
        assert not list(scratch.directory.iterdir())


def test_explicit_protection_is_nonzero(tmp_path):
    doc=document([Stroke(kind='stroke',sequence=0,mode='protect',
                         points=[point(.5,.5)],radius=.2,hardness=1.)],w=100,h=100)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        with prepare_raster(doc,'a'*64,scratch,tile=256) as files:
            assert files.protected_nonzero
            assert np.any(np.load(files.protected,allow_pickle=False)>0)


def test_quota_and_cancellation_remove_partial_outputs(tmp_path):
    doc=document([])
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,100,min_free_bytes=0) as scratch:
        with pytest.raises(DiskQuota):
            with prepare_raster(doc,'a'*64,scratch):pass
        assert not list(scratch.directory.iterdir())
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==10:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_raster(doc,'a'*64,scratch,check_active=active):pass
        assert not list(scratch.directory.iterdir())


def test_consumer_failure_cleanup(tmp_path):
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='consumer'):
            with prepare_raster(document([]),'a'*64,scratch):raise RuntimeError('consumer')
        assert not list(scratch.directory.iterdir())
