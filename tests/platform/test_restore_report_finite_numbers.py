import pytest

from photo_worker.runtime.restore_report import _json


def test_private_restore_report_allows_finite_export_params_only():
    value={'matte_rgb':[0.0,0.5,1.0],'quality':90}
    assert _json(value)==value
    for invalid in (float('nan'),float('inf'),float('-inf')):
        with pytest.raises(ValueError,match='RESTORE_REPORT_VALUE'):
            _json({'matte_rgb':[invalid]})
