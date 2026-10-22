from hashlib import sha256
import json
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes
from photo_worker.operations.region.presentation import prepare_region_presentation,write_region_presentation
from photo_worker.operations.region.result import build_region_manifest
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef,ArtifactEvidence,prepare_v2_adoption
from test_region_preview_presentation import fixture,ROOT,ICC_LOCK
from test_v2_adoption import Reader


class RegionReader(Reader):
    def iter_bytes(self,ref,max_bytes):
        body=self.body[ref]
        for offset in range(0,len(body),65536):yield body[offset:offset+65536]


def case(tmp_path):
    params,body=fixture();params.source.bytes=len(body)
    rendered=prepare_region_presentation(params,body,np.full((300,400,3),.5,np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    path=tmp_path/'region.jpg';size=write_region_presentation(path,rendered)
    binary=path.read_bytes();derivative=ContractRef(artifact_id=uuid4(),sha256=sha256(binary).hexdigest())
    manifest=build_region_manifest(params,rendered,derivative,size)
    reader=RegionReader()
    ref=lambda value:ArtifactRef(value.artifact_id,value.sha256)
    params_body=canonical_bytes(params)
    params_ref=ArtifactRef(uuid4(),sha256(params_body).hexdigest())
    reader.add(params_ref,body=json.loads(params_body),source='input')
    reader.add(ref(params.source.ref),body=json.loads(body),source='input')
    reader.add(ref(params.recipe.ref),body=params.frozen_recipe.model_dump(mode='json'),source='input')
    output_ref=ref(derivative)
    reader.rows[output_ref]=ArtifactEvidence(output_ref,'image/jpeg',len(binary),'derivative','output')
    reader.body[output_ref]=binary;reader.uploads.add(output_ref)
    root=ArtifactRef(uuid4(),sha256(canonical_bytes(manifest)).hexdigest())
    reader.add(root,body=manifest.model_dump(mode='json'),role='document')
    return reader,params_ref,root,rendered.profile_sha256,output_ref


def prepare(reader,params,root,icc):
    return prepare_v2_adoption(task_id=uuid4(),operation='region.preview',version='1',root=root,reader=reader,params_ref=params,region_target_icc_sha=icc)


def test_generic_adoption_accepts_bound_region_and_exact_output_closure(tmp_path):
    reader,params,root,icc,output=case(tmp_path)
    prepared=prepare(reader,params,root,icc)
    assert prepared is not None
    with pytest.raises(AdoptionInvalid,match='ICC_REQUIRED'):prepare(reader,params,root,None)
    reader.body[output]=reader.body[output]+b'!'
    with pytest.raises(AdoptionInvalid,match='STREAM_INVALID'):prepare(reader,params,root,icc)


def test_region_output_scope_and_params_are_mandatory(tmp_path):
    reader,params,root,icc,output=case(tmp_path)
    with pytest.raises(AdoptionInvalid,match='PARAMS_REQUIRED'):
        prepare(reader,None,root,icc)
    row=reader.rows[output]
    reader.rows[output]=ArtifactEvidence(output,row.media_type,row.bytes,'other','output')
    with pytest.raises(AdoptionInvalid,match='OUTPUT_INVALID'):prepare(reader,params,root,icc)


@pytest.mark.parametrize('icc,row_present',[(None,True),('e'*64,False),('e'*64,True)])
def test_coordinator_passes_trusted_icc_and_generation_bound_params(monkeypatch,icc,row_present):
    from types import SimpleNamespace
    import photo_worker.v2.adoption_service as service
    tenant,task,result=uuid4(),uuid4(),uuid4()
    params_ref=ArtifactRef(uuid4(),'b'*64);root=ArtifactRef(uuid4(),'a'*64)
    queries=[]
    class Read:
        autocommit=True
        def execute(self,query,args):
            queries.append((query,args))
            assert args==(tenant,task,3,9,result)
            if 'v26_region_adoption_execution_profile' in query:
                return SimpleNamespace(fetchone=lambda:({},))
            assert 'v26_region_adoption_params' in query
            return SimpleNamespace(fetchone=lambda:(params_ref.artifact_id,params_ref.sha256) if row_present else None)
    class Write:
        def transaction(self):raise AssertionError('No write transaction before proof')
    from photo_worker.v2.region_replay import NativeRegionReplay
    monkeypatch.setattr(NativeRegionReplay,'verify',lambda *a,**kw:None)
    coordinator=service.AdoptionCoordinator(Read(),Write(),object(),'private',region_target_icc_sha=icc,region_replay=object.__new__(NativeRegionReplay))
    coordinator.context=lambda *_:service.AdoptionContext(3,9,result,'region.preview','1',root)
    def proof(**kwargs):
        assert kwargs['region_target_icc_sha']==icc and kwargs['params_ref']==params_ref
        raise RuntimeError('proof reached')
    monkeypatch.setattr(service,'prepare_v2_adoption',proof)
    if icc is None:
        with pytest.raises(AdoptionInvalid,match='ICC_REQUIRED'):coordinator.adopt(tenant,task)
        assert queries==[]
    elif not row_present:
        with pytest.raises(AdoptionInvalid,match='PARAMS_STALE'):coordinator.adopt(tenant,task)
    else:
        with pytest.raises(RuntimeError,match='proof reached'):coordinator.adopt(tenant,task)


@pytest.mark.parametrize('invalid',[False,True])
def test_region_stream_close_error_does_not_mask_invalid_data(tmp_path,invalid):
    reader,params,root,icc,output=case(tmp_path)
    class ClosingIterator:
        def __init__(self):self.blocks=iter([b'' if invalid else reader.body[output]]);self.closed=0
        def __iter__(self):return self
        def __next__(self):return next(self.blocks)
        def close(self):self.closed+=1;raise OSError('close failed')
    stream=ClosingIterator()
    reader.iter_bytes=lambda *args:stream
    expected=AdoptionInvalid if invalid else OSError
    message='STREAM_INVALID' if invalid else 'close failed'
    with pytest.raises(expected,match=message):prepare(reader,params,root,icc)
    assert stream.closed==1


def test_region_adoption_cancellation_after_params_stops_next_reads(tmp_path):
    from photo_worker.v2.region_adoption import verify_region_result
    from photo_worker.runtime.executor import OperationFailure
    reader,params,root,icc,output=case(tmp_path)
    original=reader.read_json;reads=[];active=[True]
    def read_json(ref,max_bytes):
        result=original(ref,max_bytes);reads.append(ref);active[0]=False
        return result
    reader.read_json=read_json
    def check():
        if not active[0]:raise OperationFailure('LEASE_LOST')
    with pytest.raises(OperationFailure,match='LEASE_LOST'):
        verify_region_result(reader,params,root,target_icc_sha256=icc,check_active=check)
    assert reads==[params]


def test_region_adoption_cancellation_during_stream_closes_iterator(tmp_path):
    from photo_worker.v2.region_adoption import verify_region_result
    from photo_worker.runtime.executor import OperationFailure
    reader,params,root,icc,output=case(tmp_path)
    active=[True]
    class CancelledStream:
        closed=0
        def __iter__(self):return self
        def __next__(self):active[0]=False;return reader.body[output]
        def close(self):self.closed+=1
    stream=CancelledStream();reader.iter_bytes=lambda *args:stream
    def check():
        if not active[0]:raise OperationFailure('LEASE_LOST')
    with pytest.raises(OperationFailure,match='LEASE_LOST'):
        verify_region_result(reader,params,root,target_icc_sha256=icc,check_active=check)
    assert stream.closed==1


def test_generic_region_adoption_forwards_activity_into_evidence_reads(tmp_path):
    reader, params, root, icc, _ = case(tmp_path)
    reads = []
    original = reader.read_json
    def read(ref, limit):
        reads.append(ref)
        return original(ref, limit)
    reader.read_json = read
    def active():
        if reads:
            raise InterruptedError('stopped after Params read')
    with pytest.raises(InterruptedError, match='Params'):
        prepare_v2_adoption(task_id=uuid4(), operation='region.preview', version='1',
            root=root, reader=reader, params_ref=params, region_target_icc_sha=icc,
            check_active=active)
    assert reads == [params]


def test_coordinator_stop_after_proof_never_enters_write_transaction(monkeypatch):
    from threading import Event
    from types import SimpleNamespace
    from photo_worker.v2 import adoption_service as service
    stop = Event()
    def active():
        if stop.is_set():
            raise InterruptedError('stopped')
    params = ArtifactRef(uuid4(), 'a' * 64)
    class Read:
        autocommit = True
        def execute(self, query, args):
            if 'v26_region_adoption_execution_profile' in query:
                return SimpleNamespace(fetchone=lambda:({},))
            assert 'v26_region_adoption_params' in query
            return SimpleNamespace(fetchone=lambda: (params.artifact_id, params.sha256))
    class Write:
        def transaction(self):
            pytest.fail('write transaction after stopped proof')
    from photo_worker.v2.region_replay import NativeRegionReplay
    monkeypatch.setattr(NativeRegionReplay,'verify',lambda *a,**kw:None)
    coordinator = service.AdoptionCoordinator(Read(), Write(), object(), 'private',
        region_target_icc_sha='b' * 64, check_active=active,region_replay=object.__new__(NativeRegionReplay))
    coordinator.context = lambda *args: service.AdoptionContext(1, 2, uuid4(),
        'region.preview', '1', ArtifactRef(uuid4(), 'c' * 64))
    def proof(**kwargs):
        kwargs['check_active']()
        stop.set()
        with pytest.raises(InterruptedError,match='stopped'):
            kwargs['check_active']()
        return SimpleNamespace(inventory=(), digest='d' * 64)
    monkeypatch.setattr(service, 'prepare_v2_adoption', proof)
    with pytest.raises(InterruptedError, match='stopped'):
        coordinator.adopt(uuid4(), uuid4())
