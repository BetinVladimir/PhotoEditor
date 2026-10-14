from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2 import negative_adoption_prepare as module
from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions
from test_negative_qualification import fixture
from test_negative_result import source_case


@pytest.mark.parametrize('changed',[False,True])
def test_negative_preparation_binds_sql_snapshot_before_replay(monkeypatch,tmp_path,changed):
    p,c,r,pin,policy=fixture();params=OpticalNegativeParams.model_validate_json(canonical_bytes(p))
    _,source,_=source_case();ref=ArtifactRef(uuid4(),'a'*64);root=ArtifactRef(uuid4(),'b'*64)
    evidence=dict(params_ref=dict(artifact_id=str(ref.artifact_id),sha256='0'*64 if changed else ref.sha256),
        profile=dict(negative_qualification=pin))
    monkeypatch.setattr(module,'_document',lambda reader,ref,model,origin:params if model is OpticalNegativeParams else source)
    monkeypatch.setattr(module,'load_negative_adoption_evidence',lambda *args,**kwargs:evidence)
    checks=[]
    monkeypatch.setattr(module,'negative_adoption_checkpoint',lambda *args,**kwargs:lambda:checks.append('current') or evidence)
    def replay(*args,**kwargs):
        assert kwargs['current_pin']().runtime_lock_sha==pin['runtime_lock_sha']
        assert kwargs['source_authority'](p,canonical_bytes(source)) is True
        checks.append('replay')
    monkeypatch.setattr(module,'verify_qualified_negative_replay',replay)
    def run():return module.prepare_negative_reconstruction(None,None,uuid4(),uuid4(),SimpleNamespace(root=root),ref,
        policy=policy,options=NegativeRuntimeOptions(object(),'b'*64,100,1<<30),scratch_root=tmp_path,
        max_bytes=1<<30,verify_color=lambda image:True,check_active=lambda:None)
    if changed:
        with pytest.raises(ValueError,match='PARAMS_CHANGED'):run()
        assert not checks
    else:
        prepared=run();assert prepared.params_raw==canonical_bytes(p) and prepared.result_ref==root
        assert checks==['current','current','current','replay','current']
