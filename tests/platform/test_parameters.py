import hashlib
import io
from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef,Contract
from photo_worker.runtime.parameters import read_contract
from photo_worker.runtime.materialize import ArtifactTransferError,materialize_gateway
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota

class Params(Contract):
    strength:float
    enabled:bool

class Stream(io.BytesIO):
    def __init__(self,data):super().__init__(data);self.size=len(data);self.reads=0
    def read(self,n=-1):
        assert 0<=n<=65536
        self.reads+=1
        return super().read(n)

def ref_for(data):return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())

def test_params_strict_json_and_digest():
    data=b'{"strength":0.5,"enabled":true}';source=Stream(data)
    result=read_contract(ref_for(data),Params,lambda _:source,lambda:None)
    assert result.enabled and result.strength==0.5 and source.closed
    for invalid in (b'{"strength":0.5,"strength":0.8,"enabled":true}',b'{"strength":NaN,"enabled":true}'):
        with pytest.raises(ArtifactTransferError,match='JSON_INVALID'):read_contract(ref_for(invalid),Params,lambda _:Stream(invalid),lambda:None)
    unknown=b'{"strength":0.5,"enabled":true,"extra":1}'
    with pytest.raises(ValidationError):read_contract(ref_for(unknown),Params,lambda _:Stream(unknown),lambda:None)
    with pytest.raises(ArtifactTransferError,match='HASH_MISMATCH'):read_contract(ref_for(b'other'),Params,lambda _:Stream(data),lambda:None)

def test_params_limit_and_cancel_close_stream():
    data=b'x'*100;source=Stream(data)
    with pytest.raises(ArtifactTransferError,match='SIZE_MISMATCH'):read_contract(ref_for(data),Params,lambda _:source,lambda:None,max_bytes=10)
    assert source.closed and source.reads==1
    checks=0
    def cancelled():
        nonlocal checks
        checks+=1
        if checks>1:raise RuntimeError('cancelled')
    source=Stream(data)
    with pytest.raises(RuntimeError,match='cancelled'):read_contract(ref_for(data),Params,lambda _:source,cancelled)
    assert source.closed and source.reads==0

def test_gateway_size_reserves_before_read(tmp_path):
    data=b'abcd';source=Stream(data)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,3,min_free_bytes=0) as scratch:
        with pytest.raises(DiskQuota):materialize_gateway(ref_for(data),scratch,lambda _:source,lambda:None)
        assert source.closed and source.reads==0
    source=Stream(data)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4,min_free_bytes=0) as scratch:
        with materialize_gateway(ref_for(data),scratch,lambda _:source,lambda:None) as artifact:
            assert artifact.path.read_bytes()==data
        assert source.closed and not artifact.path.exists()


def large_style_payload():
    import json
    from pathlib import Path
    data=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_text())
    # Valid dense17 residual is small enough; whitespace ensures >1MiB input
    # while avoiding an invalid schema or reducing the ordinary control limit.
    data['global']['residual_lut'].update(encoding='dense',values=[.012345678901234567]*(17**3*3))
    return json.dumps(data,indent=40).encode()


def test_large_style_has_its_own_limit_and_strict_reader():
    from photo_worker.contracts.style import StyleV2
    from photo_worker.runtime.parameters import read_style
    data=large_style_payload();assert len(data)>1048576
    stream=Stream(data)
    result=read_style(ref_for(data),lambda _:stream,lambda:None)
    assert stream.closed and result.global_.residual_lut.encoding=='dense'
    with pytest.raises(ArtifactTransferError,match='SIZE_MISMATCH'):
        read_contract(ref_for(data),StyleV2,lambda _:Stream(data),lambda:None)
    duplicate=b'{"schema_version":"2.0.0",'+data.lstrip()[1:]
    with pytest.raises(ArtifactTransferError,match='JSON_INVALID'):
        read_style(ref_for(duplicate),lambda _:Stream(duplicate),lambda:None)
    with pytest.raises(ArtifactTransferError,match='HASH_MISMATCH'):
        read_style(ref_for(b'wrong'),lambda _:Stream(data),lambda:None)


def test_style_reader_stops_at_large_artifact_cap(monkeypatch):
    import photo_worker.runtime.parameters as parameters
    monkeypatch.setattr(parameters,'STYLE_DOCUMENT_LIMIT',100)
    source=Stream(b'x'*101)
    with pytest.raises(ArtifactTransferError,match='SIZE_MISMATCH'):
        parameters.read_style(ref_for(b'x'*101),lambda _:source,lambda:None)
    assert source.closed and source.reads==1


def test_task_style_requires_declared_ref_and_admitted_ram(tmp_path):
    from types import SimpleNamespace
    from photo_worker.runtime.artifacts import TaskArtifacts
    from test_asset_adapters import context_for
    data=large_style_payload();ref=ref_for(data);ctx=context_for(ref,'style.resolve');calls=[]
    def opened(requested):calls.append(requested);return Stream(data)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,0,min_free_bytes=0) as scratch:
        with TaskArtifacts(ctx,SimpleNamespace(open_stream=opened),scratch) as artifacts:
            with pytest.raises(ValueError,match='admitted RAM'):artifacts.style(ref)
            assert not calls
            ctx.lease.execution_profile['resource_estimate']={'ram_bytes':3*1024**3}
            with pytest.raises(ValueError,match='declared'):artifacts.style(ref_for(data))
            assert not calls
            result=artifacts.style(ref)
            assert result.global_.residual_lut.encoding=='dense' and calls==[ref]


@pytest.mark.parametrize('mode',['valid','digest','duplicate','schema','limit'])
def test_contract_bytes_preserves_exact_validated_document(mode):
    from photo_worker.runtime.parameters import read_contract_bytes
    data=b'{ "enabled": true, "strength": 0.5 }\n'
    if mode=='duplicate':data=b'{"enabled":true,"enabled":false,"strength":0.5}'
    if mode=='schema':data=b'{"enabled":true,"strength":"invalid"}'
    source=Stream(data)
    ref=ref_for(b'changed') if mode=='digest' else ref_for(data)
    if mode=='valid':assert read_contract_bytes(ref,Params,lambda _:source,lambda:None)==data
    else:
        with pytest.raises((ArtifactTransferError,ValidationError)):
            read_contract_bytes(ref,Params,lambda _:source,lambda:None,max_bytes=2 if mode=='limit' else 1048576)
    assert source.closed
