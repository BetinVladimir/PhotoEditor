from uuid import UUID,uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,prepare_v2_adoption,AdoptionInvalid
from photo_worker.v2.render_cache_lookup import RenderCacheEntry
from photo_worker.v2.render_cache_bytes import VerifiedCacheFile
from photo_worker.v2.render_cache_republish import republish_render_cache,plan_render_cache_publication
from test_v26_render_prepare_adoption import case
from test_v26_render_recomposition_adoption import expected_pixels
from test_layer_prepare import scratch


def prepare_case(tmp_path):
    reader,root,inputs=case()
    prepared=prepare_v2_adoption(task_id=uuid4(),operation='render.compose',version='1',root=root,
        reader=reader,params_ref=inputs.recipe,render_inputs=inputs,render_expected=expected_pixels(),
        layer_scratch_root=tmp_path,layer_scratch_bytes=100000,render_max_tile_bytes=4096)
    files=[];declared=set()
    for entry in prepared.inventory:
        ref=ArtifactRef(UUID(entry['artifact_id']),entry['sha256'])
        if any(file.ref==ref for file in files):continue
        body,media,source,role=reader.objects[ref.artifact_id]
        path=tmp_path/(str(ref.artifact_id)+('.json' if media=='application/json' else '.npy'))
        path.write_bytes(body);files.append(VerifiedCacheFile(ref,path,media,role,len(body)))
        if source=='input':declared.add(ref)
    return reader,root,inputs,files,declared,RenderCacheEntry(uuid4(),root,canonical_bytes(list(prepared.inventory)))


def test_republished_actual_typed_closure_passes_independent_render_verification(tmp_path):
    reader,root,inputs,files,declared,candidate=prepare_case(tmp_path)
    original={file.ref:file.path.read_bytes() for file in files}
    old_outputs={identifier for identifier,(_,_,source,_) in reader.objects.items() if source=='output'}
    space=scratch(tmp_path,100000)
    published=[]
    class Artifacts:
        scratch=space
        def stage(self,path,owner,role,media):
            owner.validate_file(path);body=path.read_bytes()
            reference=ContractRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            reader.objects[reference.artifact_id]=(body,media,'output',role);published.append((reference,body,role))
            return reference
        def stage_contract(self,value,role):
            body=canonical_bytes(value)
            with space.reserve(len(body)) as owner:
                path=owner.path('manifest','.json')
                with space.open_writer(path,owner) as writer:writer.write(body)
                return self.stage(path,owner,role,'application/json')
    try:
        result=republish_render_cache(candidate,files,declared,Artifacts())
        assert result.document.artifact_id!=root.artifact_id
        assert result.bytes_written==sum(len(body) for _,body,_ in published)
        for identifier in old_outputs:reader.objects.pop(identifier)
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='render.compose',version='1',
            root=ArtifactRef(result.document.artifact_id,result.document.sha256),reader=reader,
            params_ref=inputs.recipe,render_inputs=inputs,render_expected=expected_pixels(),
            layer_scratch_root=tmp_path,layer_scratch_bytes=100000,render_max_tile_bytes=4096)
        assert prepared.inventory[0]['artifact_id']==str(result.document.artifact_id)
        binary_old=[body for ref,body in original.items() if next(f for f in files if f.ref==ref).media_type=='application/x-npy' and ref not in declared]
        assert sorted(binary_old)==sorted(body for _,body,role in published if role in ('pixels','alpha','mask_pixels'))
        assert not list(space.directory.iterdir())
    finally:space.cleanup()


@pytest.mark.parametrize('damage',['missing','orphan','role','corrupt'])
def test_complete_plan_rejects_bad_closure_before_staging(tmp_path,damage):
    from dataclasses import replace
    _,_,_,files,declared,candidate=prepare_case(tmp_path)
    if damage=='missing':files.pop(0)
    elif damage=='orphan':files.append(replace(files[0],ref=ArtifactRef(uuid4(),'a'*64)))
    elif damage=='role':files[0]=replace(files[0],role='pixels')
    else:files[0].path.write_bytes(b'{}')
    with pytest.raises(AdoptionInvalid):plan_render_cache_publication(candidate,files,declared)


@pytest.mark.parametrize('response',['old_id','zero_id'])
def test_gateway_cannot_return_cached_or_zero_artifact_id(tmp_path,response):
    from uuid import UUID
    _,_,_,files,declared,candidate=prepare_case(tmp_path)
    space=scratch(tmp_path,100000);started=[]
    class Outputs:
        scratch=space
        def stage(self,path,owner,role,media):
            payload=path.read_bytes();digest=sha256(payload).hexdigest()
            old=next(file for file in files if file.ref.sha256==digest)
            return ContractRef(artifact_id=old.ref.artifact_id if response=='old_id' else UUID(int=0),sha256=digest)
        def stage_contract(self,value,role):
            payload=canonical_bytes(value)
            with space.reserve(len(payload)) as owner:
                path=owner.path('manifest','.json')
                with space.open_writer(path,owner) as writer:writer.write(payload)
                return self.stage(path,owner,role,'application/json')
    try:
        with pytest.raises(AdoptionInvalid,match='PUBLICATION_UPLOAD'):
            republish_render_cache(candidate,files,declared,Outputs(),on_staging_start=lambda:started.append(True))
        assert started and not list(space.directory.iterdir())
    finally:space.cleanup()


def test_invalid_complete_plan_never_marks_staging_started(tmp_path):
    _,_,_,files,declared,candidate=prepare_case(tmp_path)
    files.pop(0)
    def forbidden():raise AssertionError('Staging was marked before plan acceptance')
    with pytest.raises(AdoptionInvalid):
        republish_render_cache(candidate,files,declared,object(),on_staging_start=forbidden)
