"""New Apply mutation receipt gate with real pre-Apply review authority.

Narrow rejection scenarios, not positive atomic Apply completion evidence.
No function doubles; no worker, object store or migrations are run by this module.
"""
import hashlib
import json
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_receipt import receipt_fixture, seal


@pytest.mark.parametrize('mutation',['missing','wrong_hash','wrong_principal','invalid_documents'])
def test_background_cleanup_apply_receipt_gate_preserves_review_and_preview(database,mutation):
    data = receipt_fixture(database)
    args,preview,scope = data[:3]
    receipt = seal(database,data)
    params = json.loads(args[5])
    child,key = uuid4(),str(uuid4())
    request = dict(run_id=str(scope['parent_run']),request=dict(expected_revision=params['workflow_revision'],
        plan_sha=params['plan']['sha256'],target_decisions=[dict(target_id=str(args[4]),decision='approve',
            preview=dict(render=receipt['working_image'],evidence=receipt['presentation']))]))
    raw = json.dumps(request,sort_keys=True,separators=(',', ':'))
    digest = hashlib.sha256(raw.encode()).hexdigest()
    if mutation != 'missing':
        database.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
            (args[0],uuid4() if mutation == 'wrong_principal' else args[2],key,'0'*64 if mutation == 'wrong_hash' else digest))
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    target = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone()
    # All cases must refuse before a child can survive the statement. The last
    # case passes the entry receipt gate but deliberately has no prepared docs.
    expected_error = psycopg.errors.InvalidParameterValue if mutation == 'invalid_documents' else psycopg.errors.SerializationFailure
    with pytest.raises(expected_error):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_semantic_admission')
            database.execute('SELECT photo.commit_semantic_shown_background_cleanup_apply_review(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (args[0],args[1],args[2],args[3],key,int(params['workflow_revision']),raw,uuid4(),'{}',Jsonb([]),child,Jsonb([]))).fetchone()
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(child,)).fetchone()[0] == 0
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == target
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone() == usage
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(preview,)).fetchone()[0] == receipt
    if mutation != 'missing':
        assert database.execute('SELECT response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=%s AND key=%s',(args[0],key)).fetchone() == (None,None,None)
