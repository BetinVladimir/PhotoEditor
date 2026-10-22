"""Exact authority ABI required before independent Region adoption starts."""
from psycopg.pq import TransactionStatus


def require_region_replay_schema(connection):
    if not connection.autocommit or connection.info.transaction_status!=TransactionStatus.IDLE:
        raise ValueError('Region replay schema requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone()!=('photo_scheduler',):
            raise ValueError('Region replay requires photo_scheduler')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in ('photo.v26_region_adoption_execution_profile(uuid,uuid,integer,bigint,uuid)',
                          'photo.v26_region_adoption_params(uuid,uuid,integer,bigint,uuid)'):
            row=connection.execute("""
              SELECT p.prokind='f' AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
               AND has_function_privilege(current_user,p.oid,'EXECUTE')
               AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
               AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
               AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
               AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
              FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            """,(signature,)).fetchone()
            if row!=(True,):raise ValueError('Region replay authority unavailable: '+signature)
