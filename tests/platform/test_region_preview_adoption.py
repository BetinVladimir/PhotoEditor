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
            assert 'v26_region_adoption_params' in query
            return SimpleNamespace(fetchone=lambda:(params_ref.artifact_id,params_ref.sha256) if row_present else None)
    class Write:
        def transaction(self):raise AssertionError('No write transaction before proof')
    coordinator=service.AdoptionCoordinator(Read(),Write(),object(),'private',region_target_icc_sha=icc)
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
