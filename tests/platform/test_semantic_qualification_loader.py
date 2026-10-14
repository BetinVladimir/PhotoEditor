from io import BytesIO
from dataclasses import asdict
import json
from copy import deepcopy
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_qualification_loader import semantic_qualification_loader
from test_semantic_qualification_report import fixture


def loader_fixture():
    policy,_,raw,pin=fixture()
    record=dict(pin=json.loads(json.dumps(asdict(pin),default=str)),policy=policy,report=dict(artifact_id=str(pin.report.artifact_id),sha256=pin.report.sha256,bucket='owned',object_key='report',object_version='pinned-v1',bytes=len(raw),media_type='application/json'))
    class Database:
        calls=0
        after=None
        def semantic_report_evidence(self,lease):
            self.calls+=1
            return self.after if self.calls>1 and self.after is not None else record
    class Objects:
        calls=[]
        body=raw
        def get_object(self,bucket,key,*,version_id):
            self.calls.append((bucket,key,version_id))
            self.response=BytesIO(self.body)
            return self.response
    db,objects=Database(),Objects()
    load=semantic_qualification_loader(db,object(),objects,'owned',frozen_pin=pin)
    return db,objects,record,load


def test_exact_version_proof_and_recheck_after_io():
    db,objects,record,load=loader_fixture()
    assert load()==record['pin']
    assert objects.calls==[('owned','report','pinned-v1')]
    assert objects.response.closed and db.calls==2


@pytest.mark.parametrize('kind',['bucket','version','size','sha','missing','extra','pin','policy'])
def test_bad_evidence_denied(kind):
    db,objects,record,load=loader_fixture()
    if kind=='bucket':record['report']['bucket']='foreign'
    elif kind=='version':record['report']['object_version']='null'
    elif kind=='size':record['report']['bytes']=True
    elif kind=='sha':record['report']['sha256']='0'*64
    elif kind=='missing':del record['report']['object_key']
    elif kind=='extra':record['report']['extra']=1
    elif kind=='pin':record['pin']['policy_sha']='0'*64
    else:record['policy']['required_checks']=['changed']
    with pytest.raises(AdoptionInvalid):load()
    if kind!='policy':assert not objects.calls


@pytest.mark.parametrize('body',['short','long','changed'])
def test_changed_object_bytes_denied_and_closed(body):
    _,objects,_,load=loader_fixture()
    if body=='short':objects.body=objects.body[:-1]
    elif body=='long':objects.body+=b' '
    else:objects.body=b'x'*len(objects.body)
    with pytest.raises(AdoptionInvalid):load()
    assert objects.response.closed


def test_revocation_during_io_denied():
    db,objects,record,load=loader_fixture()
    db.after=deepcopy(record);db.after['policy']['required_checks']=['changed']
    with pytest.raises(AdoptionInvalid,match='SCOPE_STALE'):load()
    assert db.calls==2 and objects.response.closed


def test_report_loader_owns_lease_for_both_sql_reads():
    from types import SimpleNamespace
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    db,objects,record,_=loader_fixture()
    lease=SimpleNamespace(command=SimpleNamespace(task_id='original'),fence=7)
    seen=[]
    def evidence(owner):
        seen.append((owner.command.task_id,owner.fence))
        owner.command.task_id='mutated-by-database';owner.fence=99
        return record
    db.semantic_report_evidence=evidence
    load=semantic_qualification_loader(db,lease,objects,'owned',
        frozen_pin=SemanticQualificationPin.from_value(record['pin']))
    lease.command.task_id='mutated-by-caller';lease.fence=8
    assert load()==record['pin']
    assert seen==[('original',7),('original',7)]
    assert lease.command.task_id=='mutated-by-caller' and lease.fence==8
