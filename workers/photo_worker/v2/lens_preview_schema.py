"""Read-only, exact-ABI gate before enabling lens preview maintenance."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.next_v25_lens_preview_tenant(uuid)',
    'photo.pending_v25_lens_preview_lifecycle(uuid,uuid)',
    'photo.maintain_v25_lens_preview(uuid,uuid)',
    'photo.pending_v25_lens_preview_receipts(uuid,uuid)',
    'photo.pending_v25_lens_preview_completion(uuid,uuid)',
    'photo.v25_lens_preview_context(uuid,uuid,text)',
    'photo.v25_lens_preview_result(uuid,uuid,text)',
    'photo.v25_lens_preview_evidence(uuid,uuid,text,uuid,integer,bigint,uuid)',
    'photo.record_v25_lens_preview_receipt(uuid,uuid,text,uuid,integer,bigint,text,jsonb)',
    'photo.reject_v25_lens_preview(uuid,uuid,text,uuid,integer,bigint,text)',
    'photo.finalize_successful_v25_lens_preview(uuid,uuid)',
)


def require_lens_preview_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Lens preview schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Lens preview adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Lens preview schema unavailable: '+signature)
