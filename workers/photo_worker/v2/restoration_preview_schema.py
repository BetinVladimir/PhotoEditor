"""Exact scheduler ABI gate for the V28 preview receipt sweep."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.next_v28_restoration_preview_receipt_tenant(uuid)',
    'photo.pending_v28_restoration_preview_receipts(uuid,uuid)',
    'photo.pending_v28_restoration_preview_completion(uuid,uuid)',
    'photo.v28_restoration_preview_context(uuid,uuid,text,text)',
    'photo.v28_restoration_preview_result(uuid,uuid,text,text)',
    'photo.v28_restoration_preview_evidence(uuid,uuid,text,text,uuid,integer,bigint,uuid)',
    'photo.record_v28_restoration_preview_derivative_receipt(uuid,uuid,text,text,uuid,integer,bigint,text,jsonb)',
    'photo.finalize_successful_v28_restoration_preview(uuid,uuid)',
)


def require_restoration_preview_schema(connection):
    info = getattr(connection, 'info', None)
    if (not connection.autocommit or info is None or
            info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Restoration preview schema requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Restoration preview adopter requires scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                       AND NOT EXISTS(SELECT 1 FROM aclexplode(
                           COALESCE(p.proacl,acldefault('f',p.proowner))) acl
                           WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Restoration preview schema unavailable: '+signature)
