"""Exact current API pin and retained CopyFence worker generation."""
from hashlib import sha256
from pathlib import Path
import json
from photo_worker.v2.contract_lock import SCHEMAS_SHA256,COPY_FENCE_SCHEMAS_SHA256,COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
ROOT=Path(__file__).resolve().parents[1]


def test_current_api_bundle_pin_and_all_preexisting_worker_definitions():
    raw=(ROOT/'docs/implementation-v2/contracts/schemas.json').read_bytes()
    assert sha256(raw).hexdigest()==SCHEMAS_SHA256
    bundle=json.loads(raw);bundle['$defs'].pop('CollectionApplyRequest')
    # Recorded from the exact75e3 predecessor before the sole API DTO addition.
    assert sha256(json.dumps(bundle,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()=='44a477681b7570dc27396daf89f2073de347a43ad2707b2c513621956b5b864a'


def test_copy_fence_sql_generation_and_historical_reader_pin_remain_exact():
    assert COPY_FENCE_SCHEMAS_SHA256=='75e3bcf5679402efe777b409f919bdcd594f92be5a163975ccf66e27dc3b6ee8'
    assert COPY_FENCE_SCHEMAS_SHA256!=COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
    assert SCHEMAS_SHA256!=COPY_FENCE_SCHEMAS_SHA256
    sql=(ROOT/'backend/db/migrations/02570_copy_fence_current_schema_pin.sql').read_text()
    assert "new_pin constant text := '"+COPY_FENCE_SCHEMAS_SHA256+"'" in sql
