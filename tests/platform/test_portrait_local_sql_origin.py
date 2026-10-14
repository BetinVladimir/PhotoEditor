from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.manual_mask import MaskRasterParams
from photo_worker.operations.portrait.local_sql_origin import SQLManualPairAuthority
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from test_portrait_lease_gated_authority import setup

@pytest.mark.parametrize('external',[False,True])
def test_sql_origin_pins_producer_and_repeats_after_read(external):
    ctx,params,origin=setup()
    vectors=ArtifactRef(artifact_id=uuid4(),sha256='f'*64) if external else None
    # Inline geometry is validated later by physical replay; this document must
    # nevertheless be structurally valid and source-bound before origin returns.
    if external:
        producer=MaskRasterParams(image=params.base.source,vectors=vectors)
    else:
        from photo_worker.contracts.manual_mask import ManualMask
        from photo_worker.contracts.images import Canvas
        canvas=Canvas.model_validate_json('{"width":1,"height":1,"orientation":1,"space_id":"original","source_sha256":"'+ 'e'*64+'"}')
        producer=MaskRasterParams(image=params.base.source,inline_vectors=ManualMask(source_sha256='e'*64,canvas=canvas,shapes=[]))
    raw=canonical_bytes(producer)
    original=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    ctx.lease.command.inputs=[origin.root,original]+([] if vectors is None else [vectors])
    calls=[]
    db=SimpleNamespace(local_portrait_manual_origin=lambda *args:(calls.append(args) or (origin.root.artifact_id,origin.root.sha256,original.artifact_id,original.sha256)))
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw)
    authority=SQLManualPairAuthority(db,artifacts)
    result=authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))
    assert result.root==origin.root and result.params==original and result.vectors==vectors
    assert len(calls)==2 and calls[0][-1] is None and calls[1][-1]==vectors
    original_lookup=db.local_portrait_manual_origin
    reads=[]
    def changed(*args):
        reads.append(args)
        if len(reads)==1:return original_lookup(*args)
        return (uuid4(),'a'*64,original.artifact_id,original.sha256)
    db.local_portrait_manual_origin=changed
    with pytest.raises(OperationFailure):authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))
    db.local_portrait_manual_origin=lambda *args:None
    with pytest.raises(OperationFailure):authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))
    db.local_portrait_manual_origin=original_lookup
    artifacts.contract_bytes=lambda *args:raw+b' '
    with pytest.raises(OperationFailure):authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))
    ctx.lease.command.inputs=[]
    with pytest.raises(OperationFailure):authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))

def test_database_origin_transport_binds_all_attempt_and_pixel_pins():
    ctx,params,origin=setup();db=TaskDatabase('unused');calls=[]
    db._query=lambda sql,args:(calls.append((sql,args)) or None)
    assert db.local_portrait_manual_origin(ctx.lease,canonical_bytes(params),params.mask,params.protected) is None
    sql,args=calls[0]
    assert sql.count('%s')==16 and len(args)==16
    assert args[:3]==(ctx.lease.command.tenant_id,ctx.lease.command.run_id,ctx.lease.command.task_id)
    assert args[-6:]==(params.mask.artifact_id,params.mask.sha256,params.protected.artifact_id,params.protected.sha256,None,None)

@pytest.mark.parametrize('row',[{}, {'root':'a','sha':'b','params':'c','psha':'d'}, ('bad','a'*64,uuid4(),'b'*64), (uuid4(),'BAD',uuid4(),'b'*64), (uuid4(),)])
def test_sql_origin_rejects_malformed_locator_without_object_read(row):
    ctx,params,_=setup();ctx.lease.command.inputs=[]
    db=SimpleNamespace(local_portrait_manual_origin=lambda *args:row)
    def read(*args):raise AssertionError('unauthorized object read')
    authority=SQLManualPairAuthority(db,SimpleNamespace(contract_bytes=read))
    with pytest.raises(OperationFailure):authority.resolve(ctx,params,None,SimpleNamespace(artifact=params.mask),SimpleNamespace(artifact=params.protected))
