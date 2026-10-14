"""Completed scheduler authority with exact versioned manual raster evidence."""
from io import BytesIO
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import json
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,RelativeDepth
from photo_worker.operations.session.contracts import Calibration
from photo_worker.v2.relight_adoption_authority import CompletedRelightAuthority
from photo_worker.v2.relight_adoption_qualification import verify_completed_relight_qualification
from test_v28_relight_depth import seed_depth_inputs
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef
from photo_worker.v2.relight_adoption_manual import verify_relight_adoption_manual
from test_v28_relight_manual import seed_manual_inputs
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task
from test_v25_lens_objectstore import lens_objects


def test_completed_relight_all_dependency_authorities(database,lens_objects,tmp_path):
    client,bucket=lens_objects;retained={};depth_data={};source_data={};locators={};metadata={}
    calibration=Calibration(**dict(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.3,scene_log_luma_max=1.,cluster_diameter_max=1.,sharpness_scale=1.,clipping_penalty=1.,match_inlier_min=.3,match_error_px_max=5.,identity_similarity_min=.4,landmark_drift_max=.2,normalization_scales=[1.]*6,loss_weights=[.4,.1,.1,.1,.2,.1],focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.1,clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=64,depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))
    def source_builder(artifact,original_sha):
        value=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        value['canvas'].update(width=5,height=5,source_sha256=original_sha,space_id='oriented')
        import numpy as np
        stream=BytesIO();np.save(stream,np.full((5,5,3),.4,np.float32),allow_pickle=False);payload=stream.getvalue()
        aid=artifact(sha256(payload).hexdigest(),media='application/octet-stream',role='working_rgb',body=payload)
        value['pixels']=dict(artifact_id=str(aid),sha256=sha256(payload).hexdigest());value['alpha']=None
        source_data['value']=WorkingImage.model_validate_json(canonical_bytes(value))
        return canonical_bytes(value)
    def publish(aid,raw,role):
        key='completed-manual/'+str(aid)
        version=client.put_object(bucket,key,BytesIO(raw),len(raw)).version_id
        return bucket,key,version
    def setup(db,tenant,scope):return scope['source_inputs']+seed_manual_inputs(db,tenant,scope,retained,physical=True,publish=publish)+seed_depth_inputs(db,tenant,scope,depth_data,objects=(client,bucket),physical=True,publish=publish)
    def build(pin,scope):
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(source_data['value'].canvas)),
            subject=scope['subject_ref'],protected=scope['protected_ref'],depth=scope['depth_ref'],calibration=pin['calibration'],strength=.7,lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    with psycopg.connect(**database) as db:
        args,scope,pin=relight_lease_fixture(db,qualified=True,objects=(client,bucket),calibration_raw=canonical_bytes(calibration),params_builder=build,extra_inputs_factory=setup,source_builder=source_builder)
        result,root=complete_relight_task(db,args)
        root_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(root,)).fetchone()[0]
        for aid,digest in db.execute('SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s',(args[2],)):
            media,size,key,version,objbucket=db.execute('SELECT media_type,bytes,object_key,object_version,bucket FROM photo.artifacts WHERE id=%s',(aid,)).fetchone()
            metadata[aid]=(digest,media,size)
            locators[aid]=(objbucket,key,version)
    # Newer invalid objects must never replace original pinned versions.
    for aid in retained['raw_objects']:
        if aid not in locators:continue
        objbucket,key,version=locators[aid]
        client.put_object(objbucket,key,BytesIO(b'newer-invalid'),13)
    class Reader:
        def evidence(self,ref):
            digest,media,size=metadata[ref.artifact_id]
            assert ref.sha256==digest
            return ArtifactEvidence(ref,media,size,None,'input')
        def raw(self,ref,maximum):
            self.evidence(ref)
            objbucket,key,version=locators[ref.artifact_id]
            response=client.get_object(objbucket,key,version_id=version)
            try:return response.read(maximum+1)
            finally:response.close();response.release_conn()
        def read_json(self,ref,maximum):return self.raw(ref,maximum)
        def iter_bytes(self,ref,maximum):yield self.raw(ref,maximum)
    params=ImageRelightParams.model_validate_json(args[8])
    context=SimpleNamespace(attempt=1,fence=1,result_id=result,root=ArtifactRef(root,root_sha))
    subject=MaskRef.model_validate_json(retained['target_raw'])
    guard=MaskRef.model_validate_json(retained['guard_raw'])
    depth=RelativeDepth.model_validate_json(depth_data['body'])
    with psycopg.connect(**database,autocommit=True) as scheduler:
        scheduler.execute('SET ROLE photo_scheduler')
        def qualification(evidence,params,calibration):
            return verify_completed_relight_qualification(scheduler,Reader(),args[0],args[2],context,params,calibration,expected_runtime_sha='b'*64,check_active=lambda:None)
        authority=CompletedRelightAuthority(scheduler,Reader(),args[0],args[2],context,
            scratch_root=tmp_path,max_bytes=100000,verify_qualification=qualification,check_active=lambda:None)
        def verify():return authority(params,source_data['value'],depth,subject,guard,calibration)
        assert verify() is True
        def aggregate(subject_root=retained['root_raw'].decode()):
            return scheduler.execute('SELECT photo.v28_relight_adoption_evidence('+','.join(['%s']*11)+')',
                (args[0],args[2],1,1,result,args[8],depth_data['body'].decode(),retained['target_raw'].decode(),
                 retained['guard_raw'].decode(),subject_root,retained['root_raw'].decode())).fetchone()[0]
        evidence=aggregate()
        assert set(evidence)=={'source','qualification','depth','subject','guard'}
        assert aggregate(None) is None
        from psycopg.types.json import Jsonb
        assert scheduler.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
            (args[0],args[2],result,Jsonb([]),'a'*64)).fetchone()[0] is False
        def commit(proof):
            return scheduler.execute('SELECT photo.adopt_v28_relight('+','.join(['%s']*14)+')',
                (args[0],args[2],1,1,result,Jsonb([]),'a'*64,proof,args[8],depth_data['body'].decode(),
                 retained['target_raw'].decode(),retained['guard_raw'].decode(),retained['root_raw'].decode(),
                 retained['root_raw'].decode())).fetchone()[0]
        assert commit('{}') is False
        signature='photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)'
        for role in ('photo_scheduler','photo_worker','photo_api','public'):
            assert scheduler.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
        mutations=[
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',
             'UPDATE photo.artifacts SET produced_fence=1 WHERE id=%s',retained['pins']['mask_protected'].artifact_id,'MANUAL_STALE'),
            ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',
             'UPDATE photo.tasks SET fencing_token=1 WHERE id=%s',depth_data['task'],'DEPTH_STALE'),
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',
             'UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=%s',pin['policy_id'],'SOURCE_STALE')]
        # A second real connection cannot revoke verified dependencies while
        # the aggregate transaction retains its row locks.
        with scheduler.transaction():
            assert aggregate()==evidence
            for query,identifier in (
                ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',retained['pins']['mask_protected'].artifact_id),
                ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',depth_data['pixels']),
                ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',depth_data['task']),
                ('UPDATE photo.runs SET cancel_requested=true WHERE id=(SELECT run_id FROM photo.tasks WHERE id=%s)',depth_data['task']),
                ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',pin['policy_id']),
            ):
                with psycopg.connect(**database,autocommit=True) as competing:
                    competing.execute("SET lock_timeout='100ms'")
                    with pytest.raises(psycopg.errors.LockNotAvailable):competing.execute(query,(identifier,))
            assert aggregate()==evidence
        for mutate,restore,identifier,reason in mutations:
            with psycopg.connect(**database) as db:db.execute(mutate,(identifier,))
            try:
                with pytest.raises(ValueError,match=reason):verify()
                assert aggregate() is None
            finally:
                with psycopg.connect(**database) as db:db.execute(restore,(identifier,))
            assert verify() is True
    assert not list(tmp_path.iterdir())
