from types import SimpleNamespace
import pytest
from photo_worker.v2 import portrait_donor_adopter_config as module


def test_disabled_adopter_does_not_read_configuration_files():
    assert module.donor_adopter_from_environment(lambda key,default=None:default) is None


@pytest.mark.parametrize('enabled',['TRUE','yes','1'])
def test_invalid_enablement_rejected(enabled):
    with pytest.raises(ValueError):module.donor_adopter_from_environment(lambda key,default=None:enabled)


def test_aggregate_adopter_scratch_budget_refused_before_policy_io(tmp_path,monkeypatch):
    monkeypatch.setattr(module,'donor_options_from_environment',lambda *args:SimpleNamespace(mask_max_bytes=50,material_max_bytes=60))
    values={'PHOTO_ADOPTER_DONOR_ENABLED':'true','PHOTO_ADOPTER_DONOR_RAM_MAX_BYTES':'1073741824','PHOTO_ADOPTER_DONOR_TOTAL_SCRATCH_BYTES':'100',
        'PHOTO_ADOPTER_DONOR_OUTPUT_MAX_BYTES':'20','PHOTO_ADOPTER_DONOR_OUTPUT_SCRATCH_ROOT':str(tmp_path)}
    with pytest.raises(ValueError,match='aggregate'):
        module.donor_adopter_from_environment(lambda key,default=None:values.get(key,default))


@pytest.mark.parametrize('mode',['ok','noncanonical','sha','symlink'])
def test_pinned_regular_policy_loading(tmp_path,monkeypatch,mode):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    options=SimpleNamespace(mask_max_bytes=10,material_max_bytes=20,runtime_sha='b'*64)
    monkeypatch.setattr(module,'donor_options_from_environment',lambda *args:options)
    policy=dict(operation='portrait.open_eyes',operation_version='1',backend='cpu',runtime_lock_sha='b'*64)
    raw=canonical_bytes(policy)+(b' ' if mode=='noncanonical' else b'')
    path=tmp_path/'policy.json';path.write_bytes(raw)
    if mode=='symlink':
        link=tmp_path/'link.json';link.symlink_to(path);path=link
    values={'PHOTO_ADOPTER_DONOR_ENABLED':'true','PHOTO_ADOPTER_DONOR_RAM_MAX_BYTES':'1073741824','PHOTO_ADOPTER_DONOR_TOTAL_SCRATCH_BYTES':'100',
        'PHOTO_ADOPTER_DONOR_OUTPUT_MAX_BYTES':'20','PHOTO_ADOPTER_DONOR_OUTPUT_SCRATCH_ROOT':str(tmp_path),
        'PHOTO_ADOPTER_DONOR_POLICY':str(path),'PHOTO_ADOPTER_DONOR_POLICY_SHA':'a'*64 if mode=='sha' else sha256(raw).hexdigest()}
    def run():return module.donor_adopter_from_environment(lambda key,default=None:values.get(key,default))
    if mode=='ok':assert run().policy==policy
    else:
        with pytest.raises(ValueError,match='policy file'):run()
