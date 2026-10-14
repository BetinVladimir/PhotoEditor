"""Completed qualification original bytes, synthetic trusted policy/report fixture."""
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from minio.error import S3Error
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.relight_adoption_qualification import verify_completed_relight_qualification
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('missing',[False,True])
def test_completed_qualification_original_minio_versions(database,lens_objects,missing):
    client,bucket=lens_objects
    calibration=Calibration(**dict(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.3,scene_log_luma_max=1.,cluster_diameter_max=1.,sharpness_scale=1.,clipping_penalty=1.,match_inlier_min=.3,match_error_px_max=5.,identity_similarity_min=.4,landmark_drift_max=.2,normalization_scales=[1.]*6,loss_weights=[.4,.1,.1,.1,.2,.1],focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.1,clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=64,depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))
    def build(pin,scope):
        ref=lambda:dict(artifact_id=str(uuid4()),sha256='d'*64)
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),
            depth=ref(),subject=ref(),protected=ref(),calibration=pin['calibration'],strength=.7,lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    with psycopg.connect(**database) as db:
        args,scope,pin=relight_lease_fixture(db,qualified=True,objects=(client,bucket),calibration_raw=canonical_bytes(calibration),params_builder=build)
        result,_=complete_relight_task(db,args)
        catalog={}
        for label in ('report','calibration'):
            aid=pin[label]['artifact_id']
            catalog[aid]=db.execute('SELECT bucket,object_key,object_version,bytes,media_type FROM photo.artifacts WHERE id=%s',(aid,)).fetchone()
    for objbucket,key,version,size,media in catalog.values():
        client.put_object(objbucket,key,BytesIO(b'newer-invalid'),13)
    if missing:
        objbucket,key,version,*_=catalog[pin['report']['artifact_id']]
        client.remove_object(objbucket,key,version_id=version)
    class Reader:
        def evidence(self,ref):
            objbucket,key,version,size,media=catalog[str(ref.artifact_id)]
            return ArtifactEvidence(ref,media,size,None,'input')
        def read_json(self,ref,maximum):
            objbucket,key,version,*_=catalog[str(ref.artifact_id)]
            response=client.get_object(objbucket,key,version_id=version)
            try:return response.read(maximum+1)
            finally:response.close();response.release_conn()
    params=ImageRelightParams.model_validate_json(args[8])
    with psycopg.connect(**database,autocommit=True) as scheduler:
        scheduler.execute('SET ROLE photo_scheduler')
        def verify():return verify_completed_relight_qualification(scheduler,Reader(),args[0],args[2],SimpleNamespace(attempt=1,fence=1,result_id=result),params,calibration,expected_runtime_sha='b'*64,check_active=lambda:None)
        if missing:
            with pytest.raises(S3Error):verify()
        else:
            assert verify() is True
            with psycopg.connect(**database) as db:
                db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
            with pytest.raises(ValueError,match='QUALIFICATION_STALE'):verify()
