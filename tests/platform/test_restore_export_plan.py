from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime import restore_export_plan as m


@pytest.mark.parametrize('mode',['valid','authority','missing','hash','params','task_ref','inactive'])
def test_registered_encode_params_strict_semantics_and_go_omission(mode):
    params=m.EncodeParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),format='jpeg',icc_id='srgb_v1',
        icc_sha256='b'*64,quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip')
    document=params.model_dump(mode='json',exclude_none=True)
    if mode=='params':document['quality']=80
    digest=sha256(canonical_bytes(document)).hexdigest()
    task_ref={'artifact_id':str(uuid4()),'sha256':'f'*64 if mode=='task_ref' else digest}
    tenant=uuid4()
    result=NS(registration=NS(run_id=uuid4()),artifact_id=uuid4(),object=NS(sha256='c'*64))
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT p.params'):
                assert 't.inputs=p.inputs' in sql and 'encoded.created_by_task_id=t.id' in sql
                row=None if mode=='missing' else (document,'f'*64 if mode=='hash' else digest,task_ref,digest,'photos',f'tenants/{tenant}/params','v1',len(canonical_bytes(document)))
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.verify_fixture_export_plan(Connection(),tenant,result,params,expected_database='restore',
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke()==digest and 'size' not in document
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls
