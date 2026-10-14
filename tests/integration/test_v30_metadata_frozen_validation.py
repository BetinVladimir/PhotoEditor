"""Private SQL metadata values validator; authored, NOT RUN."""
import json
from hashlib import sha256
from pathlib import Path
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_frozen_metadata_validator_is_private(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_metadata_frozen_valid(text,text,jsonb,jsonb,text,text,text[])','EXECUTE')",(role,)).fetchone()==(False,)


def test_frozen_metadata_shared_corpus_sql_values(db):
    corpus=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/delivery-metadata-crosslang.json').read_text())
    query='SELECT photo.v30_metadata_frozen_valid(%s,%s,%s,%s,%s,%s,%s)'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        for case in corpus['cases']:
            args=[case['frozen_jcs'],case['frozen_sha256'],Jsonb(case['binding']),Jsonb(case['metadata_source']),case['source_jcs'],case['policy'],case['approved_fields']]
            assert db.execute(query,args).fetchone()==(True,)
            changed=list(args);changed[1]='0'*64
            assert db.execute(query,changed).fetchone()==(False,)
            changed=list(args);changed[-1]=['gps','gps']
            assert db.execute(query,changed).fetchone()==(False,)
            changed=list(args);changed[-1]=['private_serial']
            assert db.execute(query,changed).fetchone()==(False,)
            receipt=json.loads(case['frozen_jcs']);receipt['projection']['author']='Substitution'
            body=json.dumps(receipt,ensure_ascii=False,sort_keys=True,separators=(',',':'))
            changed=list(args);changed[0]=body;changed[1]=sha256(body.encode()).hexdigest()
            assert db.execute(query,changed).fetchone()==(False,)
