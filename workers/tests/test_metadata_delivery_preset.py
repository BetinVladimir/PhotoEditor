import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export_preset import validate_frozen_export_preset


@pytest.mark.parametrize('mode',['strip','copyright','approved_exif'])
def test_frozen_delivery_preset_accepts_closed_metadata_modes(mode):
    raw=canonical_bytes(dict(format='jpeg',icc_id='srgb_v1',quality=92,subsampling='444',
        alpha='matte',matte_rgb=[1,1,1],metadata_policy=mode,naming_template='{name}'))
    validate_frozen_export_preset(raw)


@pytest.mark.parametrize('mode',['safe_camera','copyright_only','preserve_all'])
def test_frozen_delivery_preset_refuses_unknown_metadata_modes(mode):
    raw=canonical_bytes(dict(format='jpeg',icc_id='srgb_v1',quality=92,subsampling='444',
        alpha='matte',matte_rgb=[1,1,1],metadata_policy=mode,naming_template='{name}'))
    with pytest.raises(ValueError):validate_frozen_export_preset(raw)
