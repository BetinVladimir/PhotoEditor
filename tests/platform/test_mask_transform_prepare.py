from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.runtime.arrays import read_inverse_field,read_array
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.mask.transform_prepare import prepare_transform
from photo_worker.contracts.images import ResizeTransform
from test_mask_transform import common,collect
from test_mask_refine import collect as refine


def test_probability_transform_refinement_and_png(tmp_path):
    source=np.random.default_rng(2).random((9,17),dtype=np.float32)
    step=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(17,9,34,18))
    guide=np.full((18,34,3),.4,np.float32)
    expected=refine(collect(source,step),guide)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_transform(source,step,scratch,guide=guide) as files:
            assert files.radius==1
            assert np.array_equal(np.load(files.array,allow_pickle=False),expected)
            with Image.open(files.png) as image:assert np.array_equal(np.asarray(image),np.rint(expected.astype(np.float64)*65535).astype(np.uint16))
        assert not list(scratch.directory.iterdir())


def test_label_output_and_failure_cleanup(tmp_path):
    source=np.arange(12,dtype=np.uint16).reshape(3,4)
    step=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(4,3,8,6))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_transform(source,step,scratch,labels=True) as files:
            assert files.radius is None
            assert set(np.unique(np.load(files.array,allow_pickle=False)))==set(range(12))
        with pytest.raises(RuntimeError):
            with prepare_transform(source,step,scratch,labels=True):raise RuntimeError('consumer')
        assert not list(scratch.directory.iterdir())


def test_dense_reader_is_separate_and_strict(tmp_path):
    path=tmp_path/'field.npy';field=np.zeros((7,9,2),np.float32);field[...,0]=-5;field[...,1]=100
    np.save(path,field)
    with read_inverse_field(path,(7,9),lambda:None) as mapped:
        assert np.array_equal(mapped,field) and not mapped.flags.writeable
    with pytest.raises(ValueError):
        with read_array(path,(7,9,2),'float32',lambda:None):pass
    field[0,0,0]=np.inf;np.save(path,field)
    with pytest.raises(ValueError,match='NONFINITE'):
        with read_inverse_field(path,(7,9),lambda:None):pass
