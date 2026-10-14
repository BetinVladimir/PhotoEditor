"""Startup ABI and role gate for independent V30 import recipe adoption."""

from psycopg.pq import TransactionStatus


REQUIRED_FUNCTIONS = (
    'photo.v30_interchange_import_adoption_context(uuid,uuid,integer,bigint,uuid)',
    'photo.v30_import_adoption_schema_pin(uuid,uuid,integer,bigint,uuid)',
    'photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid)',
    'photo.v30_import_adoption_person_mask_live(uuid,uuid,integer,bigint,uuid,uuid,uuid,text)',
    'photo.v30_import_adoption_qualification_object(uuid,uuid,integer,bigint,uuid,text,jsonb,uuid,text)',
    'photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb,jsonb)',
)


def require_interchange_import_adopter_schema(connection):
    if (not connection.autocommit
            or connection.info.transaction_status != TransactionStatus.IDLE):
        raise ValueError('Import adopter schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
            raise ValueError('Import adopter requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature in REQUIRED_FUNCTIONS:
            row = connection.execute('''
                SELECT p.prokind='f' AND p.prosecdef
                       AND has_function_privilege(current_user,p.oid,'EXECUTE')
                       AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
            ''', (signature,)).fetchone()
            if row != (True,):
                raise ValueError('Import adopter schema unavailable: '+signature)
