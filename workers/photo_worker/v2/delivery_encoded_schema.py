"""Read-only exact ABI/role/ownership gate for delivery encode adoption."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.v30_delivery_encode_context(uuid,uuid)',
    'photo.v30_delivery_encode_artifact(uuid,uuid,integer,bigint,uuid,uuid)',
    'photo.v30_delivery_encode_staged_outputs(uuid,uuid,integer,bigint,uuid)',
    'photo.record_v30_delivery_encode_verification(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)',
    'photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)',
    'photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)',
    'photo.v30_delivery_pending_encodes(integer)',
    'photo.defer_v30_delivery_encode(uuid,uuid)',
    'photo.reject_v30_delivery_encode_after_retries(uuid,uuid,text)',
)


def require_delivery_encode_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Delivery encode schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Delivery encode adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Delivery encode schema unavailable: '+signature)
