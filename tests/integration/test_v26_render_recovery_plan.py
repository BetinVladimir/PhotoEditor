"""Planner evidence with controlled history; not a production throughput benchmark."""
from uuid import UUID

from test_v26_render_recovery_backoff import stage_incomplete


def test_recovery_uses_pending_index_in_terminal_history(db):
    tenant,batch=stage_incomplete(db)
    db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) SELECT tenant_id,'bulk','completed',input_snapshot_id,config_snapshot,config_hash,created_by FROM photo.runs CROSS JOIN generate_series(1,2000) n WHERE id=%s",(batch,))
    db.execute('ANALYZE photo.runs')
    body=db.execute("SELECT prosrc FROM pg_proc WHERE oid='photo.next_v26_render_recovery(uuid)'::regprocedure").fetchone()[0]
    # Explain the exact stored function statement under its SECURITY DEFINER role.
    statement=body.replace('p_after','NULL::uuid')
    db.execute('SET LOCAL ROLE photo_fn_owner')
    plan=db.execute('EXPLAIN (FORMAT JSON, COSTS OFF) '+statement).fetchone()[0]
    db.execute('RESET ROLE')
    def nodes(node):
        yield node
        for child in node.get('Plans',[]):yield from nodes(child)
    indexes={node.get('Index Name') for node in nodes(plan[0]['Plan'])}
    assert 'runs_v26_recovery_pending' in indexes,plan
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT tenant_id,batch_id FROM photo.next_v26_render_recovery(NULL)').fetchall()==[(UUID(str(tenant)),batch)]
    db.execute('RESET ROLE')
