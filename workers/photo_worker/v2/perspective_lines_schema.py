"""Read-only, exact-ABI gate before enabling automatic-line adoption."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS = (
    'photo.v29_perspective_lines_adoption_committed(uuid,uuid)',
    'photo.v29_perspective_lines_adoption_params(uuid,uuid,integer,bigint,uuid)',
    'photo.v29_perspective_lines_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)',
    'photo.adopt_v29_perspective_lines(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text)',
)


def require_perspective_lines_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Perspective lines schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Perspective lines adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Perspective lines schema unavailable: '+signature)
