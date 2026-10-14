from copy import deepcopy
from io import BytesIO
import pytest
from photo_worker.v2.portrait_donor_adoption_qualification import load_donor_adoption_qualification
from test_portrait_donor_qualification import fixture


@pytest.mark.parametrize('mode',['ok','corrupt','version','bucket','revoked','oversize'])
def test_completed_qualification_original_reads_and_current_snapshot(mode):
    profile,report,calibration,policy=fixture();calls=[];closed=[]
    locations=[dict(contract=contract,**profile['donor_qualification'][key],bucket='bucket',object_key=key,object_version='v1',bytes=len(body))
        for contract,key,body in [('Calibration','calibration',calibration),('Report','report',report)]]
    evidence={'profile':profile,'qualification_objects':locations};expected=deepcopy(evidence)
    if mode=='version':locations[0]['object_version']='null'
    if mode=='bucket':locations[0]['bucket']='other'
    if mode=='oversize':locations[0]['bytes']=(1<<20)+1
    current=deepcopy(evidence)
    def checkpoint():
        calls.append('authority')
        if mode=='revoked' and 'object' in calls:raise ValueError('revoked')
        return current
    class Response(BytesIO):
        def release_conn(self):closed.append(True)
    class Objects:
        def get_object(self,bucket,key,version_id):
            assert bucket=='bucket' and version_id=='v1';calls.append('object')
            raw=calibration if key=='calibration' else report
            if mode=='corrupt':raw=raw[:-1]+b' '
            return Response(raw)
    def run():return load_donor_adoption_qualification(evidence,Objects(),policy,bucket='bucket',checkpoint=checkpoint,check_active=lambda:None)
    if mode=='ok':assert run()[1].min_face_edge_px==64 and len(closed)==2
    else:
        with pytest.raises(ValueError):run()
        if mode in ('version','bucket','oversize'):assert calls==[]
        if mode=='revoked':assert closed==[True]
