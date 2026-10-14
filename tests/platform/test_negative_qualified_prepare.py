from copy import deepcopy
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.negative_qualified_prepare import prepare_qualified_negative_files
from test_negative_qualification import fixture
from test_negative_runtime_lock import lock


@pytest.mark.parametrize('revoked',[False,True])
def test_qualified_negative_preparation_rechecks_pin_and_cleans_files(tmp_path,revoked):
    p,c,r,pin,policy=fixture();runtime=lock();digest=canonical_hash(runtime)
    import json
    report=json.loads(r);report['runtime_lock_sha']=digest;r=canonical_bytes(report)
    from hashlib import sha256
    pin['report']['sha256']=sha256(r).hexdigest()
    calibration=json.loads(c);calibration['qualification']=deepcopy(pin['report']);c=canonical_bytes(calibration)
    pin['calibration']['sha256']=sha256(c).hexdigest();p['calibration']=deepcopy(pin['calibration'])
    pin['runtime_lock_sha']=digest;policy['runtime_lock_sha']=digest
    policy['calibration_shas']=[pin['calibration']['sha256']];pin['policy_sha']=canonical_hash(policy)
    calls=[]
    def current():
        calls.append(1);value=deepcopy(pin)
        if revoked and len(calls)>7:value['qualification_id']=str(uuid4())
        return value
    scan=np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        def run():
            with prepare_qualified_negative_files(scan,p,c,r,policy,pin,scratch,input_icc_sha='c'*64,
                    runtime_lock=runtime,runtime_sha=digest,ram_max_bytes=1<<30,
                    load_current_pin=current,check_active=lambda:None) as files:
                np.testing.assert_allclose(np.load(files.rgb),0,atol=1e-7)
        if revoked:
            with pytest.raises(ValueError,match='NEGATIVE_QUALIFICATION_STALE'):run()
        else:run()
        assert not list(scratch.directory.iterdir()) and len(calls)>7


@pytest.mark.parametrize('changed',[False,True])
def test_located_preparation_rechecks_original_catalog_through_staged_use(tmp_path,monkeypatch,changed):
    from io import BytesIO
    from photo_worker.v2 import negative_qualification_objects,negative_qualification_catalog
    from photo_worker.v2.negative_qualified_prepare import prepare_located_negative_files
    from hashlib import sha256
    import json
    p,c,r,pin,policy=fixture();runtime=lock();digest=canonical_hash(runtime)
    report=json.loads(r);report['runtime_lock_sha']=digest;r=canonical_bytes(report)
    pin['report']['sha256']=sha256(r).hexdigest()
    calibration=json.loads(c);calibration['qualification']=deepcopy(pin['report']);c=canonical_bytes(calibration)
    pin['calibration']['sha256']=sha256(c).hexdigest();p['calibration']=deepcopy(pin['calibration'])
    pin['runtime_lock_sha']=digest;policy['runtime_lock_sha']=digest
    policy['calibration_shas']=[pin['calibration']['sha256']];pin['policy_sha']=canonical_hash(policy)
    locs=[dict(contract=name,artifact_id=pin[key]['artifact_id'],sha256=pin[key]['sha256'],
        bucket='bucket',object_key=key,object_version='v1',bytes=len(body))
        for name,key,body in [('NegativeCalibration','calibration',c),('QualificationReport','report',r)]]
    staged=[];reads=[];checks=[]
    def catalog(*args,**kwargs):
        checks.append(1);value=deepcopy(locs)
        if changed and staged:value[0]['object_version']='v2'
        return value
    monkeypatch.setattr(negative_qualification_objects,'resolve_negative_qualification_objects',catalog)
    monkeypatch.setattr(negative_qualification_catalog,'resolve_negative_qualification_objects',catalog)
    class Objects:
        def get_object(self,bucket,key,version_id):
            reads.append(key);return BytesIO(c if key=='calibration' else r)
    scan=np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        def run():
            with prepare_located_negative_files(None,Objects(),*[uuid4() for _ in range(4)],
                    scan,p,policy,pin,scratch,bucket='bucket',input_icc_sha='c'*64,
                    runtime_lock=runtime,runtime_sha=digest,ram_max_bytes=1<<30,check_active=lambda:None) as files:
                np.testing.assert_allclose(np.load(files.rgb),0,atol=1e-7)
                staged.append(1)
        if changed:
            with pytest.raises(ValueError,match='NEGATIVE_QUALIFICATION_CHANGED'):run()
        else:run()
        assert not list(scratch.directory.iterdir())
    assert reads==['calibration','report'] and len(checks)>10
