from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2 import delivery_encoded_service as service


def fixture():
    row = (2, 7, uuid4(), 'export.encode', '1', uuid4(), 'a'*64, uuid4(), 'b'*64)
    class Connection:
        autocommit = True
        rows = [row, row]
        calls = []
        def execute(self, query, args):
            self.calls.append((query,args))
            return SimpleNamespace(fetchone=lambda: self.rows.pop(0))
    return Connection(), row


@pytest.mark.parametrize('change', ['valid','revoked','fence','result','root','params'])
def test_delivery_encode_context_rechecked_after_physical_io(monkeypatch,tmp_path,change):
    connection,row = fixture()
    if change == 'revoked': connection.rows[1] = None
    if change in ('fence','result','root','params'):
        updated=list(row)
        index={'fence':1,'result':2,'root':5,'params':7}[change]
        updated[index]=8 if change=='fence' else uuid4()
        connection.rows[1]=tuple(updated)
    physical=[]
    proof=object()
    def prepare(reader,params,root,**options):
        assert reader.result_id==row[2] and reader.attempt==2 and reader.fence==7
        assert params.artifact_id==row[7] and root.artifact_id==row[5]
        assert connection.autocommit
        physical.append(True)
        return proof
    monkeypatch.setattr(service,'prepare_delivery_encode',prepare)
    args=dict(tenant_id=uuid4(),task_id=uuid4(),allowed_bucket='photos',scratch_root=tmp_path,max_bytes=100,max_pixels=12)
    if change=='valid':
        result=service.prepare_current_delivery_encode(connection,object(),**args)
        assert result.evidence is proof and result.context.result_id==row[2]
    else:
        with pytest.raises(AdoptionInvalid):service.prepare_current_delivery_encode(connection,object(),**args)
    assert physical==[True] and len(connection.calls)==2


def test_delivery_encode_preparation_refuses_open_transaction_before_sql(tmp_path):
    connection,_=fixture();connection.autocommit=False
    with pytest.raises(ValueError,match='AUTOCOMMIT'):
        service.prepare_current_delivery_encode(connection,object(),tenant_id=uuid4(),task_id=uuid4(),
            allowed_bucket='photos',scratch_root=tmp_path,max_bytes=100,max_pixels=12)
    assert connection.calls==[]


def prepared_fixture():
    from hashlib import sha256
    from photo_worker.v2.adoption import ArtifactRef
    from photo_worker.v2.delivery_encoded_prepare import PreparedDeliveryEncode
    from photo_worker.v2.delivery_encoded_verify import VerifiedEncodedRaster
    params, source, manifest = b'{"params":1}', b'{"source":1}', b'{"manifest":1}'
    root=ArtifactRef(uuid4(),sha256(manifest).hexdigest())
    raster=ArtifactRef(uuid4(),'c'*64)
    context=service.DeliveryEncodeContext(2,7,uuid4(),root,ArtifactRef(uuid4(),sha256(params).hexdigest()))
    evidence=PreparedDeliveryEncode(params,source,manifest,root,raster,VerifiedEncodedRaster(raster.sha256,42,4,3,False))
    return service.PreparedCurrentDeliveryEncode(context,evidence,uuid4(),uuid4())


@pytest.mark.parametrize('response', [(True,), (False,), (None,), (1,), None, (), (True,True)])
def test_delivery_encode_record_binds_exact_proof_and_requires_true(response):
    prepared=prepared_fixture()
    calls=[]
    connection=SimpleNamespace(execute=lambda sql,args: calls.append((sql,args)) or SimpleNamespace(fetchone=lambda:response))
    args=dict(tenant_id=prepared.tenant_id,task_id=prepared.task_id,prepared=prepared)
    if response==(True,) and type(response[0]) is bool:
        service.record_prepared_delivery_encode(connection,**args)
    else:
        with pytest.raises(AdoptionInvalid,match='VERIFICATION_REFUSED'):
            service.record_prepared_delivery_encode(connection,**args)
    c,e=prepared.context,prepared.evidence
    assert len(calls)==1
    assert calls[0][1]==(prepared.tenant_id,prepared.task_id,2,7,c.result_id,
        e.params_jcs.decode(),e.source_jcs.decode(),e.manifest_jcs.decode(),42)


@pytest.mark.parametrize('change',['tenant','task','root','cancelled'])
def test_delivery_encode_record_refuses_changed_scope_before_sql(change):
    from dataclasses import replace
    from photo_worker.v2.adoption import ArtifactRef
    prepared=prepared_fixture()
    tenant,task=prepared.tenant_id,prepared.task_id
    if change=='tenant':tenant=uuid4()
    if change=='task':task=uuid4()
    if change=='root':prepared=replace(prepared,context=replace(prepared.context,root=ArtifactRef(uuid4(),'d'*64)))
    calls=[]
    connection=SimpleNamespace(execute=lambda *args:calls.append(args))
    def active():
        if change=='cancelled':raise RuntimeError('cancelled')
    with pytest.raises((AdoptionInvalid,RuntimeError)):
        service.record_prepared_delivery_encode(connection,tenant_id=tenant,task_id=task,prepared=prepared,check_active=active)
    assert calls==[]


def test_delivery_encode_adoption_uses_dedicated_atomic_commit():
    prepared=prepared_fixture()
    calls=[]
    connection=SimpleNamespace(execute=lambda sql,args:calls.append((sql,args)) or SimpleNamespace(fetchone=lambda:(True,)))
    service.adopt_prepared_delivery_encode(connection,tenant_id=prepared.tenant_id,
        task_id=prepared.task_id,prepared=prepared)
    assert len(calls)==1
    assert calls[0][0]=='SELECT photo.adopt_v30_delivery_encode(%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    assert calls[0][1][:5]==(prepared.tenant_id,prepared.task_id,2,7,prepared.context.result_id)


@pytest.mark.parametrize('scenario',['success','prepare_error','sql_error','cancel_after_sql'])
def test_delivery_encode_runtime_transaction_boundaries(monkeypatch,tmp_path,scenario):
    from contextlib import contextmanager
    trace=[]
    prepared=prepared_fixture()
    read=SimpleNamespace(autocommit=True)
    class Write:
        autocommit=True
        @contextmanager
        def transaction(self):
            trace.append('begin')
            try:yield
            except BaseException:
                trace.append('rollback');raise
            else:trace.append('commit')
    write=Write()
    sql_done=False
    def prepare(connection,objects,**args):
        assert connection is read and trace==[]
        trace.append('physical')
        if scenario=='prepare_error':raise RuntimeError('prepare')
        return prepared
    def adopt(connection,**args):
        nonlocal sql_done
        assert connection is write and trace==['physical','begin']
        assert args['prepared'] is prepared
        trace.append('sql')
        sql_done=True
        if scenario=='sql_error':raise RuntimeError('sql')
    def active():
        if scenario=='cancel_after_sql' and sql_done:raise RuntimeError('cancelled')
    monkeypatch.setattr(service,'prepare_current_delivery_encode',prepare)
    monkeypatch.setattr(service,'adopt_prepared_delivery_encode',adopt)
    runtime=service.DeliveryEncodeAdoptionService(read,write,object(),allowed_bucket='photos',
        scratch_root=tmp_path,max_bytes=100000,max_pixels=12,check_active=active)
    if scenario=='success':
        assert runtime.adopt(prepared.tenant_id,prepared.task_id) is True
        assert trace==['physical','begin','sql','commit']
    else:
        with pytest.raises(RuntimeError):runtime.adopt(prepared.tenant_id,prepared.task_id)
        assert trace==(['physical'] if scenario=='prepare_error' else ['physical','begin','sql','rollback'])


@pytest.mark.parametrize('scenario',['valid','duplicate','oversize','invalid','rejected','infrastructure'])
def test_delivery_encode_pending_page_is_bounded_and_validated_before_adoption(scenario):
    rows=[(uuid4(),uuid4()),(uuid4(),uuid4())]
    if scenario=='duplicate':rows[1]=rows[0]
    if scenario=='oversize':rows.append((uuid4(),uuid4()))
    if scenario=='invalid':rows[1]=(uuid4(),'untrusted')
    calls=[]
    runtime=object.__new__(service.DeliveryEncodeAdoptionService)
    from contextlib import nullcontext
    deferred=[]
    runtime.write_connection=SimpleNamespace(transaction=lambda:nullcontext(),execute=lambda sql,args:deferred.append(args) or SimpleNamespace(fetchone=lambda:(True,)))
    runtime.check_active=lambda:None
    runtime.read_connection=SimpleNamespace(execute=lambda sql,args:SimpleNamespace(fetchall=lambda:rows))
    def adopt(tenant,task):
        calls.append((tenant,task))
        if len(calls)==1 and scenario=='rejected':raise AdoptionInvalid('refused')
        if scenario=='infrastructure':raise RuntimeError('database')
        return True
    runtime.adopt=adopt
    if scenario in ('duplicate','oversize','invalid'):
        with pytest.raises(AdoptionInvalid):runtime.process_pending(2)
        assert calls==[]
    elif scenario=='infrastructure':
        with pytest.raises(RuntimeError):runtime.process_pending(2)
        assert len(calls)==1
    else:
        result=runtime.process_pending(2)
        assert result.candidates==2
        assert result.adopted==(1 if scenario=='rejected' else 2)
        assert len(result.rejected)==(1 if scenario=='rejected' else 0)
        assert calls==rows
        assert deferred==([rows[0]] if scenario=='rejected' else [])


@pytest.mark.parametrize('cancel_at',[2,3])
def test_delivery_encode_empty_page_reports_late_cancellation(cancel_at):
    runtime=object.__new__(service.DeliveryEncodeAdoptionService)
    runtime.read_connection=SimpleNamespace(execute=lambda sql,args:SimpleNamespace(fetchall=lambda:[]))
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==cancel_at:
            raise RuntimeError('cancelled')
    runtime.check_active=active
    with pytest.raises(RuntimeError,match='cancelled'):
        runtime.process_pending(2)
    assert calls==cancel_at
