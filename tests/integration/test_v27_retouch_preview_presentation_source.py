"""Canonical retouch presentation metadata access; physical reads are separate."""
import pytest

from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('loss', [None, 'presentation_unverified', 'pixels_unverified', 'task_cancel', 'preview_cancel'])
def test_presentation_source_requires_live_complete_canonical_result(database, loss):
    data = retouch_bundle_fixture(database, admitted=True)
    args, context = data['args'], data['scope']['retouch_context_ref']
    call = (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256)
    def read():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            rows = database.execute('SELECT * FROM photo.semantic_retouch_preview_presentation_source('+','.join(['%s']*8)+')', call).fetchall()
            database.execute('RESET ROLE')
            return rows
    rows = read()
    assert len(rows) == 1
    ref = data['refs']['preview_presentation']
    assert rows[0][:5] == (data['task'],data['result'],1,1,ref.artifact_id)
    assert rows[0][5] == ref.sha256
    assert rows[0][8] not in ('', 'null', None)
    if loss == 'presentation_unverified':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (ref.artifact_id,))
    elif loss == 'pixels_unverified':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (data['refs']['pixels'].artifact_id,))
    elif loss == 'task_cancel':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (data['task'],))
    elif loss == 'preview_cancel':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (data['run'],))
    before = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone()
    assert read() == (rows if loss is None else [])
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone() == before
