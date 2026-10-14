from pathlib import Path
from types import SimpleNamespace
from hashlib import sha256
import pytest
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.optical.negative_color import negative_icc_verifier,LINEAR_PROFILE_SHA
from photo_worker.v2.negative_source import negative_source_authority
from test_negative_result import source_case


def test_negative_actual_icc_bytes_and_source_identity():
    root=Path(__file__).resolve().parents[2]/'resources/icc'
    verify=negative_icc_verifier(ICCRegistry(root));_,image,_=source_case()
    assert verify(image) is False
    image.color.icc_sha256=LINEAR_PROFILE_SHA
    assert verify(image) is True


@pytest.mark.parametrize('value',[False,None,1])
def test_negative_color_requires_literal_true(value):
    p,_,source=source_case();body=canonical_bytes(p)
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(body).hexdigest())))
    authority=negative_source_authority(SimpleNamespace(negative_source_ready=lambda *args:True),lease,body,
        verify_color=lambda image:value,check_active=lambda:None)
    with pytest.raises(ValueError,match='NEGATIVE_ICC_MISMATCH'):authority(p,source)
