from copy import deepcopy
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
import pytest
from test_perspective_adapter import source,params
from test_relight_qualification import evidence
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.operations.optical.perspective_evidence import load_perspective_evidence


def fixture():
    image=source();intent=params(image,1);body=canonical_bytes(intent)
    _,pin,report,calibration,policy=evidence()
    policy['operation']='optical.perspective';report['operation']='optical.perspective'
    pin['policy_sha']=canonical_hash(policy);pin['report']['sha256']=canonical_hash(report)
    bodies={'report':canonical_bytes(report),'calibration':canonical_bytes(calibration)}
    record={'pin':pin,'policy':policy}
    for label in bodies:
        record[label]={**pin[label], 'bucket':'photos','object_key':label,'object_version':'version-1',
            'bytes':len(bodies[label]),'media_type':'application/json'}
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.perspective',operation_version='1',resource_class='cpu',params=SimpleNamespace(sha256=sha256(body).hexdigest())),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',perspective_runtime_sha=pin['runtime_lock_sha'],perspective_qualification=pin))
    return body,lease,record,bodies


class Objects:
    def __init__(self,bodies):self.bodies=bodies;self.streams=[];self.calls=[]
    def get_object(self,bucket,key,*,version_id):
        self.calls.append((bucket,key,version_id));stream=BytesIO(self.bodies[key]);self.streams.append(stream);return stream


def test_exact_objects_and_repeated_live_scope_reads():
    body,lease,record,bodies=fixture();calls=[]
    def read(*args):calls.append(True);return deepcopy(record)
    database=SimpleNamespace(perspective_qualification_evidence=read);objects=Objects(bodies)
    result=load_perspective_evidence(database,lease,body,objects,'photos',check_active=lambda:None)
    assert result.report_raw==bodies['report'] and result.calibration_raw==bodies['calibration']
    assert len(calls)>=6 and all(stream.closed for stream in objects.streams)
    assert objects.calls==[('photos','report','version-1'),('photos','calibration','version-1')]


@pytest.mark.parametrize('change',['size','hash','version','bucket','scope','params'])
def test_invalid_provenance_refused(change):
    body,lease,record,bodies=fixture();calls=[]
    if change=='size':bodies['report']=bodies['report'][:-1]
    if change=='hash':bodies['report']=b'x'*len(bodies['report'])
    if change=='version':record['report']['object_version']='null'
    if change=='bucket':record['report']['bucket']='foreign'
    if change=='params':lease.command.params.sha256='0'*64
    def read(*args):
        calls.append(True);value=deepcopy(record)
        if change=='scope' and len(calls)>2:value['report']['object_version']='version-2'
        return value
    objects=Objects(bodies);database=SimpleNamespace(perspective_qualification_evidence=read)
    with pytest.raises(ValueError):load_perspective_evidence(database,lease,body,objects,'photos',check_active=lambda:None)
    assert all(stream.closed for stream in objects.streams)
    if change in ('version','bucket','params'):assert not objects.calls
