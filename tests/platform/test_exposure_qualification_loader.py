from io import BytesIO
from dataclasses import asdict
from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import json
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2 import exposure_qualification_loader as module
from test_exposure_qualification_report import exposure_fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure',[None,'short','version','revoked'])
def test_cpu_loader_reads_exact_version_and_rechecks_scope(monkeypatch,failure):
    policy,_,report,pin=exposure_fixture();edit,_,_=source_fixture()
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit));raw=canonical_bytes(edit)
    params=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    monkeypatch.setattr(module,'load_exposure_adoption_params',lambda *a,**k:(params,edit))
    record=dict(pin=json.loads(json.dumps(asdict(pin),default=str)),policy=policy,
        report=dict(artifact_id=str(pin.report.artifact_id),sha256=pin.report.sha256,bucket='owned',
            object_key='report',object_version='null' if failure=='version' else 'v1',bytes=len(report),media_type='application/json'))
    calls=[]
    def execute(sql,args):
        calls.append(args)
        return SimpleNamespace(fetchone=lambda:(None if failure=='revoked' and len(calls)>=3 else record,))
    objects=SimpleNamespace(calls=[])
    def get(bucket,key,*,version_id):
        objects.calls.append((bucket,key,version_id));objects.response=BytesIO(report[:-1] if failure=='short' else report)
        return objects.response
    objects.get_object=get
    context=SimpleNamespace(operation='semantic.local_exposure',version='1',attempt=1,fence=2,result_id=uuid4())
    load=module.exposure_adoption_qualification_loader(SimpleNamespace(autocommit=True,execute=execute),
        uuid4(),uuid4(),context,SimpleNamespace(read_json=lambda *a:raw),objects,'owned',check_active=lambda:None)
    if failure:
        with pytest.raises(AdoptionInvalid):load()
    else:
        assert load()==record['pin'] and objects.calls==[('owned','report','v1')] and len(calls)==3
    if objects.calls:assert objects.response.closed


@pytest.mark.parametrize('failure', [None, 'params', 'revoked', 'bytes', 'pin'])
def test_worker_cpu_report_is_params_and_lease_bound(failure):
    from copy import deepcopy
    policy, _, report, pin = exposure_fixture()
    raw = b'{"schema_version":1}'
    lease = SimpleNamespace(command=SimpleNamespace(params=SimpleNamespace(
        sha256=sha256(raw).hexdigest())), fence=17)
    record = dict(pin=json.loads(json.dumps(asdict(pin), default=str)), policy=policy,
        report=dict(artifact_id=str(pin.report.artifact_id), sha256=pin.report.sha256,
            bucket='owned', object_key='report', object_version='v1',
            bytes=len(report), media_type='application/json'))
    calls = []
    def evidence(owner, params):
        calls.append((owner.fence, params))
        assert owner.fence == 17 and params == raw
        if failure == 'revoked' and len(calls) == 2:
            return None
        result = deepcopy(record)
        if failure == 'pin':
            result['pin']['policy_sha'] = '0' * 64
        return result
    objects = SimpleNamespace(calls=[])
    def get(bucket, key, *, version_id):
        objects.calls.append((bucket, key, version_id))
        objects.response = BytesIO(report[:-1] if failure == 'bytes' else report)
        return objects.response
    objects.get_object = get
    database = SimpleNamespace(exposure_report_evidence=evidence)
    if failure == 'params':
        with pytest.raises(AdoptionInvalid):
            module.exposure_worker_qualification_loader(database, lease, raw+b' ',
                objects, 'owned', frozen_pin=pin)
        assert not calls and not objects.calls
        return
    load = module.exposure_worker_qualification_loader(database, lease, raw,
        objects, 'owned', frozen_pin=pin)
    lease.fence = 999  # The loader owns its original lease snapshot.
    if failure:
        with pytest.raises(AdoptionInvalid):
            load()
    else:
        assert load() == record['pin']
        assert len(calls) == 2 and objects.calls == [('owned', 'report', 'v1')]
    if objects.calls:
        assert objects.response.closed
