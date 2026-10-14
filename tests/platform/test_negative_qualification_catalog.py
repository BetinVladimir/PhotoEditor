from contextlib import contextmanager
from uuid import uuid4
import pytest
from photo_worker.v2.negative_qualification_catalog import resolve_negative_qualification_objects
from test_negative_qualification import fixture


@pytest.mark.parametrize('mode',['ok','role','revoked','version','commit'])
def test_negative_catalog_commits_before_returning_owned_locations(mode):
    _,_,_,pin,_=fixture();calls=[]
    locations=[dict(contract=name,artifact_id=pin[key]['artifact_id'],sha256=pin[key]['sha256'],bucket='bucket',
        object_key=key,object_version='v1',bytes=100) for name,key in [('NegativeCalibration','calibration'),('QualificationReport','report')]]
    if mode=='version':locations[0]['object_version']='null'
    class Connection:
        autocommit=True
        @contextmanager
        def transaction(self):
            calls.append('begin');yield
            if mode=='commit':raise ValueError('commit failure')
            calls.append('commit')
        def execute(self,query,args=None):
            calls.append(query)
            if query=='SELECT current_user':
                assert calls[1]=="SET LOCAL statement_timeout = '15s'"
            class Cursor:
                def fetchone(self):
                    if query=='SELECT current_user':return ('photo_worker' if mode=='role' else 'photo_scheduler',)
                    return (None if mode=='revoked' else locations,)
            return Cursor()
    def run():return resolve_negative_qualification_objects(Connection(),*[uuid4() for _ in range(4)],pin,check_active=lambda:None)
    if mode=='ok':
        got=run();locations[0]['object_version']='changed'
        assert got[0]['object_version']=='v1' and calls[-1]=='commit'
    else:
        with pytest.raises(ValueError):run()


def test_negative_catalog_missing_connection_refused_before_activity():
    _,_,_,pin,_=fixture()
    def forbidden():pytest.fail('Invalid catalog reached external callback')
    with pytest.raises(ValueError,match='NEGATIVE_CATALOG_CONFIGURATION'):
        resolve_negative_qualification_objects(None,*[uuid4() for _ in range(4)],pin,check_active=forbidden)
