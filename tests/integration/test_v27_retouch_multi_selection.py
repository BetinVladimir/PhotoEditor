"""Same-review multi-target private decision gate, before child creation."""
from semantic_target_state_fixture import reject_target_with_revision
import json

import psycopg
import pytest
from retouch_multi_selection_fixture import multi_selection_fixture
from test_v27_retouch_apply_decision_gate import lock_decision
from test_v27_retouch_apply_atomic_commit import snapshot
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('alpha',[False,True])
def test_multi_target_decision_binds_two_admitted_receipts_and_replays_without_child(database,alpha):
    bundles,args=multi_selection_fixture(database,with_alpha=alpha)
    decisions=json.loads(args[7])['targets']
    assert len(decisions)==2
    assert len({bundle['run'] for bundle in bundles})==2
    assert len({bundle['task'] for bundle in bundles})==2
    assert len({bundle['result'] for bundle in bundles})==2
    receipts={str(bundle['target']):database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (bundle['run'],)).fetchone()[0] for bundle in bundles}
    for decision in decisions:
        receipt=receipts[decision['target_id']]
        assert decision['preview']==dict(render=receipt['working_image'],evidence=receipt['presentation'])
    before=snapshot(database,args[0])
    owner=lock_decision(database,args)
    assert owner is not None and lock_decision(database,args)==owner
    assert snapshot(database,args[0])==before


@pytest.mark.parametrize('mutation',['target','preview','producer','receipt','pixels','before'])
def test_multi_target_gate_refuses_second_target_loss_without_partial_mutation(database,mutation):
    bundles,args=multi_selection_fixture(database)
    owner=lock_decision(database,args)
    second=bundles[1]
    with database.transaction(force_rollback=True):
        if mutation=='target':
            reject_target_with_revision(database, second['target'])
        elif mutation=='preview':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(second['run'],))
        elif mutation=='producer':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(second['task'],))
        elif mutation=='receipt':
            database.execute('UPDATE photo.runs SET v27_retouch_preview_receipt=NULL WHERE id=%s',(second['run'],))
        else:
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(second['refs'][mutation].artifact_id,))
        before=snapshot(database,args[0])
        with pytest.raises(psycopg.Error) as failure:
            lock_decision(database,args)
        assert failure.value.sqlstate in ('40001','23514')
        assert snapshot(database,args[0])==before
    assert lock_decision(database,args)==owner
