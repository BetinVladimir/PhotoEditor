"""Own SQL selection boundary; synthetic producer, no physical/GPU proof.

Selection holds authority locks for the caller transaction. It must not itself
approve targets, persist decisions, consume quota or create Apply tasks.
"""
from uuid import uuid4

import psycopg
import pytest
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_preview_receipt import seal
from test_v27_retouch_apply_decision_gate import state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def selection_args(data):
    params=data['scope']['retouch_params']
    refs=data['refs']
    return [data['args'][0],params.intent_id,*data['args'][2:4],
        int(params.workflow_revision),data['target'],int(params.target_revision),
        params.plan.sha256,params.candidate.sha256,
        refs['working_image'].artifact_id,refs['working_image'].sha256,
        refs['preview_presentation'].artifact_id,refs['preview_presentation'].sha256]


def select(db,args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        rows=db.execute('SELECT preview_id,receipt FROM photo.lock_semantic_retouch_preview_selection('
            +','.join(['%s']*13)+')',args).fetchall()
        db.execute('RESET ROLE')
        return rows


@pytest.mark.parametrize('alpha',[False,True])
def test_selection_returns_exact_receipt_without_side_effects(database,alpha):
    data=retouch_bundle_fixture(database,admitted=True,with_alpha=alpha)
    receipt=seal(database,data)
    args=selection_args(data)
    before=state(database,args[0])
    expected=[(data['run'],receipt)]
    assert select(database,args)==expected
    assert select(database,args)==expected
    assert state(database,args[0])==before


@pytest.mark.parametrize('field',[
    'tenant','intent','actor','session','workflow_revision','target','target_revision',
    'plan','candidate','render','render_sha','presentation','presentation_sha',
    'alias','missing_render','invalid_render_sha',
])
def test_selection_rejects_substituted_request_without_mutation(database,field):
    data=retouch_bundle_fixture(database,admitted=True)
    receipt=seal(database,data)
    args=selection_args(data)
    tenant=args[0]
    index={'tenant':0,'intent':1,'actor':2,'session':3,'workflow_revision':4,
        'target':5,'target_revision':6,'plan':7,'candidate':8,'render':9,
        'render_sha':10,'presentation':11,'presentation_sha':12}
    if field in index:
        i=index[field]
        args[i]=args[i]+1 if i in (4,6) else '0'*64 if i in (7,8,10,12) else uuid4()
    elif field=='alias': args[11]=args[9]
    elif field=='missing_render': args[9]=None
    else: args[10]='bad'
    before=state(database,tenant)
    with pytest.raises(psycopg.Error) as failure:
        select(database,args)
    assert failure.value.sqlstate in ('22023','40001','42501')
    assert state(database,tenant)==before
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (data['run'],)).fetchone()[0]==receipt


@pytest.mark.parametrize('role',[
    'working_image','pixels','preview_evidence','preview_presentation','before','after','affected','protected',
])
def test_selection_rejects_any_unverified_bundle_member_and_recovers_after_rollback(database,role):
    data=retouch_bundle_fixture(database,admitted=True)
    receipt=seal(database,data)
    args=selection_args(data)
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',
            (data['refs'][role].artifact_id,))
        before=state(database,args[0])
        with pytest.raises(psycopg.errors.SerializationFailure):
            select(database,args)
        assert state(database,args[0])==before
    assert select(database,args)==[(data['run'],receipt)]
