"""Real SQL projection scenarios, authored without execution.

These exercise receipt read shape, not complete relight decision authority.
"""
import hashlib
import pytest
from psycopg.types.json import Jsonb


def test_text_manifest_is_projected_without_rewriting_original_bytes(db):
    raw = '{"source": {"artifact_id":"11111111-1111-4111-8111-111111111111", "sha256":"' + 'a' * 64 + '"},"variant":"preview1600"}'
    receipt = {'result_id': 'result', 'manifest': raw, 'objects': []}
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        projected, original = db.execute(
            "SELECT photo.v26_derivative_receipt_document(%s), %s::jsonb->>'manifest'",
            (Jsonb(receipt), Jsonb(receipt))).fetchone()
    assert projected['manifest']['source']['sha256'] == 'a' * 64
    assert projected['manifest']['variant'] == 'preview1600'
    assert projected['result_id'] == receipt['result_id']
    assert original == raw
    assert hashlib.sha256(original.encode()).digest() == hashlib.sha256(raw.encode()).digest()


def test_object_manifest_projection_is_idempotent(db):
    receipt = {'manifest': {'source': {'sha256': 'a' * 64}}, 'objects': []}
    projected = db.execute('SELECT photo.v26_derivative_receipt_document(%s)',
                           (Jsonb(receipt),)).fetchone()[0]
    assert projected == receipt
    assert db.execute('SELECT photo.v26_derivative_receipt_document(%s)',
                      (Jsonb(projected),)).fetchone()[0] == receipt


@pytest.mark.parametrize('receipt', [None, [], {}, {'manifest': None},
                                    {'manifest': 'invalid'}, {'manifest': '[]'},
                                    {'manifest': 'null'}, {'manifest': 42},
                                    {'manifest': ' ' * (1048576 + 1)}])
def test_malformed_manifest_projection_fails_closed(db, receipt):
    assert db.execute('SELECT photo.v26_derivative_receipt_document(%s)',
                      (Jsonb(receipt),)).fetchone()[0] is None


@pytest.mark.parametrize('role', ['photo_api', 'photo_scheduler', 'photo_worker', 'public'])
def test_projection_has_explicit_api_only_execute(db, role):
    assert db.execute(
        "SELECT has_function_privilege(%s,'photo.v26_derivative_receipt_document(jsonb)','EXECUTE')",
        (role,)).fetchone()[0] is (role == 'photo_api')
