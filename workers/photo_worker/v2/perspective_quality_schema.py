"""Read-only, exact-ABI gate before enabling perspective preview maintenance."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.next_v29_perspective_quality_tenant(uuid)',
    'photo.pending_v29_perspective_quality_lifecycle(uuid,uuid)',
    'photo.maintain_v29_perspective_quality(uuid,uuid)',
    'photo.pending_v29_perspective_quality_receipts(uuid,uuid)',
    'photo.pending_v29_perspective_quality_completion(uuid,uuid)',
    'photo.v29_perspective_quality_context(uuid,uuid)',
    'photo.v29_perspective_quality_result(uuid,uuid)',
    'photo.v29_perspective_quality_evidence(uuid,uuid,uuid,integer,bigint,uuid)',
    'photo.record_v29_perspective_quality_receipt(uuid,uuid,uuid,integer,bigint,text,jsonb)',
    'photo.reject_v29_perspective_quality(uuid,uuid,uuid,integer,bigint,text)',
    'photo.finalize_successful_v29_perspective_quality(uuid,uuid)',
)


def require_perspective_quality_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Perspective quality schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Perspective quality adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Perspective quality schema unavailable: '+signature)
