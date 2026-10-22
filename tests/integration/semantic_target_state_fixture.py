"""SQL metadata fixture transition; not an authenticated decision receipt.

Use only owner-controlled rollback fixtures. Never disables a state guard.
"""
def reject_target_with_revision(db, target):
    before = db.execute("""SELECT t.tenant_id,t.project_id,t.intent_id,t.state,t.revision,
        i.workflow_id,i.intent_artifact_id,r.created_by
        FROM photo.semantic_targets t JOIN photo.semantic_intents i
        ON i.tenant_id=t.tenant_id AND i.id=t.intent_id AND i.project_id=t.project_id
        JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id
        JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
        WHERE t.id=%s FOR UPDATE OF t""", (target,)).fetchone()
    assert before is not None and before[3] == 'proposed'
    tenant,project,intent,_,revision,workflow,artifact,actor = before
    batch = db.execute("""INSERT INTO photo.v2_review_batches
        (tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action)
        VALUES(%s,%s,%s,%s,%s,'decide') RETURNING id""",
        (tenant,project,workflow,artifact,actor)).fetchone()[0]
    changed = db.execute("""UPDATE photo.semantic_targets SET state='rejected',batch_id=%s
        WHERE tenant_id=%s AND project_id=%s AND intent_id=%s AND id=%s AND state='proposed'""",
        (batch,tenant,project,intent,target))
    assert changed.rowcount == 1
    after = db.execute("""SELECT state,revision,batch_id,child_run_id,review_id,override_id
        FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s""", (tenant,target)).fetchone()
    assert after == ('rejected',revision+1,batch,None,None,None)
    return after
