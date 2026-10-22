from pathlib import Path
import pytest
from photo_worker.v2.service import Adopter,AdopterConfig,_validate_retouch_pixel_scratch


@pytest.mark.parametrize('enabled,root,budget',[
    (True,None,1024),(True,'',1024),(True,object(),1024),
    (True,Path('/unused'),None),(True,Path('/unused'),True),
    (True,Path('/unused'),0),(True,Path('/unused'),2**53),
    ('true',Path('/unused'),1024),
])
def test_retouch_adopter_rejects_missing_physical_budget_before_installation(enabled,root,budget):
    with pytest.raises(ValueError,match='retouch adopter'):
        Adopter(None,None,None,'photos',shown_retouch_enabled=enabled,
            layer_scratch_root=root,layer_scratch_bytes=budget)


def test_retouch_scratch_policy_keeps_disabled_lane_optional(tmp_path):
    assert _validate_retouch_pixel_scratch(False,None,None) is None
    assert _validate_retouch_pixel_scratch(True,tmp_path,1024) is None
    assert _validate_retouch_pixel_scratch(True,str(tmp_path),1024) is None


def retouch_environment():
    return {'DATABASE_URL':'postgres://unused','S3_ENDPOINT':'https://objects.example.test',
        'S3_BUCKET':'photos','S3_ACCESS_KEY':'test-access','S3_SECRET_KEY':'test-secret'}


def test_retouch_environment_enabled_requires_scratch_during_parse():
    env={**retouch_environment(),'PHOTO_ADOPTER_SHOWN_RETOUCH_ENABLED':'true'}
    with pytest.raises(ValueError,match='retouch adopter scratch'):
        AdopterConfig.from_env(env)


def test_retouch_environment_enabled_keeps_exact_bounded_scratch(tmp_path):
    env={**retouch_environment(),'PHOTO_ADOPTER_SHOWN_RETOUCH_ENABLED':'true',
        'PHOTO_ADOPTER_LAYER_SCRATCH_ROOT':str(tmp_path.resolve()),'PHOTO_ADOPTER_LAYER_SCRATCH_BYTES':'1024'}
    config=AdopterConfig.from_env(env)
    assert config.shown_retouch_enabled is True
    assert config.layer_scratch_root==tmp_path.resolve() and config.layer_scratch_bytes==1024


def test_retouch_environment_disabled_does_not_require_scratch():
    config=AdopterConfig.from_env(retouch_environment())
    assert config.shown_retouch_enabled is False
    assert config.layer_scratch_root is None and config.layer_scratch_bytes is None


@pytest.mark.parametrize('value',['','TRUE','1','yes'])
def test_retouch_environment_refuses_ambiguous_enablement(value):
    with pytest.raises(ValueError,match='retouch adopter enablement'):
        AdopterConfig.from_env({**retouch_environment(),'PHOTO_ADOPTER_SHOWN_RETOUCH_ENABLED':value})
