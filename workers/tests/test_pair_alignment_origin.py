"""Origin binding without changing registration's numerical algorithms."""
from uuid import uuid4
import pytest
from photo_worker.contracts.pair import RegistrationParams, PairAlignment, validate_alignment_origin


@pytest.mark.parametrize('manual', [False, True])
@pytest.mark.parametrize('mode', ['valid', 'before', 'after', 'crop', 'confirmation', 'matrix'])
def test_alignment_original_params_binding(manual, mode):
    before = dict(artifact_id=uuid4(), sha256='a' * 64)
    after = dict(artifact_id=uuid4(), sha256='b' * 64)
    identity = [1., 0., 0., 0., 1., 0., 0., 0., 1.]
    params = RegistrationParams(before=before, after=after,
                                manual_transform=identity if manual else None,
                                manual_confirmed=manual)
    result = dict(before=before, after=after,
                  before_canvas=dict(width=32, height=32, orientation=1, space_id="working", source_sha256="a" * 64), after_canvas=dict(width=32, height=32, orientation=1, space_id="working", source_sha256="b" * 64),
                  before_to_after=identity, overlap=1, median_reprojection=None if manual else 0,
                  inlier_count=0 if manual else 12,
                  confidence='user_confirmed' if manual else 'measured', crop_aware=False, warnings=[])
    if mode in ('before', 'after'):
        result[mode] = dict(result[mode], artifact_id=uuid4())
    if mode == 'crop': result['crop_aware'] = True
    if mode == 'confirmation':
        result.update(confidence='measured' if manual else 'user_confirmed',
                      median_reprojection=0 if manual else None, inlier_count=12 if manual else 0)
    if mode == 'matrix': result['before_to_after'] = [1., 0., 1., 0., 1., 0., 0., 0., 1.]
    alignment = PairAlignment(**result)
    if mode == 'valid' or mode == 'matrix' and not manual:
        assert validate_alignment_origin(params, alignment) is alignment
    else:
        with pytest.raises(ValueError): validate_alignment_origin(params, alignment)
