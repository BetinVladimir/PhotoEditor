from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.optical.perspective_color import perspective_icc_verifier


@pytest.mark.parametrize('mismatch',[None,'sha','bytes','id'])
def test_color_identity_matches_operator_registry(mismatch):
    raw=b'unit-only-profile';digest=sha256(raw).hexdigest()
    color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256=digest)
    registry=object.__new__(ICCRegistry)
    registry.resolve=lambda _:SimpleNamespace(id='other' if mismatch=='id' else color.profile_id,
        sha256='0'*64 if mismatch=='sha' else digest,data=b'changed' if mismatch=='bytes' else raw)
    verify=perspective_icc_verifier(registry)
    if mismatch is None:assert verify(color) is True
    else:
        with pytest.raises(ValueError,match='ICC_MISMATCH'):verify(color)
