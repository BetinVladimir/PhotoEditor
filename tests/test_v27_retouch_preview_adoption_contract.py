"""Frozen retouch producer identity is not SQL decision/receipt authority."""
import json
from pathlib import Path
from uuid import UUID,uuid4,uuid5
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput


def fixture():
    return (Path(__file__).resolve().parents[1]/'backend/internal/semanticreview/testdata/retouch_preview_adoption_params.json').read_bytes()


def test_shown_retouch_shared_go_wire_keeps_maximum_attempt_and_fence():
    raw=fixture();owned=ShownRetouchAdoptionInput.model_validate_json(raw)
    assert canonical_bytes(owned)==raw
    assert owned.preview_attempt==2147483647 and owned.preview_fence==9223372036854775807


@pytest.mark.parametrize('failure',['foreign_cleanup','foreign_exposure','task','alias','scope_alias','zero_uuid',
    'zero_attempt','bool_attempt','attempt_overflow','zero_fence','fence_overflow','approval_field'])
def test_shown_retouch_refuses_foreign_or_unbound_producer(failure):
    raw=parse_json(fixture())
    if failure.startswith('foreign_'):
        kind='background_cleanup' if failure=='foreign_cleanup' else 'local_exposure'
        raw['preview_task_id']=str(uuid5(UUID(raw['preview_run_id']),'semantic.preview_'+kind+':'+raw['target_id']))
    if failure=='task':raw['preview_task_id']=str(uuid4())
    if failure=='alias':raw['pixels']=raw['working_image']
    if failure=='scope_alias':raw['child_run_id']=raw['preview_run_id']
    if failure=='zero_uuid':raw['tenant_id']=str(UUID(int=0))
    if failure=='zero_attempt':raw['preview_attempt']=0
    if failure=='bool_attempt':raw['preview_attempt']=True
    if failure=='attempt_overflow':raw['preview_attempt']=2**31
    if failure=='zero_fence':raw['preview_fence']=0
    if failure=='fence_overflow':raw['preview_fence']=2**63
    if failure=='approval_field':raw['confirmed']=True
    with pytest.raises(ValueError):ShownRetouchAdoptionInput.model_validate_json(json.dumps(raw))
