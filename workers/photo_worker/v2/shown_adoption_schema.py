"""Structural migration-2586 marker, checked before shown adoption polling."""
from psycopg.pq import TransactionStatus

REQUIRED_FUNCTIONS=(
    ('photo.semantic_shown_adoption_evidence_scope(uuid,uuid,integer,bigint,text)',False,'uuid'),
    ('photo.v27_shown_adoption_artifact(uuid,uuid,integer,bigint,uuid,text)',True,
     'TABLE(bucket text, object_key text, object_version text, sha256 text, bytes bigint, media_type text, role text, source text)'),
    ('photo.v27_shown_adoption_staged_outputs(uuid,uuid,integer,bigint,text)',True,
     'TABLE(artifact_id uuid, sha256 text, state text)'),
)

def require_shown_adoption_evidence_schema(connection):
    if not connection.autocommit or connection.info.transaction_status!=TransactionStatus.IDLE:
        raise ValueError('Shown evidence schema check requires idle autocommit connection')
    with connection.transaction():
        if connection.execute('SELECT current_user').fetchone()!=('photo_scheduler',):
            raise ValueError('Shown evidence reader requires photo_scheduler role')
        connection.execute("SET LOCAL statement_timeout='15s'")
        for signature,exposed,result in REQUIRED_FUNCTIONS:
            row=connection.execute('''SELECT p.prokind='f' AND p.prosecdef AND p.provolatile='s'
                AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
                AND pg_get_function_result(p.oid)=%s
                AND has_function_privilege(current_user,p.oid,'EXECUTE')=%s
                AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
                    WHERE acl.privilege_type='EXECUTE' AND acl.grantee<>p.proowner
                    AND (NOT %s OR acl.grantee<>('photo_scheduler'::regrole)::oid))
                AND NOT EXISTS(SELECT FROM unnest(ARRAY['photo_api','photo_worker','photo_gateway',
                    'photo_portal','photo_semantic_admission','photo_maintenance']) role_name
                    WHERE has_function_privilege(role_name,p.oid,'EXECUTE'))
                FROM pg_proc p WHERE p.oid=to_regprocedure(%s)''',
                (result,exposed,exposed,signature)).fetchone()
            if row!=(True,):
                raise ValueError('Shown evidence schema unavailable: '+signature)
