from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.perspective_replacement_detection_job import decode_job


def packet():
    tenant,run,task=map(str,(uuid4(),uuid4(),uuid4()))
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    descriptor={'tenant_id':tenant,'run_id':run,'task_id':task,'kind':'v29_perspective_replacement_detection_v1'}
    return {'schema_version':1,'socket':'/tmp/job.sock','token':'b'*64,'tenant_id':tenant,'run_id':run,'task_id':task,
        'descriptor_jcs':canonical_bytes(descriptor).decode(),'attempt':1,'fence':2,'result_id':str(uuid4()),
        'root':ref,'params':ref,'timeout_seconds':60}


def test_job_packet_binds_descriptor_scope_and_is_canonical():
    value=packet();raw=canonical_bytes(value)
    assert decode_job(raw)==value
    with pytest.raises(ValueError):decode_job(raw+b' ')
    value['task_id']=str(uuid4())
    with pytest.raises(ValueError):decode_job(canonical_bytes(value))


@pytest.mark.parametrize('key,value',[('attempt',True),('fence',0),('timeout_seconds',601),('schema_version',True)])
def test_job_packet_rejects_invalid_execution_bounds(key,value):
    job=packet();job[key]=value
    with pytest.raises(ValueError):decode_job(canonical_bytes(job))
