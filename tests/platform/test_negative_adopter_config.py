from pathlib import Path
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import negative_adopter_config as module
from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions


@pytest.mark.parametrize('mode',['ok','policy','runtime','symlink','disabled'])
def test_negative_adopter_requires_original_policy_runtime_and_icc(monkeypatch,tmp_path,mode):
    policy=dict(operation='optical.negative',operation_version='1',backend='cpu',runtime_lock_sha='a'*64)
    raw=canonical_bytes(policy);path=tmp_path/'policy.json';path.write_bytes(raw)
    if mode=='symlink':
        link=tmp_path/'link.json';link.symlink_to(path);path=link
    options=NegativeRuntimeOptions(object(),'b'*64 if mode=='runtime' else 'a'*64,100,1000)
    monkeypatch.setattr(module,'negative_options_from_environment',lambda *args:options)
    values=dict(PHOTO_ADOPTER_NEGATIVE_ENABLED='false' if mode=='disabled' else 'true',
        PHOTO_ADOPTER_NEGATIVE_TOTAL_SCRATCH_BYTES='200',PHOTO_ADOPTER_NEGATIVE_SCRATCH_ROOT=str(tmp_path),
        PHOTO_ADOPTER_NEGATIVE_POLICY=str(path),PHOTO_ADOPTER_NEGATIVE_POLICY_SHA='0'*64 if mode=='policy' else sha256(raw).hexdigest(),
        PHOTO_ICC_ROOT=str(Path(__file__).resolve().parents[2]/'resources/icc'))
    if mode=='disabled':assert module.negative_adopter_from_environment(values.get) is None
    elif mode=='ok':
        item=module.negative_adopter_from_environment(values.get)
        assert item.policy==policy and item.options is options and callable(item.verify_color)
    else:
        with pytest.raises(ValueError):module.negative_adopter_from_environment(values.get)
