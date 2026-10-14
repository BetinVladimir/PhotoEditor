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
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef
from photo_worker.v2.relight_adoption_manual import verify_relight_adoption_manual
from test_v28_relight_manual import seed_manual_inputs
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('protected',[False,True])
def test_completed_manual_original_versions_replay(database,lens_objects,tmp_path,protected):
    client,bucket=lens_objects;retained={};source_data={};locators={};metadata={}
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
    def setup(db,tenant,scope):return seed_manual_inputs(db,tenant,scope,retained,physical=True,publish=publish)
    def build(pin,scope):
        return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(source_data['value'].canvas)),
            subject=scope['subject_ref'],protected=scope['protected_ref'],depth=dict(artifact_id=str(uuid4()),sha256='d'*64),calibration=pin['calibration'],strength=.7,lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    with psycopg.connect(**database) as db:
        args,scope,_=relight_lease_fixture(db,qualified=True,objects=(client,bucket),params_builder=build,extra_inputs_factory=setup,source_builder=source_builder)
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
    mask=MaskRef.model_validate_json(retained['guard_raw'] if protected else retained['target_raw'])
    with psycopg.connect(**database,autocommit=True) as scheduler:
        scheduler.execute('SET ROLE photo_scheduler')
        def verify():return verify_relight_adoption_manual(scheduler,Reader(),args[0],args[2],context,params,mask,
            protected=protected,scratch_root=tmp_path,max_bytes=100000,check_active=lambda:None)
        assert verify() is True
        with psycopg.connect(**database) as db:
            db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins']['mask_protected'].artifact_id,))
        with pytest.raises(ValueError,match='RELIGHT_ADOPTION_MANUAL_STALE'):verify()
    assert not list(tmp_path.iterdir())
