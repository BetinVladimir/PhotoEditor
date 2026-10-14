"""Real SQL/original-version IO; synthetic qualification, no image quality claim."""
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.database import TaskDatabase
from photo_worker.v2.relight_qualification_loader import load_relight_qualification
from test_v28_relight_lease import relight_lease_fixture
from test_v25_lens_objectstore import lens_objects

@pytest.mark.parametrize('failure',['','corrupt-report','missing-calibration'])
def test_relight_original_qualification_versions(database,lens_objects,failure):
    client,bucket=lens_objects
    calibration=Calibration(**dict(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.3,scene_log_luma_max=1.,cluster_diameter_max=1.,sharpness_scale=1.,
        clipping_penalty=1.,match_inlier_min=.3,match_error_px_max=5.,identity_similarity_min=.4,
        landmark_drift_max=.2,normalization_scales=[1.]*6,loss_weights=[.4,.1,.1,.1,.2,.1],
        focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.1,
        clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=64,
        depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))
    craw=canonical_bytes(calibration)
    def build(pin,scope):
        ref=lambda:dict(artifact_id=str(uuid4()),sha256='e'*64)
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),
            source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='f'*64),
            strength=.7,depth=ref(),subject=ref(),protected=ref(),calibration=pin['calibration'],
            lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    class Upload:
        def put_object(self,bucket,key,stream,length,**kwargs):
            if failure=='corrupt-report' and not key.startswith('relight-calibration/'):
                return client.put_object(bucket,key,BytesIO(b'x'*length),length,**kwargs)
            return client.put_object(bucket,key,stream,length,**kwargs)
    with psycopg.connect(**database) as db:
        args,scope,pin=relight_lease_fixture(db,qualified=True,objects=(Upload(),bucket),
            calibration_raw=craw,params_builder=build)
        profile=db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(args[2],)).fetchone()[0]
        locations={key:db.execute('SELECT object_key,object_version FROM photo.artifacts WHERE id=%s',(pin[key]['artifact_id'],)).fetchone() for key in ('report','calibration')}
    for key,(object_key,version) in locations.items():
        newer=b'newer-invalid-document'
        client.put_object(bucket,object_key,BytesIO(newer),len(newer),content_type='application/json')
    if failure=='missing-calibration':
        key,version=locations['calibration'];client.remove_object(bucket,key,version_id=version)
    command=SimpleNamespace(operation='image.relight',operation_version='1',tenant_id=args[0],run_id=args[1],
        task_id=args[2],attempt=1,params=ArtifactRef(artifact_id=args[6],sha256=args[7]))
    lease=SimpleNamespace(command=command,fence=1,worker_id=args[5],input_digest=args[9],execution_profile=profile)
    database_port=TaskDatabase(make_conninfo(**database))
    def load():return load_relight_qualification(database_port,lease,args[8].encode(),client,bucket,
        expected_runtime_sha='b'*64,check_active=lambda:None)
    if failure:
        from minio.error import S3Error
        with pytest.raises(ValueError if failure=='corrupt-report' else S3Error):load()
    else:
        report,actual,policy,current=load()
        assert actual==craw and sha256(report).hexdigest()==pin['report']['sha256']
        assert policy['operation']=='image.relight' and current()==pin
        with psycopg.connect(**database) as db:
            db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        with pytest.raises(ValueError):current()
