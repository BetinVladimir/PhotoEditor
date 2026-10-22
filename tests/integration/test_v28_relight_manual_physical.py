"""Live worker SQL plus real versioned MinIO documents and raster replay.

Transport maps are fixture-seeded; this is not public gateway/closure acceptance.
"""
from io import BytesIO
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
from pathlib import Path
import json
import psycopg
from psycopg.conninfo import make_conninfo
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.runtime.database import TaskDatabase
from wire_runtime_login import worker_login
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.portrait.relight_manual_authority import SQLRelightManualOrigin,RelightManualMaskAuthority
from test_v28_relight_manual import seed_manual_inputs
from test_v28_relight_lease import relight_lease_fixture
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('failure',['','corrupt-root','missing-params','wrong-raster'])
def test_live_manual_origin_original_versions_and_vector_replay(database,lens_objects,tmp_path,failure):
    client,bucket=lens_objects;retained={};locators={}
    def publish(aid,raw,role):
        key='manual-relight/'+str(aid)
        stored=b'x'*len(raw) if failure=='corrupt-root' and role=='rasterized_mask' else raw
        version=client.put_object(bucket,key,BytesIO(stored),len(stored)).version_id
        locators[aid]=(key,version,len(raw),sha256(raw).hexdigest())
        return bucket,key,version
    def setup(db,tenant,scope):
        return seed_manual_inputs(db,tenant,scope,retained,physical=True,publish=publish)
    def build(pin,scope):
        ref=lambda:dict(artifact_id=str(uuid4()),sha256='d'*64)
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),
            subject=scope['subject_ref'],protected=scope['protected_ref'],depth=ref(),calibration=ref(),strength=.7,lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    with psycopg.connect(**database) as db:
        args,scope,_=relight_lease_fixture(db,params_builder=build,extra_inputs_factory=setup)
        profile=db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(args[2],)).fetchone()[0]
        refs=[ArtifactRef(artifact_id=r[0],sha256=r[1]) for r in db.execute('SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s',(args[2],))]
    for aid,(key,version,_,_) in locators.items():client.put_object(bucket,key,BytesIO(b'newer-invalid'),13)
    if failure=='missing-params':
        key,version,_,_=locators[retained['params']];client.remove_object(bucket,key,version_id=version)
    params=ImageRelightParams.model_validate_json(args[8])
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(tenant_id=args[0],run_id=args[1],task_id=args[2],attempt=1,params=ArtifactRef(artifact_id=args[6],sha256=args[7]),inputs=refs),worker_id=args[5],fence=1,input_digest=args[9],execution_profile=profile))
    source_raw=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    source_raw['canvas']=retained['canvas'].model_dump(mode='json')
    source=WorkingImage.model_validate_json(canonical_bytes(source_raw))
    class Artifacts:
        def contract_bytes(self,ref,model):
            assert ref in refs
            key,version,size,digest=locators[ref.artifact_id]
            response=client.get_object(bucket,key,version_id=version)
            try:raw=response.read(size+1)
            finally:response.close();response.release_conn()
            if len(raw)!=size or sha256(raw).hexdigest()!=ref.sha256:raise ValueError('PINNED_BYTES_MISMATCH')
            return raw
        def contract(self,ref,model):return model.model_validate_json(self.contract_bytes(ref,model))
        @contextmanager
        def input(self,ref,suffix):
            raw=self.contract_bytes(ref,None)
            if failure=='wrong-raster' and ref==retained['pins']['mask_target']:
                import numpy as np
                array=np.load(BytesIO(raw),allow_pickle=False).copy();array[0,0]=1-array[0,0]
                stream=BytesIO();np.save(stream,array,allow_pickle=False);raw=stream.getvalue()
            path=tmp_path/(str(ref.artifact_id)+suffix);path.write_bytes(raw)
            yield SimpleNamespace(path=path)
    # Attest the isolated runtime identity before any expected negative outcome.
    with worker_login(make_conninfo(**database),args[2]) as runtime_dsn:
        artifacts=Artifacts();database_reader=TaskDatabase(runtime_dsn)
        database_reader.ping()
        authority=RelightManualMaskAuthority(artifacts,SQLRelightManualOrigin(database_reader,artifacts))
        guard=MaskRef.model_validate_json(retained['guard_raw'])
        if failure:
            from minio.error import S3Error
            expected=S3Error if failure=='missing-params' else ValueError
            match=None if failure=='missing-params' else ('RASTER_VECTOR_MISMATCH' if failure=='wrong-raster' else 'PINNED_BYTES_MISMATCH')
            with pytest.raises(expected,match=match):
                authority.verify(ctx,params,source,guard,protected=True)
        else:
            assert authority.verify(ctx,params,source,guard,protected=True) is True
            target=MaskRef.model_validate_json(retained['target_raw'])
            assert authority.verify(ctx,params,source,target,protected=False) is True
            with psycopg.connect(**database) as db:db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins']['mask_protected'].artifact_id,))
            with pytest.raises(OperationFailure,match='^RELIGHT_MANUAL_ORIGIN_INVALID$'):authority.verify(ctx,params,source,guard,protected=True)
