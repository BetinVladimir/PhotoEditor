"""Exact skipped-body field proof; no GPU/provider substitution."""
import numpy as np
import pytest
from photo_worker.algorithms.body_warp import identity_inverse_field
from photo_worker.operations.region.geometry_replay import require_skipped_body_identity, require_person_mask_identity


def test_wide_identity_and_last_horizontal_tile_are_verified():
    field=identity_inverse_field(1025,2)
    calls=[]
    require_skipped_body_identity(field,check_active=lambda:calls.append(True))
    assert len(calls)==6  # five horizontal tiles and final lease check
    field[1,1024,0]-=1
    with pytest.raises(ValueError,match='REGION_BODY_SKIPPED_FIELD_NOT_IDENTITY'):
        require_skipped_body_identity(field)


def test_panorama_skip_proof_cancels_between_horizontal_tiles():
    field=identity_inverse_field(1025,2)
    calls=[]
    def active():
        calls.append(True)
        if len(calls)==2:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        require_skipped_body_identity(field,check_active=active)
    assert len(calls)==2


def test_person_replay_compares_the_last_panorama_tile_exactly():
    person=np.zeros((2,1025),dtype=np.float32)
    original=person.copy()
    require_person_mask_identity(person,original)
    original[1,1024]=np.nextafter(np.float32(0),np.float32(1))
    with pytest.raises(ValueError,match='REGION_PERSON_MODEL_REPLAY_MISMATCH'):
        require_person_mask_identity(person,original)


def test_person_replay_cancels_before_second_horizontal_tile():
    person=np.zeros((2,1025),dtype=np.float32)
    calls=[]
    def active():
        calls.append(True)
        if len(calls)==3:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        require_person_mask_identity(person,person,check_active=active)
    assert len(calls)==3


def test_person_replay_refuses_shape_mismatch():
    with pytest.raises(ValueError,match='REGION_PERSON_MODEL_REPLAY_MISMATCH'):
        require_person_mask_identity(np.zeros((2,3)),np.zeros((2,4)))
