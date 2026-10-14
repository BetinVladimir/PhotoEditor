from io import BytesIO
from hashlib import sha256
from uuid import uuid4
from dataclasses import replace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
from photo_worker.v2.render_cache_lookup import RenderCacheEntry
from photo_worker.v2.render_cache_store import RenderCacheObjectReader
from photo_worker.v2.render_cache_bytes import verified_render_cache_files
from test_v26_render_cache_identity import values
from test_layer_prepare import scratch


def setup_case():
    tenant,recipe,profile,parents,pin=values()
    inputs=RenderAdoptionInputs(recipe,canonical_bytes(profile),parents,pin)
    import numpy as np
    npy=BytesIO();np.save(npy,np.array([-.2,4,.8],dtype=np.float32),allow_pickle=False)
    bodies=[b'{"schema_version":1}',npy.getvalue()];refs=[ArtifactRef(uuid4(),sha256(body).hexdigest()) for body in bodies]
    inventory=[dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256,purpose='a'*64,ordinal=i) for i,ref in enumerate(refs)]
    candidate=RenderCacheEntry(uuid4(),refs[0],canonical_bytes(inventory))
    rows=[(ref.artifact_id,ref.sha256,len(body),'application/json' if i==0 else 'application/x-npy',
           'document' if i==0 else 'pixels','cache','object-'+str(i),'version-'+str(i),i)
          for i,(ref,body) in enumerate(zip(refs,bodies))]
    class DB:
        autocommit=True
        def execute(self,query,args):
            assert 'v26_render_cache_artifacts' in query and args[:4]==(tenant,task,1,1)
            return self
        def fetchall(self):return rows
    task=uuid4()
    class Response(BytesIO):
        def close(self):
            if not self.closed:objects.closed+=1
            super().close()
        def release_conn(self):objects.released+=1
    class Objects:
        opened=closed=released=0
        def get_object(self,bucket,key,*,version_id):
            i=int(key.split('-')[-1]);assert bucket=='cache' and version_id=='version-'+str(i)
            self.opened+=1;return Response(bodies[i])
    objects=Objects()
    reader=RenderCacheObjectReader(DB(),objects,tenant_id=tenant,task_id=task,attempt=1,fence=1,
                                  allowed_bucket='cache',inputs=inputs,candidate=candidate)
    return candidate,reader,objects,bodies,rows


def test_all_actual_bytes_are_pinned_hash_verified_and_owned_until_exit(tmp_path):
    candidate,reader,objects,bodies,_=setup_case();space=scratch(tmp_path,10000)
    try:
        with verified_render_cache_files(candidate,reader,space,max_bytes=10000) as files:
            assert [file.path.read_bytes() for file in files]==bodies
            assert objects.opened==objects.closed==objects.released==2
            assert all(file.path.exists() for file in files)
        assert not list(space.directory.iterdir())
        with pytest.raises(AdoptionInvalid):reader.evidence(ArtifactRef(uuid4(),'a'*64))
        with pytest.raises(AdoptionInvalid):reader.staged_outputs()
    finally:space.cleanup()


@pytest.mark.parametrize('failure',['hash','short','extra','noncanonical','quota','cancel','consumer'])
def test_failed_reuse_read_never_leaks_scratch_or_object_streams(tmp_path,failure):
    candidate,reader,objects,bodies,rows=setup_case();space=scratch(tmp_path,10000)
    if failure=='hash':bodies[1]=bodies[1][:-1]+b'x'
    elif failure=='short':bodies[1]=bodies[1][:-1]
    elif failure=='extra':bodies[1]+=b'x'
    elif failure=='noncanonical':
        bodies[0]=b'{ "schema_version":1}'
        ref=ArtifactRef(candidate.root.artifact_id,sha256(bodies[0]).hexdigest())
        from photo_worker.contracts.base import parse_json
        inventory=parse_json(candidate.inventory_jcs);inventory[0]['sha256']=ref.sha256
        candidate=replace(candidate,root=ref,inventory_jcs=canonical_bytes(inventory))
        old=reader._locations.pop(next(iter(reader._locations)))
        from photo_worker.v2.adoption_store import _Location
        evidence=replace(old.evidence,ref=ref,bytes=len(bodies[0]))
        reader._locations[ref]=_Location(old.bucket,old.key,old.version,evidence)
    def active():
        if failure=='cancel' and objects.opened:raise RuntimeError('cancelled')
    try:
        with pytest.raises(Exception):
            with verified_render_cache_files(candidate,reader,space,max_bytes=1 if failure=='quota' else 10000,check_active=active):
                if failure=='consumer':raise RuntimeError('consumer')
        assert objects.opened==objects.closed==objects.released
        assert not list(space.directory.iterdir())
    finally:space.cleanup()


def test_canonical_document_above_one_megabyte_uses_declared_eight_megabyte_limit(tmp_path):
    candidate,reader,objects,bodies,_=setup_case()
    bodies[0]=canonical_bytes({'data':'x'*1100000})
    ref=ArtifactRef(candidate.root.artifact_id,sha256(bodies[0]).hexdigest())
    from photo_worker.contracts.base import parse_json
    from photo_worker.v2.adoption_store import _Location
    inventory=parse_json(candidate.inventory_jcs);inventory[0]['sha256']=ref.sha256
    old=reader._locations.pop(candidate.root)
    reader._locations[ref]=_Location(old.bucket,old.key,old.version,
        replace(old.evidence,ref=ref,bytes=len(bodies[0])))
    candidate=replace(candidate,root=ref,inventory_jcs=canonical_bytes(inventory))
    space=scratch(tmp_path,2000000)
    try:
        with verified_render_cache_files(candidate,reader,space,max_bytes=2000000) as files:
            assert files[0].path.read_bytes()==bodies[0]
        assert objects.opened==objects.closed==objects.released==2
        assert not list(space.directory.iterdir())
    finally:space.cleanup()
