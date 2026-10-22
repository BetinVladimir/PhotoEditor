"""Separate accepted reference provenance; synthetic SQL metadata, not photo QA."""
import json
import pytest
from retouch_apply_documents_fixture import prepared_retouch_review_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('dense_warps',[0,32])
def test_retouch_review_keeps_reference_and_target_dependencies_distinct(database,dense_warps):
    args,parent,scope,target=prepared_retouch_review_fixture(database,with_alpha=True,
        dense_warps=dense_warps,reference_dense_warps=dense_warps)
    source=json.loads(scope['source_body'])
    reference=json.loads(scope['reference_body'])
    intent=json.loads(scope['intent_body'])
    transfer=json.loads(scope['transfer_params_body'])
    selected=json.loads(scope['selected_mask_body'])
    candidate_mask=json.loads(scope['candidate_mask_body'])
    assert intent['reference']==transfer['reference']==scope['reference_binding']
    assert transfer['target']!=transfer['reference']
    assert source['canvas']['source_sha256']!=reference['canvas']['source_sha256']
    assert selected['transform_chain']==reference['transform_chain']
    assert candidate_mask['transform_chain']==source['transform_chain']
    for name,image in [('source_protection',reference),('target_protection',source)]:
        protection=json.loads(scope['protection_documents'][name]['body'])
        assert protection['canvas']==image['canvas']
        assert protection['transform_chain']==image['transform_chain']
    source_fields={s['inverse_field']['artifact_id'] for s in source['transform_chain']['steps']}
    reference_fields={s['inverse_field']['artifact_id'] for s in reference['transform_chain']['steps']}
    assert len(source_fields)==len(reference_fields)==dense_warps
    assert source_fields.isdisjoint(reference_fields)
    upstream={str(ref[0]) for ref in scope['reference_inputs']}
    assert reference_fields<=upstream and reference['pixels']['artifact_id'] in upstream
    assert reference['alpha']['artifact_id'] in upstream
    row=database.execute('SELECT v.master_artifact_id,v.content_hash,a.project_id,v.review_state FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=%s AND v.id=%s',
        (args[0],intent['reference']['asset_version_id'])).fetchone()
    assert str(row[0])==intent['reference']['source']['artifact_id']
    assert row[1]==intent['reference']['source']['sha256'] and row[2]==scope['project'] and row[3]=='accepted'
    proposal=json.loads(args[5])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        base=(*args[:4],int(proposal['workflow_revision']),target,int(proposal['target_revision']),
            scope['version'],proposal['candidate']['artifact_id'],proposal['candidate']['sha256'])
        reference_pin=intent['reference']['source']
        selected=database.execute('SELECT * FROM photo.semantic_approved_image_object('+','.join(['%s']*13)+')',
            (*base,True,reference_pin['artifact_id'],reference_pin['sha256'])).fetchone()
        assert selected is not None and str(selected[1])==reference_pin['artifact_id']
        assert database.execute('SELECT * FROM photo.semantic_approved_image_object('+','.join(['%s']*13)+')',
            (*base,True,proposal['source']['source']['artifact_id'],proposal['source']['source']['sha256'])).fetchone() is None
        for step in range(dense_warps):
            binary=database.execute('SELECT * FROM photo.semantic_approved_image_binary('+','.join(['%s']*16)+')',
                (*base,True,reference_pin['artifact_id'],reference_pin['sha256'],scope['reference_body'].decode(),'inverse_field',step)).fetchone()
            expected=reference['transform_chain']['steps'][step]['inverse_field']
            assert binary is not None and str(binary[1])==expected['artifact_id'] and binary[2]==expected['sha256']
        database.execute('RESET ROLE')


def test_maximum_separate_reference_survives_preview_apply_claim_and_io(database):
    from psycopg.types.json import Jsonb
    from test_v27_shown_retouch_live_io import claimed_fixture,gateway,heartbeat
    x=claimed_fixture(database,dense_warps=32,reference_dense_warps=32,with_alpha=True)
    documents=x['plan']['documents']
    source,reference=(json.loads(documents[k]) for k in ('source','reference'))
    assert source!=reference
    fields=[s['inverse_field'] for image in (source,reference) for s in image['transform_chain']['steps']]
    assert len(fields)==64 and len({r['artifact_id'] for r in fields})==64
    entries=x['plan']['inventory']['entries']
    # Exact semantic maximum: 22 distinct fixed refs, two alpha, 64 fields.
    assert len(entries)==88
    assert len(x['command']['inputs'])==89  # Manifest + direct roots.
    assert x['command']['params']==dict(artifact_id=x['plan']['params_artifact_id'],sha256=x['plan']['params_sha'])
    persisted=database.execute('SELECT artifact_id,sha256,contract,ordinal FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s ORDER BY ordinal',
        (x['tenant'],x['task'])).fetchall()
    assert len(persisted)==90
    assert [(str(a),sha,contract,ordinal) for a,sha,contract,ordinal in persisted[:88]]==[
        (e['ref']['artifact_id'],e['ref']['sha256'],e['contract'],i) for i,e in enumerate(entries)]
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.semantic_shown_retouch_closure_ready(%s,%s)',
            (x['task'],Jsonb(json.loads(x['plan']['params_body'])))).fetchone()[0] is True
        database.execute('RESET ROLE')
    for ref in fields+[source['alpha'],reference['alpha']]:
        row=gateway(database,x,ref['artifact_id'])
        assert row is not None and row[4]==ref['sha256']
    assert heartbeat(database,x)[0] is True
    database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',
        (reference['transform_chain']['steps'][-1]['inverse_field']['artifact_id'],))
    assert gateway(database,x,reference['pixels']['artifact_id']) is None
    assert heartbeat(database,x)==(False,None,True)


@pytest.mark.parametrize('source_steps,reference_steps',[(33,0),(0,33)])
def test_semantic_image_limit_refuses_oversize_chain_before_activation(database,source_steps,reference_steps):
    import psycopg
    from retouch_preview_admission_fixture import prepared_retouch_preview_fixture,activate_retouch_preview_fixture
    prepared=prepared_retouch_preview_fixture(database,dense_warps=source_steps,
        reference_dense_warps=reference_steps)
    before=database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',
        (prepared['run'],)).fetchone()
    with pytest.raises(psycopg.Error):
        with database.transaction():
            activate_retouch_preview_fixture(database,prepared)
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',
        (prepared['run'],)).fetchone()==before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE id=%s',
        (prepared['task'],)).fetchone()==(0,)
