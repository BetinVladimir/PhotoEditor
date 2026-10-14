from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_adoption_qualification import semantic_adoption_qualification_loader
from test_semantic_qualification_loader import loader_fixture
from test_semantic_transfer_contracts import transfer_documents
from photo_worker.contracts.base import ArtifactRef


@pytest.mark.parametrize('variant',['valid','model','calibration','revoked'])
def test_completed_result_qualification_binds_params_and_exact_report(variant):
    _,objects,record,_=loader_fixture()
    params,_,_=transfer_documents()
    params.model_lock_sha=record['pin']['model_lock_sha']
    params.calibration=ArtifactRef.model_validate_json(__import__('json').dumps(record['pin']['calibration']))
    if variant=='model':params.model_lock_sha='0'*64
    if variant=='calibration':params.calibration.artifact_id=uuid4()
    scope=(uuid4(),uuid4(),2,3,uuid4());calls=[]
    def execute(query,args):
        assert args==scope and 'adoption_report_evidence' in query
        calls.append(args)
        return SimpleNamespace(fetchone=lambda:(None if variant=='revoked' and len(calls)>1 else record,))
    connection=SimpleNamespace(autocommit=True,execute=execute)
    def run():
        load=semantic_adoption_qualification_loader(connection,*scope[:2],SimpleNamespace(attempt=2,fence=3,result_id=scope[4]),params,objects,'owned')
        return load()
    if variant=='valid':
        assert run()==record['pin'] and len(calls)==3
        assert objects.calls==[('owned','report','pinned-v1')]
    else:
        with pytest.raises(AdoptionInvalid):run()
        assert objects.calls==[]
