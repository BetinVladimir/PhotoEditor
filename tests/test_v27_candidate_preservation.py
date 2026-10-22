import numpy as np
import pytest
from photo_worker.operations.semantic.candidate_preservation import verify_candidate_preservation


def verify(before, after, mask, protected, **kwargs):
    return verify_candidate_preservation(before, after, mask, protected,
        max_workspace_bytes=8*1048576, check_active=lambda: None, **kwargs)


def test_preserves_hdr_and_allows_photographer_review_of_changed_region():
    before = np.full((8, 8, 3), 2., dtype=np.float32)
    after = before.copy(); after[2, 2] = 3.
    mask = np.zeros((8, 8), np.float32); mask[2, 2] = 1
    proof = verify(before, after, mask, np.zeros_like(mask))
    assert proof.changed_pixels == 1 and proof.pixels == 64


@pytest.mark.parametrize('protected', [False, True])
def test_rejects_changes_outside_allowed_region(protected):
    before = np.zeros((8, 8, 3), np.float32); after = before.copy(); after[0, 0] = 1
    mask = np.ones((8, 8), np.float32) if protected else np.zeros((8, 8), np.float32)
    protection = np.ones_like(mask) if protected else np.zeros_like(mask)
    with pytest.raises(ValueError, match='PRESERVATION_FAILED'):
        verify(before, after, mask, protection)


def test_alpha_must_remain_bit_exact():
    before = np.ones((8, 8, 3), np.float32); mask = np.ones((8, 8), np.float32)
    alpha = np.ones_like(mask); changed = alpha.copy(); changed[0, 0] = .5
    with pytest.raises(ValueError, match='ALPHA_CHANGED'):
        verify(before, before, mask, np.zeros_like(mask), original_alpha=alpha, candidate_alpha=changed)


def test_admits_workspace_before_scanning_and_observes_cancellation():
    before = np.ones((8, 8, 3), np.float32); mask = np.ones((8, 8), np.float32)
    with pytest.raises(ValueError, match='WORKSPACE_EXCEEDED'):
        verify_candidate_preservation(before, before, mask, mask,
            max_workspace_bytes=1, check_active=lambda: None)
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        verify_candidate_preservation(before, before, mask, mask,
            max_workspace_bytes=8*1048576, check_active=cancelled)

def test_accepts_binary_inpaint_masks_without_full_float_copies():
    before = np.ones((8, 8, 3), np.float32); after = before.copy(); after[2, 2] = .5
    mask = np.zeros((8, 8), np.bool_); mask[2, 2] = True
    protected = np.zeros_like(mask)
    assert verify(before, after, mask, protected).changed_pixels == 1
    protected[2, 2] = True
    with pytest.raises(ValueError, match='PRESERVATION_FAILED'):
        verify(before, after, mask, protected)


@pytest.mark.parametrize('scope', ['outside', 'protected', 'alpha'])
def test_signed_zero_change_is_rejected_by_bitwise_preservation(scope):
    before = np.zeros((8, 8, 3), np.float32)
    after = before.copy()
    mask = np.ones((8, 8), np.float32)
    protected = np.zeros_like(mask)
    kwargs = {}
    if scope == 'alpha':
        alpha = np.zeros_like(mask)
        changed = alpha.copy()
        changed[0, 0] = np.float32(-0.)
        kwargs = dict(original_alpha=alpha, candidate_alpha=changed)
    else:
        after[0, 0, 0] = np.float32(-0.)
        if scope == 'outside': mask[0, 0] = 0
        else: protected[0, 0] = np.nextafter(np.float32(0), np.float32(1))
    with pytest.raises(ValueError, match='ALPHA_CHANGED' if scope == 'alpha' else 'PRESERVATION_FAILED'):
        verify(before, after, mask, protected, **kwargs)


@pytest.mark.parametrize('field', ['original','candidate','mask','protected','original_alpha','candidate_alpha'])
@pytest.mark.parametrize('value', [float('nan'),float('inf'),float('-inf'),-.1])
def test_preservation_rejects_invalid_numeric_values_in_every_owned_plane(field,value):
    values=dict(original=np.full((8,8,3),.5,np.float32),candidate=np.full((8,8,3),.5,np.float32),
        mask=np.ones((8,8),np.float32),protected=np.zeros((8,8),np.float32),
        original_alpha=np.ones((8,8),np.float32),candidate_alpha=np.ones((8,8),np.float32))
    values[field].flat[-1]=value
    with pytest.raises(ValueError,match='ALPHA_CHANGED' if 'alpha' in field else 'ARRAYS_INVALID'):
        verify_candidate_preservation(**values,max_workspace_bytes=8*1048576,check_active=lambda:None)


@pytest.mark.parametrize('field', ['mask','protected','original_alpha','candidate_alpha'])
def test_preservation_rejects_weights_or_alpha_above_one(field):
    values=dict(original=np.full((8,8,3),.5,np.float32),candidate=np.full((8,8,3),.5,np.float32),
        mask=np.ones((8,8),np.float32),protected=np.zeros((8,8),np.float32),
        original_alpha=np.ones((8,8),np.float32),candidate_alpha=np.ones((8,8),np.float32))
    values[field].flat[-1]=np.nextafter(np.float32(1),np.float32(2))
    with pytest.raises(ValueError,match='ALPHA_CHANGED' if 'alpha' in field else 'ARRAYS_INVALID'):
        verify_candidate_preservation(**values,max_workspace_bytes=8*1048576,check_active=lambda:None)
