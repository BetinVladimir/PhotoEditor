"""Private source publication SQL validation. Authored, NOT RUN."""
from copy import deepcopy
from hashlib import sha256
import json
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_source_publication_value_validator_private(db,role):
    signature='photo.v30_metadata_source_publication_valid(text,text,jsonb,jsonb,text,text,text[])'
    assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(False,)


def test_source_publication_external_authority_and_resource_limits(db):
    corpus=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/delivery-metadata-crosslang.json').read_text())
    query='SELECT photo.v30_metadata_source_publication_valid(%s,%s,%s,%s,%s,%s,%s)'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        for case in corpus['cases']:
            scope={name:str(uuid4()) for name in ('tenant_id','project_id','actor_id','session_id')}
            scope.update(binding=case['binding'],original={'artifact_id':str(uuid4()),'sha256':'d'*64},original_bytes=100,original_mime='image/jpeg')
            envelope=dict(schema_version=1,scope=scope,metadata_source=case['metadata_source'],metadata_jcs=case['source_jcs'],
                policy=case['policy'],approved_fields=case['approved_fields'],frozen_jcs=case['frozen_jcs'],frozen_sha256=case['frozen_sha256'])
            raw=json.dumps(envelope,ensure_ascii=False,sort_keys=True,separators=(',',':'))
            args=[raw,sha256(raw.encode()).hexdigest(),Jsonb(scope),Jsonb(case['metadata_source']),case['source_jcs'],case['policy'],case['approved_fields']]
            assert db.execute(query,args).fetchone()==(True,)
            for mode in ('actor','oversized','alias','mime','extra','pin','approval'):
                changed=list(args);current=deepcopy(scope);value=deepcopy(envelope)
                if mode=='actor':current['actor_id']=str(uuid4())
                elif mode=='oversized':current['original_bytes']=2147483649;value['scope']=current
                elif mode=='alias':current['original']['artifact_id']=case['metadata_source']['artifact_id'];value['scope']=current
                elif mode=='mime':current['original_mime']='image/raw';value['scope']=current
                elif mode=='extra':value['unexpected']=True
                elif mode=='approval':changed[-1]=['private_serial']
                body=json.dumps(value,ensure_ascii=False,sort_keys=True,separators=(',',':'))
                changed[0]=body;changed[1]='0'*64 if mode=='pin' else sha256(body.encode()).hexdigest();changed[2]=Jsonb(current)
                assert db.execute(query,changed).fetchone()==(False,),mode
