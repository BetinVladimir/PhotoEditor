from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.optical.negative_stage import StagedNegativeResult
from photo_worker.v2 import negative_execute as module
from test_negative_qualification import fixture
from test_negative_result import source_case


def test_declared_negative_execution_joins_owned_inputs_and_review_handoff(monkeypatch):
    p,c,r,pin,policy=fixture();base,_,source=source_case();p['base']=base['base'];raw=canonical_bytes(p)
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',resource_class='cpu',
        params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())),execution_profile=dict(
        negative_runtime_sha=pin['runtime_lock_sha'],negative_qualification=deepcopy(pin),resource_estimate={'output_bytes':100}))
    calls=[];declared=[lease.command.params,ArtifactRef.model_validate_json(canonical_bytes(p['base']['source']))]
    class Artifacts:
        scratch=object()
        def contract_bytes(self,ref,model):calls.append('document');return raw if ref==lease.command.params else source
        def declared_input_refs(self,maximum):assert maximum==512;return declared
        def require_declared_input(self,ref):calls.append(ref)
    def evidence(*args,**kwargs):return r,c,policy,lambda:deepcopy(pin)
    monkeypatch.setattr(module,'load_negative_qualification',evidence)
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(6))
    def stage(*args,**kwargs):
        assert args[4]==raw and args[5]==source and args[6]==c and args[7]==r
        assert kwargs['qualification_authority']().runtime_lock_sha==pin['runtime_lock_sha']
        return StagedNegativeResult(outputs[-1],outputs,True,100)
    monkeypatch.setattr(module,'prepare_and_stage_leased_negative',stage)
    database=SimpleNamespace(negative_lease_ready=lambda *args:True,negative_source_ready=lambda *args:True)
    result=module.execute_declared_sql_negative(SimpleNamespace(lease=lease,check_active=lambda:None),Artifacts(),database,
        reader=object(),qualification_objects=object(),bucket='bucket',runtime_lock=object(),runtime_sha=pin['runtime_lock_sha'],
        source_max_bytes=200000,ram_max_bytes=1<<30,verify_color=lambda image:True)
    assert result.outputs==[outputs[-1]] and result.review_required and result.bytes_written==100
    assert ArtifactRef.model_validate_json(canonical_bytes(pin['report'])) in calls and ArtifactRef.model_validate_json(canonical_bytes(pin['calibration'])) in calls
