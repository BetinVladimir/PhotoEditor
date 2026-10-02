"""Metadata-only user mask scope; no mask bytes or probability qualification."""
from copy import deepcopy
from uuid import uuid4
import json
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition


def test_original_user_mask_scope(db):
    case=seed_original_composition(db)
    body=json.loads(case['Params']);mask=body['mask']['mask'];version=body['base']['asset_version_id']
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,100,'application/json','v1',clock_timestamp(),'document')",(mask['artifact_id'],case['Tenant'],str(uuid4()),mask['sha256']))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,100,'test','ready','v1',clock_timestamp())",(case['Tenant'],uuid4(),case['Project'],version,mask['artifact_id'],mask['sha256']))
    def check(params=body,ref=mask):
        return db.execute('SELECT photo.v25_layer_original_user_mask_scope(%s,%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],case['Project'],case['Snapshot'],case['Actor'],case['Session'],Jsonb(params),ref['artifact_id'],ref['sha256'])).fetchone()[0]
    assert check() is True
    assert check(ref=dict(mask,artifact_id=str(uuid4()))) is False
    assert check(ref=dict(mask,sha256='0'*64)) is False
    protected=deepcopy(body);protected['protected_mask']=mask
    protected['mask']['mask']=dict(artifact_id=str(uuid4()),sha256='c'*64)
    assert check(params=protected) is True
    unrelated=deepcopy(protected);unrelated['protected_mask']=None
    assert check(params=unrelated) is False
    nested=deepcopy(body);nested['mask']=dict(op='invert',operand=dict(op='union',left=body['mask'],right=body['mask']))
    assert check(params=nested) is True
    for column,value,restore in [('object_version','different','v1'),('bucket','other','test'),('bytes',101,100),('state','retired','ready')]:
        db.execute(f'UPDATE photo.v2_draft_uploads SET {column}=%s WHERE artifact_id=%s',(value,mask['artifact_id']))
        assert check() is False,column
        db.execute(f'UPDATE photo.v2_draft_uploads SET {column}=%s WHERE artifact_id=%s',(restore,mask['artifact_id']))
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
    assert check() is False
    assert db.execute("SELECT has_function_privilege('photo_api','photo.v25_layer_original_user_mask_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text)','EXECUTE')").fetchone()[0] is False
