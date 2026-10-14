"""SQL metadata worker compilation proof; authored, NOT RUN."""
import json
from hashlib import sha256
from pathlib import Path
from uuid import uuid4,uuid5
import pytest
from psycopg.types.json import Jsonb


def wire(value):return json.dumps(value,ensure_ascii=False,sort_keys=True,separators=(',',':'))


@pytest.mark.parametrize('mode',['valid','params-policy','copy-gps','extra-field','raster-alias','projection','task'])
def test_metadata_write_plan_matches_frozen_projection(db,mode):
    corpus=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/delivery-metadata-crosslang.json').read_text())
    query='SELECT photo.v30_metadata_write_plan_valid(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        for case in corpus['cases']:
            if case['policy']=='strip':continue
            task=uuid4()
            encoded_id=str(uuid4())
            image={'artifact':{'artifact_id':encoded_id if mode=='raster-alias' else str(uuid4()),'sha256':'c'*64},
                'format':'jpeg','width':4,'height':3,'icc_id':'srgb_v1','icc_sha256':'d'*64,'alpha':False,'metadata_policy':'strip'}
            image_body=wire(image)
            encoded={'artifact_id':encoded_id,'sha256':sha256(image_body.encode()).hexdigest()}
            projection=wire(json.loads(case['frozen_jcs'])['projection'])
            params={'encoded':encoded,'document':{'artifact_id':str(uuid5(task,'delivery:metadata:projection:1')),
                'sha256':sha256(projection.encode()).hexdigest()},'policy':'copyright_only' if case['policy']=='copyright' else 'safe_camera',
                'copy_gps':case['policy']=='approved_exif' and 'gps' in case['approved_fields'],'tool_id':'exiftool_v1'}
            if mode=='params-policy':params['policy']='strip'
            if mode=='copy-gps':params['copy_gps']=not params['copy_gps']
            if mode=='extra-field':params['untrusted']=True
            if mode=='projection':projection=wire({'author':'Substitution'})
            if mode=='task':task=uuid4()
            args=(task,Jsonb(encoded),image_body,case['frozen_jcs'],case['frozen_sha256'],Jsonb(case['binding']),
                Jsonb(case['metadata_source']),case['source_jcs'],case['policy'],case['approved_fields'],projection,wire(params))
            assert db.execute(query,args).fetchone()==(mode=='valid',)


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_metadata_write_plan_validator_private(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_metadata_write_plan_valid(uuid,jsonb,text,text,text,jsonb,jsonb,text,text,text[],text,text)','EXECUTE')",(role,)).fetchone()==(False,)
