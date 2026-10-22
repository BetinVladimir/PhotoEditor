"""Complete tenant row snapshots for retouch Apply rollback/replay assertions.

Table identifiers are an explicit static allowlist, never caller-controlled SQL.
"""
TABLES = (
    'runs', 'tasks', 'task_results', 'task_attempts', 'semantic_targets',
    'semantic_intents', 'v2_review_batches', 'quota_reservations', 'tenant_usage',
    'idempotency_records', 'run_events', 'outbox', 'v2_task_inputs', 'artifacts',
    'output_uploads', 'backend_documents', 'audit_log', 'tenant_event_cursors',
    'v2_workflows',
)


def tenant_transaction_state(db, tenant):
    return tuple((table, db.execute('SELECT to_jsonb(r) FROM photo.' + table
        + ' r WHERE tenant_id=%s ORDER BY to_jsonb(r)::text', (tenant,)).fetchall())
        for table in TABLES)
