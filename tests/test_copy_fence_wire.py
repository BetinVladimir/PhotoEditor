"""Authored precision/compatibility scenarios; no application or DB startup."""
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.copy_fence import copy_fence_wire, parse_copy_fence_wire


@pytest.mark.parametrize('value', [1,9007199254740991,9007199254740992,9223372036854775807])
def test_copy_fence_roundtrips_without_changing_safe_numeric_wire(value):
    wire = copy_fence_wire(value)
    assert type(wire) is (int if value <= 9007199254740991 else str)
    assert parse_copy_fence_wire(parse_json(canonical_bytes(wire))) == value


@pytest.mark.parametrize('value', [0,-1,True,False,None,1.0,'1','01',
    '9007199254740991',9007199254740992,'9223372036854775808',
    '+9007199254740992','9e18','９００７１９９２５４７４０９９２'])
def test_copy_fence_rejects_ambiguous_or_unsafe_wire(value):
    with pytest.raises(ValueError): parse_copy_fence_wire(value)


@pytest.mark.parametrize('value', [0,-1,True,1.0,'1',9223372036854775808])
def test_copy_fence_encoder_requires_positive_int64(value):
    with pytest.raises(ValueError): copy_fence_wire(value)


@pytest.mark.parametrize('value', [1,9007199254740991,9007199254740992,9223372036854775807])
def test_typed_copy_fence_preserves_internal_int_and_canonical_json(value):
    from photo_worker.contracts.base import Contract
    from photo_worker.contracts.copy_fence import CopyFence
    class Document(Contract):
        preview_fence: CopyFence
    document = Document(preview_fence=value)
    raw = canonical_bytes(document)
    restored = Document.model_validate_json(raw)
    assert type(restored.preview_fence) is int and restored.preview_fence == value
    assert canonical_bytes(restored) == raw
    assert document.model_dump(mode='python')['preview_fence'] == value


@pytest.mark.parametrize('wire', [0,True,1.0,'1','01',9007199254740992,'9223372036854775808'])
def test_typed_copy_fence_refuses_noncanonical_json_wire(wire):
    import json
    from pydantic import ValidationError
    from photo_worker.contracts.base import Contract
    from photo_worker.contracts.copy_fence import CopyFence
    class Document(Contract):
        preview_fence: CopyFence
    with pytest.raises(ValidationError):
        Document.model_validate_json(json.dumps({'preview_fence':wire}))


@pytest.mark.parametrize('value,accepted', [
    ('9007199254740991',False),('9007199254740992',True),
    ('9999999999999999',True),('10000000000000000',True),
    ('999999999999999999',True),('1000000000000000000',True),
    ('9223372036854775807',True),('9223372036854775808',False),
    ('10000000000000000000',False),('09007199254740992',False),
])
def test_copy_fence_schema_pattern_matches_exact_decimal_range(value,accepted):
    import re
    from photo_worker.contracts.copy_fence import COPY_FENCE_STRING_PATTERN
    assert bool(re.fullmatch(COPY_FENCE_STRING_PATTERN,value)) is accepted


@pytest.mark.parametrize('model_name,operation', [
    ('ShownExposureAdoptionInput','semantic.preview_local_exposure'),
    ('ShownBackgroundCleanupAdoptionInput','semantic.preview_background_cleanup'),
])
def test_all_shown_copy_models_preserve_full_sql_fence(model_name,operation):
    import json
    from uuid import uuid4,uuid5
    from pydantic import ValidationError
    from photo_worker.contracts import semantic_preview
    model=getattr(semantic_preview,model_name)
    target,run=uuid4(),uuid4()
    ref=lambda:{'artifact_id':str(uuid4()),'sha256':'a'*64}
    document=dict(schema_version=1,tenant_id=str(uuid4()),child_run_id=str(uuid4()),
        target_id=str(target),approved_edit=ref(),preview_run_id=str(run),
        preview_task_id=str(uuid5(run,operation+':'+str(target))),
        preview_result_id=str(uuid4()),preview_attempt=2147483647,
        preview_fence='9223372036854775807',presentation=ref(),numerical_evidence=ref(),
        working_image=ref(),pixels=ref(),model_lock_sha='b'*64,
        runtime_lock_sha='c'*64,device_profile_sha='d'*64)
    raw=canonical_bytes(document)
    restored=model.model_validate_json(raw)
    assert restored.preview_fence==9223372036854775807
    assert canonical_bytes(restored)==raw
    for unsafe in [9223372036854775807,9007199254740992,'1','9223372036854775808']:
        document['preview_fence']=unsafe
        with pytest.raises(ValidationError):model.model_validate_json(json.dumps(document))
