from copy import deepcopy
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.portrait_donor_qualification_loader import load_donor_qualification
from test_portrait_donor_qualification import fixture
from test_portrait_open_eyes_prepare import arguments


@pytest.mark.parametrize('sealed',[False,True])
@pytest.mark.parametrize('mode',['ok','scope','version','bucket','corrupt','changed_after_read'])
def test_leased_donor_qualification_original_objects(mode,sealed):
    profile,report,calibration,policy=fixture();params,proof=arguments()[:2];body=canonical_bytes(params)
    if sealed:
        from uuid import uuid4
        from photo_worker.v2.contract_lock import SQL_TRANSPORT_SCHEMAS_SHA256
        profile.update(contracts_sha=SQL_TRANSPORT_SCHEMAS_SHA256,v2_input_manifest={'artifact_id':str(uuid4()),'sha256':'a'*64},v2_entry_count=20,v2_entries_sha='b'*64)
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256=sha256(body).hexdigest())),execution_profile=profile)
    record=dict(pin=deepcopy(profile['donor_qualification']),policy=policy)
    for label,raw in [('report',report),('calibration',calibration)]:
        record[label]=dict(artifact_id=record['pin'][label]['artifact_id'],sha256=record['pin'][label]['sha256'],bucket='test',object_key=label,object_version='v1',bytes=len(raw),media_type='application/json')
    if mode=='version':record['report']['object_version']='null'
    if mode=='bucket':record['report']['bucket']='other'
    calls=[]
    def evidence(*args):
        assert ('contracts_sha' in args[0].execution_profile)==sealed
        if sealed:
            assert args[0].execution_profile['v2_entry_count']==20
        calls.append('sql')
        if mode=='scope':return None
        value=deepcopy(record)
        if mode=='changed_after_read' and len(calls)>1:value['report']['object_version']='v2'
        return value
    def get(bucket,key,version_id):
        calls.append(key);assert bucket=='test' and version_id=='v1'
        raw=report if key=='report' else calibration
        if mode=='corrupt':raw=b' '+raw[1:]
        return BytesIO(raw)
    def run():return load_donor_qualification(SimpleNamespace(donor_qualification_evidence=evidence),lease,body,proof,SimpleNamespace(get_object=get),'test',check_active=lambda:None)
    if mode=='ok':
        r,c,p,current=run();assert (r,c,p)==(report,calibration,policy)
        assert current()==record['pin'] and calls==['sql','report','calibration','sql','sql']
    else:
        with pytest.raises(ValueError):run()
        if mode in ('scope','version','bucket'):assert calls==['sql']
