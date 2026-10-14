from types import SimpleNamespace
from uuid import uuid4
import pytest
from test_semantic_qualification_loader import loader_fixture
from test_semantic_transfer_contracts import transfer_documents
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams
from photo_worker.operations.semantic.assembly_installation import sql_semantic_assembly_factory
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('variant',['valid','calibration','lease','owner','params'])
def test_factory_verifies_real_report_and_binds_owners(variant):
    reports,objects,record,_=loader_fixture();pin=SemanticQualificationPin.from_value(record['pin'])
    transfer,_,_=transfer_documents()
    params=V2SemanticAssembleParams(schema_version=1,intent=transfer.intent,candidates=[ArtifactRef(artifact_id=uuid4(),sha256='a'*64)],model_lock_sha=pin.model_lock_sha,calibration=ArtifactRef(artifact_id=pin.calibration.artifact_id,sha256=pin.calibration.sha256))
    if variant=='calibration':params.calibration=ArtifactRef(artifact_id=uuid4(),sha256=pin.calibration.sha256)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))
    ctx=SimpleNamespace(lease=SimpleNamespace(fence=1,command=SimpleNamespace(params=ref)),check_active=lambda:None)
    def bytes_for(value,model):
        assert value==ref and model is V2SemanticAssembleParams
        return canonical_bytes(params)
    artifacts=SimpleNamespace(contract_bytes=bytes_for)
    db=SimpleNamespace(semantic_assembly_qualification=lambda lease:record['pin'],semantic_assembly_report_evidence=reports.semantic_report_evidence,semantic_assembly_snapshot=lambda lease:[],semantic_assembly_producer=lambda *args:None)
    install=sql_semantic_assembly_factory(database=db,objects=objects,bucket='owned')
    if variant=='calibration':
        with pytest.raises(AdoptionInvalid,match='BINDING_INVALID'):install(ctx,artifacts)
        assert objects.calls==[];return
    owned=install(ctx,artifacts)
    assert objects.calls==[('owned','report','pinned-v1')] and objects.response.closed
    if variant=='lease':
        ctx.lease.fence=2
        with pytest.raises(AdoptionInvalid,match='LEASE_CHANGED'):owned.load_snapshot(artifacts,None)
    if variant=='owner':
        with pytest.raises(AdoptionInvalid,match='OWNER_CHANGED'):owned.load_snapshot(object(),None)
    if variant=='params':
        changed=params.model_copy(deep=True);changed.model_lock_sha='f'*64
        with pytest.raises(AdoptionInvalid,match='PARAMS_CHANGED'):owned.verify_provenance(changed,None,None,[])
