"""02530 normalizes the authorized list without touching data or table grants."""
from pathlib import Path
from uuid import uuid4
import pytest

ROOT = Path(__file__).resolve().parents[2]
TABLES = ('export_delivery_tickets', 'export_delivery_batches', 'editor_effect_builds',
          'editor_recipe_bridges', 'apply_culling_reviews')


@pytest.mark.migration_head(2529)
def test_purge_allowlist_upgrade_preserves_rows_and_table_acl(db):
    user = db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',
                      (str(uuid4())+'@purge-upgrade.test',)).fetchone()[0]
    tenant = db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Purge upgrade',%s,'{}','{}') RETURNING id", (user,)).fetchone()[0]
    def snapshot():
        rows = [db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s', (tenant,)).fetchone()]
        for name in TABLES:
            rows.append(db.execute('SELECT oid,relowner,relacl::text FROM pg_class WHERE oid=to_regclass(%s)', ('photo.'+name,)).fetchone())
            assert rows[-1] is not None
        return rows
    before = snapshot()
    db.execute((ROOT/'backend/db/migrations/02530_tenant_purge_allowlist_normalization.sql').read_text().split('-- +goose Down')[0])
    assert snapshot() == before
    definition = db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
    import re
    array = re.search(r'tables_to_purge constant text\[\]:=ARRAY\[(.*?)\]', definition, re.S)
    assert array is not None
    for name in TABLES:
        assert array[1].count("'"+name+"'") == 1
        assert 'DELETE FROM photo.'+name+' WHERE' not in definition
    # Reapplying cannot silently duplicate entries or broaden the function.
    with pytest.raises(Exception, match='TENANT_PURGE_ALLOWLIST_ENTRY_SHAPE'):
        with db.transaction():
            db.execute((ROOT/'backend/db/migrations/02530_tenant_purge_allowlist_normalization.sql').read_text().split('-- +goose Down')[0])
