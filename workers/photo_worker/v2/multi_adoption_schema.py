"""Read-only, exact-ABI gate before enabling multi-image adoption."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.v29_multi_adoption_params(uuid,uuid,integer,bigint,uuid)',
    'photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)',
    'photo.adopt_v29_multi(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)',
)


def require_multi_adoption_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Multi-image schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Multi-image adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Multi-image schema unavailable: '+signature)
