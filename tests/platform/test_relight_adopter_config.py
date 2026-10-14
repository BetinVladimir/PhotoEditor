import pytest
from photo_worker.v2 import relight_adopter_config as module


def test_disabled_does_not_load_runtime(monkeypatch):
    monkeypatch.setattr(module,'load_relight_runtime_lock',lambda *a:pytest.fail('disabled IO'))
    assert module.relight_adopter_from_environment({}.get) is None


@pytest.mark.parametrize('field,value', [('PHOTO_ADOPTER_RELIGHT_ENABLED','yes'),
    ('PHOTO_ADOPTER_RELIGHT_INPUT_BYTES','0'),('PHOTO_ADOPTER_RELIGHT_TOTAL_SCRATCH_BYTES','1'),
    ('PHOTO_ADOPTER_RELIGHT_RUNTIME_SHA','bad'),('PHOTO_ADOPTER_RELIGHT_RUNTIME_LOCK','relative')])
def test_invalid_configuration_refuses_before_runtime_io(tmp_path,monkeypatch,field,value):
    env=dict(PHOTO_ADOPTER_RELIGHT_ENABLED='true',PHOTO_ADOPTER_RELIGHT_SCRATCH_ROOT=str(tmp_path),
        PHOTO_ADOPTER_RELIGHT_INPUT_BYTES='1000',PHOTO_ADOPTER_RELIGHT_TOTAL_SCRATCH_BYTES='2000',
        PHOTO_ADOPTER_RELIGHT_RUNTIME_LOCK=str(tmp_path/'runtime.json'),PHOTO_ADOPTER_RELIGHT_RUNTIME_SHA='a'*64)
    env[field]=value
    monkeypatch.setattr(module,'load_relight_runtime_lock',lambda *a:pytest.fail('invalid runtime IO'))
    with pytest.raises(ValueError):module.relight_adopter_from_environment(env.get)


def test_explicit_runtime_pin_and_budgets(tmp_path,monkeypatch):
    env=dict(PHOTO_ADOPTER_RELIGHT_ENABLED='true',PHOTO_ADOPTER_RELIGHT_SCRATCH_ROOT=str(tmp_path),
        PHOTO_ADOPTER_RELIGHT_INPUT_BYTES='1000',PHOTO_ADOPTER_RELIGHT_TOTAL_SCRATCH_BYTES='2000',
        PHOTO_ADOPTER_RELIGHT_RUNTIME_LOCK=str(tmp_path/'runtime.json'),PHOTO_ADOPTER_RELIGHT_RUNTIME_SHA='a'*64)
    calls=[]
    monkeypatch.setattr(module,'load_relight_runtime_lock',lambda *a:calls.append(a) or {'pin':'fixture'})
    installation=module.relight_adopter_from_environment(env.get)
    assert installation.max_bytes==1000 and installation.max_scratch_bytes==2000
    assert calls==[(env['PHOTO_ADOPTER_RELIGHT_RUNTIME_LOCK'],'a'*64)]
