from __future__ import annotations

# Runtime identities only. Operator backup/restore identities have separate rules.
_RUNTIME_ROLES = frozenset(('photo_worker', 'photo_scheduler', 'photo_api'))
_ROLE_SQL = """SELECT current_user='photo_worker' AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreaterole AND NOT r.rolreplication
            AND EXISTS(SELECT FROM pg_roles login WHERE login.rolname=session_user
              AND NOT login.rolsuper AND NOT login.rolbypassrls AND NOT login.rolcreaterole AND NOT login.rolreplication
    AND login.rolname NOT IN('photo_fn_owner','photo_runtime_owner'))
            AND NOT EXISTS(SELECT FROM pg_roles privileged
              WHERE (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreaterole OR privileged.rolreplication
      OR privileged.rolname IN('photo_fn_owner','photo_runtime_owner')
      OR EXISTS(SELECT FROM pg_namespace n WHERE n.nspname='photo' AND n.nspowner=privileged.oid)
      OR EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='photo' AND c.relowner=privileged.oid)
      OR EXISTS(SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
        WHERE n.nspname='photo' AND p.prosecdef AND p.proowner=privileged.oid))
                AND (pg_has_role(current_user,privileged.oid,'MEMBER') OR pg_has_role(session_user,privileged.oid,'MEMBER')))
            AND NOT has_schema_privilege(current_user,'photo','CREATE')
  AND NOT has_schema_privilege(session_user,'photo','CREATE')
  FROM pg_roles r WHERE r.rolname=current_user"""


def require_application_role(connection, role: str) -> None:
    """Attest both selected and login identities before any application query.

    The literal replacement is limited to a closed internal role allowlist.
    Schema ownership and transitive privileged memberships are rejected too.
    Caller must close the physical connection if attestation raises.
    """
    if role not in _RUNTIME_ROLES:
        raise ValueError('APPLICATION_DATABASE_ROLE_INVALID')
    query = _ROLE_SQL.replace("current_user='photo_worker'", "current_user='" + role + "'", 1)
    row = connection.execute(query).fetchone()
    if row != (True,) or type(row[0]) is not bool:
        raise ValueError('APPLICATION_DATABASE_ROLE_ISOLATION_INVALID')
