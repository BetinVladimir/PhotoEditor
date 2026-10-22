"""Multi-target publisher groundwork; no claim of multi-target admission yet."""
from copy import deepcopy
import json
from uuid import uuid4

import pytest
from retouch_copy_apply_fixture import publish_retouch_decision_bundles
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_apply_atomic_commit import snapshot
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mode',['empty','duplicate','foreign_review','target_mismatch'])
def test_multi_decision_rejects_invalid_whole_scope_before_receipt_or_publication(database,mode):
    first=retouch_bundle_fixture(database,admitted=True)
    tenant=first['args'][0]
    foreign=None
    if mode=='empty': bundles=[]
    elif mode=='duplicate': bundles=[first,first]
    elif mode=='foreign_review':
        policies={operation:{key:first['scope'][scope_key][key] for key in ('policy_id','issuer_id','policy_sha')}
            for operation,scope_key in (
                ('semantic.transfer','qualification'),
                ('semantic.preview_retouch_region','retouch_preview_qualification'))}
        foreign=retouch_bundle_fixture(database,admitted=True,qualification_policies=policies)
        assert foreign['args'][0] != tenant
        for operation,scope_key in (
                ('semantic.transfer','qualification'),
                ('semantic.preview_retouch_region','retouch_preview_qualification')):
            own_pin=first['scope'][scope_key];other_pin=foreign['scope'][scope_key]
            assert {key:other_pin[key] for key in policies[operation]} == policies[operation]
            assert own_pin['qualification_id'] != other_pin['qualification_id']
            assert own_pin['report']['artifact_id'] != other_pin['report']['artifact_id']
            assert database.execute('SELECT tenant_id,project_id,trusted_import,status FROM photo.model_qualifications WHERE id=%s',
                (other_pin['qualification_id'],)).fetchone() == (foreign['args'][0],foreign['scope']['project'],True,'passed')
        assert database.execute('SELECT count(*) FROM photo.tenants WHERE id=ANY(%s)',
            ([tenant,foreign['args'][0]],)).fetchone() == (2,)
        for current in (first,foreign):
            assert database.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',
                (current['task'],)).fetchone() == ('succeeded',current['result'])
        bundles=[first,foreign]
    else:
        changed=deepcopy(first)
        changed['target']=uuid4()
        bundles=[changed]
    before=snapshot(database,tenant)
    foreign_before=snapshot(database,foreign['args'][0]) if foreign else None
    with pytest.raises(ValueError):
        publish_retouch_decision_bundles(database,bundles)
    assert snapshot(database,tenant)==before
    if foreign:
        assert snapshot(database,foreign['args'][0])==foreign_before


def test_common_publisher_keeps_single_target_real_receipt_and_original_bytes(database):
    bundle=retouch_bundle_fixture(database,admitted=True)
    request,artifact,body=publish_retouch_decision_bundles(database,[bundle])
    parsed=json.loads(body)
    receipt=database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (bundle['run'],)).fetchone()[0]
    assert parsed['targets'][0]['preview']==dict(render=receipt['working_image'],evidence=receipt['presentation'])
    assert json.loads(request)['request']['target_decisions'][0]['preview']==parsed['targets'][0]['preview']
    assert database.execute('SELECT state FROM photo.backend_documents WHERE tenant_id=%s AND artifact_id=%s',
        (bundle['args'][0],artifact)).fetchone()==('ready',)
