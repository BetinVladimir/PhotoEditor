from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.registration import PairAlignment
from photo_worker.operations.pair.prepare import warped_pair
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota


def alignment():return PairAlignment(np.eye(3),1.,None,0,'user_confirmed',False,())

def test_warped_pair_has_owned_readonly_maps_and_releases_quota(tmp_path):
    source=np.random.default_rng(42).random((48,72,3),dtype=np.float32)
    budget=48*72*13+8192
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,budget,min_free_bytes=0) as scratch:
        with warped_pair(source,alignment(),(48,72),scratch) as pair:
            rgb_map,valid_map=pair.before._mmap,pair.valid._mmap
            assert isinstance(pair.before,np.memmap) and isinstance(pair.valid,np.memmap)
            assert not pair.before.flags.writeable and not pair.valid.flags.writeable
            assert np.max(np.abs(pair.before-source))<2/65535
            assert pair.valid[1:-1,1:-1].all()
            assert len(list(scratch.directory.iterdir()))==2
            assert all(path.stat().st_mode&0o777==0o600 for path in scratch.directory.iterdir())
            with pytest.raises(DiskQuota):scratch.reserve(1)
        assert rgb_map.closed and valid_map.closed and list(scratch.directory.iterdir())==[]
        scratch.reserve(budget).release()


def test_cancel_and_insufficient_quota_leave_no_buffers(tmp_path):
    source=np.ones((20,20,3),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        calls=0
        def active():
            nonlocal calls
            calls+=1
            if calls==5:raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with warped_pair(source,alignment(),(20,20),scratch,check_active=active):pytest.fail('Unexpected result')
        assert list(scratch.directory.iterdir())==[]
        reserved=scratch.reserve(99999)
        with pytest.raises(DiskQuota):
            with warped_pair(source,alignment(),(20,20),scratch):pass
        assert list(scratch.directory.iterdir())==[]
        reserved.release()


def test_consumer_failure_closes_borrowed_maps(tmp_path):
    source=np.ones((20,20,3),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='consumer'):
            with warped_pair(source,alignment(),(20,20),scratch) as pair:
                mapping=pair.before._mmap
                raise RuntimeError('consumer')
        assert mapping.closed and list(scratch.directory.iterdir())==[]


def test_spatial_split_can_write_owned_disk_maps_without_changing_digest(tmp_path):
    from photo_worker.algorithms.sampling import split_blocks
    valid=np.ones((513,517),bool);valid[:50,:70]=False
    expected=split_blocks(valid,42)
    train=np.lib.format.open_memmap(tmp_path/'train.npy',mode='w+',dtype=np.bool_,shape=valid.shape)
    holdout=np.lib.format.open_memmap(tmp_path/'holdout.npy',mode='w+',dtype=np.bool_,shape=valid.shape)
    try:
        actual=split_blocks(valid,42,train_out=train,holdout_out=holdout)
        assert actual.train is train and actual.holdout is holdout
        assert actual.digest==expected.digest and actual.holdout_blocks==expected.holdout_blocks
        assert np.array_equal(actual.train,expected.train) and np.array_equal(actual.holdout,expected.holdout)
        import hashlib
        raw=np.packbits(actual.train,bitorder='little').tobytes()+np.packbits(actual.holdout,bitorder='little').tobytes()+np.asarray(valid.shape,dtype='<i8').tobytes()
        assert hashlib.sha256(raw).hexdigest()==actual.digest
    finally:train._mmap.close();holdout._mmap.close()


def test_split_rejects_aliased_outputs():
    from photo_worker.algorithms.sampling import split_blocks
    valid=np.ones((32,32),bool);other=np.empty_like(valid)
    for train,holdout in ((valid,other),(other,other),(other,None)):
        with pytest.raises(ValueError,match='OUTPUTS'):
            split_blocks(valid,42,train_out=train,holdout_out=holdout)


def test_nested_warp_and_split_reservations_cleanup_on_cancel(tmp_path):
    from photo_worker.operations.pair.prepare import spatial_split
    source=np.ones((32,48,3),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        with warped_pair(source,alignment(),(32,48),scratch) as pair:
            with spatial_split(pair.valid,42,scratch) as split:
                assert len(list(scratch.directory.iterdir()))==4
                assert not np.any(split.train&split.holdout)
                assert np.array_equal(split.train|split.holdout,pair.valid)
                mapping=split.train._mmap
            assert mapping.closed and len(list(scratch.directory.iterdir()))==2
            def cancelled():raise RuntimeError('cancelled')
            with pytest.raises(RuntimeError,match='cancelled'):
                with spatial_split(pair.valid,42,scratch,check_active=cancelled):pass
        assert list(scratch.directory.iterdir())==[]


def test_prepared_samples_outlive_scratch_and_keep_holdout_independent(tmp_path):
    from photo_worker.operations.pair.prepare import prepare_samples
    image=np.random.default_rng(4).uniform(.1,.9,(64,96,3)).astype(np.float32)
    person=np.full((64,96),.8,np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,200000,min_free_bytes=0) as scratch:
        one,evidence=prepare_samples('pair',image,image,alignment(),scratch,person=person,max_samples=200)
        two,repeat=prepare_samples('pair',image,image,alignment(),scratch,person=person,max_samples=200)
        assert evidence==repeat and evidence.train_seed!=evidence.holdout_seed
        assert list(scratch.directory.iterdir())==[]
    assert one.train_before.shape==(200,3) and one.holdout_before.shape==(200,3)
    assert np.array_equal(one.train_before,two.train_before)
    assert np.max(np.abs(one.train_before-one.train_after))<2/65535
    assert np.all(one.train_person==np.float32(.8))
    train={tuple(row) for row in one.train_after}
    held={tuple(row) for row in one.holdout_after}
    assert not train.intersection(held)
    one.validate()
