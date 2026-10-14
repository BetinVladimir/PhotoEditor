"""Real versioned S3 cache-byte/republication proof, not mTLS/SQL/GPU acceptance."""
from io import BytesIO
from uuid import uuid4
from hashlib import sha256
from pathlib import Path
import sys
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,prepare_v2_adoption
from photo_worker.v2.render_cache_store import RenderCacheObjectReader
from photo_worker.v2.render_cache_bytes import verified_render_cache_files
from photo_worker.v2.render_cache_republish import republish_render_cache
from test_v25_lens_objectstore import lens_objects

# Reuse the independently verified typed fixture; this adds real object IO.
unit_path=str(Path(__file__).resolve().parents[1]/'unit')
sys.path.insert(0,unit_path)
try:
    from test_v26_render_cache_republish import prepare_case
    from test_v26_render_recomposition_adoption import expected_pixels
    from test_layer_prepare import scratch
finally:sys.path.remove(unit_path)


@pytest.fixture
def render_cache_objects():
    # Resume only an explicitly identified repository-owned fixture container
    # after a Docker launch observation timeout; never restart on that timeout.
    import os,re,subprocess,shutil,time
    name=os.getenv('PHOTO_TEST_EXISTING_MINIO_NAME')
    if not name:
        yield from lens_objects.__wrapped__()
        return
    if re.fullmatch(r'photoeditor-v25-[0-9a-f]{32}',name) is None:
        raise ValueError('Invalid isolated fixture identity')
    state=subprocess.run(['docker','inspect','--format',
        '{{.State.Status}} {{index .Config.Labels "photoeditor.scope"}}',name],
        capture_output=True,text=True,timeout=10)
    assert state.returncode==0 and state.stdout.strip()=='running development'
    root=Path(__file__).resolve().parents[2]
    folder=root/'.runtime/v25-objectstore'/name.removeprefix('photoeditor-v25-')
    credentials=dict(line.split('=',1) for line in (folder/'minio.env').read_text().splitlines())
    port=subprocess.check_output(['docker','port',name,'9000/tcp'],text=True,timeout=10).strip()
    assert port.startswith('127.0.0.1:')
    from minio import Minio
    from minio.versioningconfig import VersioningConfig,ENABLED
    import urllib3
    client=Minio(port,access_key=credentials['MINIO_ROOT_USER'],secret_key=credentials['MINIO_ROOT_PASSWORD'],
        secure=False,http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=1,read=2),retries=False))
    bucket='cache-recovered-'+uuid4().hex
    try:
        client.make_bucket(bucket);client.set_bucket_versioning(bucket,VersioningConfig(ENABLED))
        yield client,bucket
    finally:
        subprocess.run(['docker','stop','--time','3',name],capture_output=True,text=True,timeout=10)
        deadline=time.monotonic()+5
        while True:
            inventory=subprocess.run(['docker','ps','--all','--filter','name='+name,'--format','{{.Names}}'],
                capture_output=True,text=True,timeout=3)
            if inventory.returncode==0 and name not in inventory.stdout.splitlines():
                shutil.rmtree(folder);break
            if time.monotonic()>deadline:raise RuntimeError('Fixture absence unconfirmed; retain data')
            time.sleep(.1)


@pytest.mark.parametrize('delete_pinned',[False,True])
def test_real_versioned_cache_closure_and_fresh_typed_republication(render_cache_objects,tmp_path,delete_pinned):
    client,bucket=render_cache_objects
    original,root,inputs,files,declared,candidate=prepare_case(tmp_path)
    tenant,task=uuid4(),uuid4();rows=[];versions={};bodies={};published=[]
    for ordinal,file in enumerate(files):
        payload=file.path.read_bytes();key='cached/'+str(file.ref.artifact_id)
        first=client.put_object(bucket,key,BytesIO(payload),len(payload),content_type=file.media_type)
        assert first.version_id and first.version_id!='null'
        versions[file.ref]=(key,first.version_id);bodies[file.ref]=payload
        rows.append((file.ref.artifact_id,file.ref.sha256,len(payload),file.media_type,file.role,
                     bucket,key,first.version_id,ordinal))
        # Latest is wrong, while the admitted immutable version remains correct.
        wrong=b'x'*len(payload)
        replacement=client.put_object(bucket,key,BytesIO(wrong),len(wrong),content_type=file.media_type)
        assert replacement.version_id!=first.version_id
    class PrimaryManifest:
        autocommit=True
        def execute(self,query,args):
            assert 'v26_render_cache_artifacts' in query and args[:4]==(tenant,task,1,1)
            return self
        def fetchall(self):return rows
    reader=RenderCacheObjectReader(PrimaryManifest(),client,tenant_id=tenant,task_id=task,
        attempt=1,fence=1,allowed_bucket=bucket,inputs=inputs,candidate=candidate)
    space=scratch(tmp_path,200000)
    class Outputs:
        scratch=space
        def stage(self,path,owner,role,media):
            owner.validate_file(path);payload=path.read_bytes()
            ref=ContractRef(artifact_id=uuid4(),sha256=sha256(payload).hexdigest())
            key='new-attempt/'+str(ref.artifact_id)
            uploaded=client.put_object(bucket,key,BytesIO(payload),len(payload),content_type=media)
            assert uploaded.version_id and uploaded.version_id!='null'
            # Independently read the uploaded version before typed verification.
            response=client.get_object(bucket,key,version_id=uploaded.version_id)
            try:stored=response.read()
            finally:response.close();response.release_conn()
            assert stored==payload
            original.objects[ref.artifact_id]=(stored,media,'output',role)
            published.append((ref,key,uploaded.version_id,payload,media))
            return ref
        def stage_contract(self,value,role):
            payload=canonical_bytes(value)
            with space.reserve(len(payload)) as owner:
                path=owner.path('manifest','.json')
                with space.open_writer(path,owner) as writer:writer.write(payload)
                return self.stage(path,owner,role,'application/json')
    old_outputs={i for i,(_,_,source,_) in original.objects.items() if source=='output'}
    try:
        if delete_pinned:
            key,version=versions[root];client.remove_object(bucket,key,version_id=version)
            with pytest.raises(Exception):
                with verified_render_cache_files(candidate,reader,space,max_bytes=100000) as verified:
                    republish_render_cache(candidate,verified,declared,Outputs())
            assert not published
        else:
            with verified_render_cache_files(candidate,reader,space,max_bytes=100000) as verified:
                assert all(file.path.read_bytes()==bodies[file.ref] for file in verified)
                result=republish_render_cache(candidate,verified,declared,Outputs())
            assert result.document.artifact_id!=root.artifact_id
            for identifier in old_outputs:original.objects.pop(identifier)
            prepared=prepare_v2_adoption(task_id=task,operation='render.compose',version='1',
                root=ArtifactRef(result.document.artifact_id,result.document.sha256),reader=original,
                params_ref=inputs.recipe,render_inputs=inputs,render_expected=expected_pixels(),
                layer_scratch_root=tmp_path,layer_scratch_bytes=100000,render_max_tile_bytes=4096)
            assert prepared.inventory[0]['artifact_id']==str(result.document.artifact_id)
            old_binary=[bodies[file.ref] for file in files if file.media_type=='application/x-npy' and file.ref not in declared]
            assert sorted(old_binary)==sorted(payload for _,_,_,payload,media in published if media=='application/x-npy')
        assert not list(space.directory.iterdir())
    finally:space.cleanup()
