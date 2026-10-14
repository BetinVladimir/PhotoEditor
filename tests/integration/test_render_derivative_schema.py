"""Startup admission against an isolated migrated PostgreSQL database."""
import psycopg
import pytest
from photo_worker.v2.render_derivative_schema import require_render_derivative_schema


def test_render_derivative_schema_role_presence_and_grant(database):
    with psycopg.connect(**database, autocommit=True) as connection:
        with pytest.raises(ValueError, match='requires photo_scheduler'):
            require_render_derivative_schema(connection)
        connection.execute('SET ROLE photo_scheduler')
        require_render_derivative_schema(connection)
        connection.execute('RESET ROLE')
        signature = 'photo.pending_v26_render_derivative_receipts(uuid)'
        with connection.transaction(force_rollback=True):
            connection.execute('REVOKE EXECUTE ON FUNCTION '+signature+' FROM photo_scheduler')
            connection.execute('SET LOCAL ROLE photo_scheduler')
            with pytest.raises(ValueError, match='schema unavailable'):
                require_render_derivative_schema(connection)
        with connection.transaction(force_rollback=True):
            connection.execute('ALTER FUNCTION '+signature+' RENAME TO test_pending_hidden')
            connection.execute('SET LOCAL ROLE photo_scheduler')
            with pytest.raises(ValueError, match='schema unavailable'):
                require_render_derivative_schema(connection)
        connection.execute('SET ROLE photo_scheduler')
        require_render_derivative_schema(connection)
