"""Original depth producer Params bytes through real worker SQL and MinIO."""
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import RelativeDepth
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.runtime.database import TaskDatabase
from wire_runtime_login import worker_login
from pydantic import ValidationError
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.portrait.relight_depth_authority import SQLRelightDepthAuthority
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_lease import relight_lease_fixture
from test_v25_lens_objectstore import lens_objects

@pytest.mark.parametrize('failure',['','corrupt','missing-version'])
def test_original_depth_params_with_exact_version(database,lens_objects,failure):
    client,bucket=lens_objects;retained={}
    class Upload:
        def put_object(self,bucket,key,stream,size,**kwargs):
            if failure=='corrupt':return client.put_object(bucket,key,BytesIO(b'x'*size),size,**kwargs)
            return client.put_object(bucket,key,stream,size,**kwargs)
    def setup(db,tenant,scope):return seed_depth_inputs(db,tenant,scope,retained,objects=(Upload(),bucket))
    def build(pin,scope):
        ref=lambda:dict(artifact_id=str(uuid4()),sha256='d'*64)
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),
            source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),
            depth=scope['depth_ref'],subject=ref(),protected=ref(),calibration=ref(),strength=.7,
            lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    with psycopg.connect(**database) as db:
        args,scope,_=relight_lease_fixture(db,params_builder=build,extra_inputs_factory=setup)
        profile=db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(args[2],)).fetchone()[0]
        refs=[ArtifactRef(artifact_id=row[0],sha256=row[1]) for row in db.execute('SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s',(args[2],)).fetchall()]
    newer=b'newer-invalid-params';client.put_object(bucket,retained['key'],BytesIO(newer),len(newer),content_type='application/json')
    if failure=='missing-version':client.remove_object(bucket,retained['key'],version_id=retained['version'])
    command=SimpleNamespace(tenant_id=args[0],run_id=args[1],task_id=args[2],attempt=1,
        params=ArtifactRef(artifact_id=args[6],sha256=args[7]),inputs=refs)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command,worker_id=args[5],
        fence=1,input_digest=args[9],execution_profile=profile))
    params=ImageRelightParams.model_validate_json(args[8]);depth=RelativeDepth.model_validate_json(retained['body'])
    def unused(*args):pytest.fail('Direct versioned reader must not fall back to generic artifact transport')
    # Attest the isolated runtime identity before any expected negative outcome.
    with worker_login(make_conninfo(**database),args[2]) as runtime_dsn:
        runtime_database=TaskDatabase(runtime_dsn)
        runtime_database.ping()
        authority=SQLRelightDepthAuthority(runtime_database,SimpleNamespace(contract_bytes=unused),objects=client,bucket=bucket)
        if failure:
            from minio.error import S3Error
            with pytest.raises(ValidationError if failure=='corrupt' else S3Error) as caught:authority.verify(ctx,params,None,depth)
            if failure=='corrupt':assert caught.value.errors()[0]['type']=='json_invalid'
        else:
            assert authority.verify(ctx,params,None,depth) is True
            with psycopg.connect(**database) as db:
                db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['root'],))
            with pytest.raises(OperationFailure,match='^RELIGHT_DEPTH_ORIGIN_INVALID$'):authority.verify(ctx,params,None,depth)
