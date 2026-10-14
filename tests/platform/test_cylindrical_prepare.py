from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.cylindrical_prepare import prepare_cylindrical_frame, cylindrical_resources
from photo_worker.runtime.scratch import ScratchSpace


def test_cylindrical_files_retain_field_and_zero_padding(tmp_path):
    rgb = np.full((51, 101, 3), 4., np.float32); alpha = np.ones((51, 101), np.float32)
    required = cylindrical_resources(alpha.shape, tile=7)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, required['scratch_bytes'], min_free_bytes=0) as scratch:
        with prepare_cylindrical_frame(rgb, alpha, scratch, ram_max_bytes=required['ram_bytes'], tile=7) as files:
            output = np.load(files.rgb); opacity = np.load(files.alpha); coverage = np.load(files.coverage)
            field = np.load(files.inverse_field)
            np.testing.assert_array_equal(field[25, 50], [50.5, 25.5])
            np.testing.assert_array_equal(output[25, 50], [4, 4, 4])
            assert not output[~coverage].any() and not opacity[~coverage].any()
            files.reservation.validate(); paths = [files.rgb, files.alpha, files.coverage, files.inverse_field]
        assert all(not path.exists() for path in paths)
    assert np.all(rgb == 4) and np.all(alpha == 1)


def test_projection_ram_precedes_file_allocation():
    class NoScratch:
        def reserve(self, _size): pytest.fail('unexpected allocation')
    with pytest.raises(ValueError, match='CYLINDRICAL_RAM_EXCEEDED'):
        with prepare_cylindrical_frame(np.ones((8, 8, 3), np.float32), np.ones((8, 8), np.float32),
                NoScratch(), ram_max_bytes=1):
            pytest.fail('unexpected result')


def test_retained_projection_field_is_replayed_before_staging(tmp_path):
    from photo_worker.operations.optical.cylindrical_validation import verify_cylindrical_files
    rgb=np.ones((51,101,3),np.float32);alpha=np.ones((51,101),np.float32)
    required=cylindrical_resources(alpha.shape)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,required['scratch_bytes'],min_free_bytes=0) as scratch:
        with prepare_cylindrical_frame(rgb,alpha,scratch,ram_max_bytes=required['ram_bytes']) as files:
            verify_cylindrical_files(files,alpha.shape,tile=7)
            field=np.load(files.inverse_field,mmap_mode='r+',allow_pickle=False)
            try:field[25,50,0]+=1;field.flush()
            finally:field._mmap.close()
            with pytest.raises(ValueError,match='CYLINDRICAL_FIELD_INVALID'):
                verify_cylindrical_files(files,alpha.shape,tile=13)
