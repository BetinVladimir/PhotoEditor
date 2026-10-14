"""Fail before polling when installed Render receipt SQL is unavailable."""

REQUIRED_FUNCTIONS = (
    'photo.pending_v26_render_derivative_receipts(uuid)',
    'photo.v26_render_derivative_context(uuid,uuid,text)',
    'photo.v26_render_derivative_result(uuid,uuid,text)',
    'photo.v26_render_derivative_evidence(uuid,uuid,text,uuid,integer,bigint,uuid)',
    'photo.record_v26_render_derivative_receipt(uuid,uuid,text,uuid,integer,bigint,text)',
    'photo.reject_v26_render_derivative(uuid,uuid,text,uuid,integer,bigint,text)',
)


def require_render_derivative_schema(connection):
    """Check the actual session role and exact SQL argument signatures read-only."""
    if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
        raise ValueError('Render derivative adopter requires photo_scheduler role')
    for signature in REQUIRED_FUNCTIONS:
        row = connection.execute('''
            SELECT p.prokind='f' AND p.prosecdef
                   AND has_function_privilege(current_user,p.oid,'EXECUTE')
            FROM pg_catalog.pg_proc p
            WHERE p.oid=to_regprocedure(%s)
        ''', (signature,)).fetchone()
        if row != (True,):
            raise ValueError('Render derivative schema unavailable: '+signature)
