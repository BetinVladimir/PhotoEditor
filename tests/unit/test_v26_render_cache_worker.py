from io import BytesIO
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from dataclasses import asdict
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes,parse_json
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid,prepare_v2_adoption
from photo_worker.v2.render_cache_worker import WorkerRenderCache
from test_v26_render_cache_republish import prepare_case
from test_v26_render_recomposition_adoption import expected_pixels
from test_layer_prepare import scratch


@pytest.mark.parametrize('mode',['hit','miss','corrupt','quota','authority_lost','authority_lost_on_miss','missing_version','upload_failure','bad_role'])
def test_worker_cache_uses_gateway_and_fresh_publication_with_real_typed_bytes(tmp_path,mode):
    reader,root,context,files,declared,candidate=prepare_case(tmp_path)
    space=scratch(tmp_path,200000)
    reads=[];uploads=[];old_outputs={identifier for identifier,(_,_,source,_) in reader.objects.items() if source=='output'}
    class Stream(BytesIO):
        @property
        def size(self):return len(self.getvalue())
    class Gateway:
        def open_stream(self,ref):
            reads.append(ref);body=reader.objects[ref.artifact_id][0]
            if mode=='missing_version' and ref.artifact_id==root.artifact_id:
                from photo_worker.runtime.gateway import GatewayError
                raise GatewayError('ARTIFACT_MISSING')
            if mode=='corrupt' and ref.artifact_id==root.artifact_id:body=body[:-1]+b'x'
            return Stream(body)
    class Artifacts:
        scratch=space;gateway=Gateway()
        def stream(self,ref):return self.gateway.open_stream(ref)
        def _allows(self,ref):return ArtifactRef(ref.artifact_id,ref.sha256) in declared
        def stage(self,path,owner,role,media):
            owner.validate_file(path);body=path.read_bytes()
            ref=ContractRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            reader.objects[ref.artifact_id]=(body,media,'output',role);uploads.append(ref)
            if mode=='upload_failure':raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_UPLOAD')
            return ref
        def stage_contract(self,value,role):
            body=canonical_bytes(value)
            with space.reserve(len(body)) as owner:
                path=owner.path('manifest','.json')
                with space.open_writer(path,owner) as writer:writer.write(body)
                return self.stage(path,owner,role,'application/json')
    command=SimpleNamespace(tenant_id=uuid4(),params=ContractRef(artifact_id=context.recipe.artifact_id,sha256=context.recipe.sha256))
    lease=SimpleNamespace(command=command,execution_profile=context.execution_profile())
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    class Database:
        def render_cache_context(self,actual,raw):
            assert actual is lease and sha256(raw).hexdigest()==context.recipe.sha256
            if mode=='authority_lost':return None
            return (context.recipe.artifact_id,context.recipe.sha256,context.execution_profile(),
                    [{'artifact_id':str(ref.artifact_id),'sha256':ref.sha256} for ref in context.parent_roots],asdict(context.qualification_pin))
        def quarantine_render_cache(self,*args):
            assert args[-1]==candidate.result_id
            return True
        def admit_render_recipe(self,actual,raw):return mode!='authority_lost_on_miss'
        def lookup_render_cache(self,actual,raw,key,body):
            assert actual is lease and parse_json(body)['tenant_id']==str(command.tenant_id)
            if mode in ('miss','authority_lost_on_miss'):return None
            return (candidate.result_id,{'artifact_id':str(root.artifact_id),'sha256':root.sha256},parse_json(candidate.inventory_jcs))
        def render_cache_artifacts(self,*args):
            return [(f.ref.artifact_id,f.ref.sha256,f.bytes,f.media_type,'pixels' if mode=='bad_role' and f.ref==root else f.role,'never-used','never-used','version-1',i) for i,f in enumerate(files)]
    try:
        service=WorkerRenderCache(Database(),max_bytes=1 if mode=='quota' else 100000)
        if mode in ('authority_lost','authority_lost_on_miss','upload_failure'):
            with pytest.raises(AdoptionInvalid):service.try_publish(ctx,Artifacts(),context.qualification_pin)
            assert len(uploads)==1 if mode=='upload_failure' else not uploads
        else:
            result=service.try_publish(ctx,Artifacts(),context.qualification_pin)
            if mode in ('miss','quota','corrupt','missing_version','bad_role'):assert result is None and not uploads
            else:
                assert result.document.artifact_id!=root.artifact_id and len(uploads)>1
                for identifier in old_outputs:reader.objects.pop(identifier)
                prepared=prepare_v2_adoption(task_id=uuid4(),operation='render.compose',version='1',
                    root=ArtifactRef(result.document.artifact_id,result.document.sha256),reader=reader,
                    params_ref=context.recipe,render_inputs=context,render_expected=expected_pixels(),
                    layer_scratch_root=tmp_path,layer_scratch_bytes=100000,render_max_tile_bytes=4096)
                assert prepared.inventory[0]['artifact_id']==str(result.document.artifact_id)
        assert not list(space.directory.iterdir())
    finally:space.cleanup()
