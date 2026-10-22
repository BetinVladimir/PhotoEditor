"""Exact seven-role presentation inventory; metadata authority, not PNG QA."""
import pytest

from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config

ROLES = ('preview_evidence','working_image','pixels','before','after','affected','protected')


@pytest.mark.parametrize('loss', [None, *ROLES])
def test_sidecars_require_all_exact_verified_outputs(database, loss):
    data = retouch_bundle_fixture(database, admitted=True)
    args, context = data['args'], data['scope']['retouch_context_ref']
    call = (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256,
            data['bodies']['preview_presentation'].decode())
    def read():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            rows = database.execute('SELECT * FROM photo.semantic_retouch_preview_presentation_sidecars('+','.join(['%s']*9)+')', call).fetchall()
            database.execute('RESET ROLE')
            return rows
    rows = read()
    assert len(rows) == 7 and {row[0] for row in rows} == set(ROLES)
    assert len({row[1] for row in rows}) == 7
    for role, artifact, sha, bucket, key, version, size, media in rows:
        ref = data['refs'][role]
        assert artifact == ref.artifact_id and sha == ref.sha256
        assert bucket and key and version not in ('', 'null', None)
        assert size == len(data['bodies'][role])
        assert media == ('application/x-npy' if role == 'pixels' else 'image/png' if role in ROLES[3:] else 'application/json')
    if loss is not None:
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (data['refs'][loss].artifact_id,))
    before = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone()
    # Bundle gate must reject the entire inventory, never return a partial set.
    assert read() == (rows if loss is None else [])
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone() == before


@pytest.mark.parametrize('mutation', ['whitespace', 'truncated', 'foreign_document'])
def test_sidecar_inventory_requires_exact_canonical_presentation_bytes(database, mutation):
    import psycopg
    data = retouch_bundle_fixture(database, admitted=True)
    args, context = data['args'], data['scope']['retouch_context_ref']
    raw = data['bodies']['preview_presentation'].decode()
    if mutation == 'whitespace': raw += ' '
    elif mutation == 'truncated': raw = raw[:-1]
    else: raw = data['bodies']['preview_evidence'].decode()
    call = (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256,raw)
    before = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone()
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            database.execute('SELECT * FROM photo.semantic_retouch_preview_presentation_sidecars('+','.join(['%s']*9)+')', call).fetchall()
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone() == before
