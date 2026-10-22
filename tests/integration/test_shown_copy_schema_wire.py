"""Real SQL copy-wire vectors, grouped because the helpers are pure SELECTs.

Every logical test retains a private database. Explicit case IDs identify all
original61 vector/catalog cases across the two files without61 database clones.
"""
import json
from photo_worker.v2.contract_lock import (
    COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256,
    COPY_FENCE_PREVIOUS_SCHEMAS_SHA256,
)

SCHEMAS = (COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256,
           COPY_FENCE_PREVIOUS_SCHEMAS_SHA256)
SCHEMA_WIRES = (1, 9007199254740991, '9007199254740992', '9223372036854775807')
INVALID_PARAMS = (None, [], {}, {'preview_fence': None}, {'preview_fence': True},
                  {'preview_fence': 0}, {'preview_fence': '1'},
                  {'preview_fence': 9007199254740992}, {'preview_fence': '9223372036854775808'})
UNKNOWN_SCHEMAS = (None, '', '0'*64, '7593be39')


def test_sql_copy_wire_is_bound_to_schema_generation(db):
    for schema_index, schema in enumerate(SCHEMAS):
        for wire_index, wire in enumerate(SCHEMA_WIRES):
            case_id = f'schema-generation-{schema_index}/wire-{wire_index}: {schema}/{wire!r}'
            expected = schema in (COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256) or type(wire) is int
            row = db.execute('SELECT photo.shown_copy_schema_wire_ready(%s,%s::jsonb)',
                             (schema, json.dumps({'preview_fence': wire}))).fetchone()
            assert row[0] is expected, case_id


def test_sql_copy_schema_never_authorizes_invalid_wire(db):
    for schema_index, schema in enumerate(SCHEMAS):
        for params_index, params in enumerate(INVALID_PARAMS):
            case_id = f'invalid-schema-{schema_index}/params-{params_index}: {schema}/{params!r}'
            row = db.execute('SELECT photo.shown_copy_schema_wire_ready(%s,%s::jsonb)',
                             (schema, json.dumps(params))).fetchone()
            assert row[0] is False, case_id


def test_sql_copy_schema_rejects_missing_unknown_or_partial_pin(db):
    for index, schema in enumerate(UNKNOWN_SCHEMAS):
        row = db.execute('SELECT photo.shown_copy_schema_wire_ready(%s,%s::jsonb)',
                         (schema, '{"preview_fence":1}')).fetchone()
        assert row[0] is False, f'unknown-schema-{index}: {schema!r}'


def test_current_copy_pin_installed_readers_and_new_seals_preserve_history(db):
    """Inspect actual installed chain; no copied SQL bodies or fabricated tasks."""
    rows = db.execute("""SELECT p.proname, p.prosrc, pg_get_userbyid(p.proowner),
        p.oid, p.prosecdef, p.proconfig FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
        WHERE n.nspname='photo' AND (p.proname IN
        ('shown_copy_schema_wire_ready','semantic_shown_retouch_task_live',
         'semantic_retouch_preview_closure_ready','seal_semantic_retouch_preview_inputs',
         'seal_semantic_shown_retouch_apply_inputs')
         OR position('SEMANTIC_SHOWN_RETOUCH_CLAIM_STALE' IN p.prosrc)>0)""").fetchall()
    assert len(rows) >= 6
    claim_seen = False
    for name, body, owner, oid, definer, config in rows:
        assert owner == 'photo_fn_owner'
        # The pure immutable wire helper has always been SECURITY INVOKER.
        # Preserve that boundary; the data-bearing readers/sealers are definers.
        assert definer is (name != 'shown_copy_schema_wire_ready')
        assert 'search_path=pg_catalog, photo' in config or 'search_path=pg_catalog,photo' in config
        assert COPY_FENCE_SCHEMAS_SHA256 in body
        if name.startswith('seal_'):
            assert COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256 not in body
        else:
            assert COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256 in body
        if 'SEMANTIC_SHOWN_RETOUCH_CLAIM_STALE' in body:
            claim_seen = True
        # Reader rollout must not grant new public authority.
        assert not db.execute("SELECT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE grantee=0 AND privilege_type='EXECUTE') FROM pg_proc WHERE oid=%s", (oid,)).fetchone()[0]
    assert claim_seen
